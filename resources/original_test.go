package resources

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lnikonl/nfs4viv/viv"
)

func TestOriginalResources(t *testing.T) {
	root := os.Getenv("NFS4_CARS")
	if root == "" {
		for _, candidate := range []string{"../original/nfs4viv/DATA/CARS", "../../nfs4net/original/NFS4/DATA/CARS"} {
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				root = candidate
				break
			}
		}
	}
	if root == "" {
		t.Skip("original CAR samples are not available; set NFS4_CARS")
	}
	count := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.EqualFold(entry.Name(), "car.viv") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.HasPrefix(data, []byte("BIGF")) {
			return nil // Unmaterialized git-lfs object.
		}
		archive, err := viv.Read(data)
		if err != nil {
			return err
		}
		for _, e := range archive.Entries {
			format := strings.TrimPrefix(strings.ToLower(filepath.Ext(e.Name)), ".")
			if format == "tga" || Supported(format) {
				count++
				t.Run(filepath.Base(filepath.Dir(path))+"/"+e.Name, func(t *testing.T) {
					if err := Validate(format, e.Data, 256); err != nil {
						t.Fatalf("Validate: %v", err)
					}
					if format == "tga" {
						return
					}
					files, err := Extract(format, e.Data)
					if err != nil {
						t.Fatalf("Extract: %v", err)
					}
					packed, err := Pack(format, files)
					if err != nil {
						t.Fatalf("Pack: %v", err)
					}
					if !bytes.Equal(e.Data, packed) {
						for i := 0; i < min(len(e.Data), len(packed)); i++ {
							if e.Data[i] != packed[i] {
								t.Fatalf("round trip differs at 0x%x: 0x%02x != 0x%02x", i, e.Data[i], packed[i])
							}
						}
						t.Fatalf("round trip changed size: %d -> %d", len(e.Data), len(packed))
					}
				})
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Skip("no materialized original resources")
	}
	t.Logf("checked %d original resources", count)
}
