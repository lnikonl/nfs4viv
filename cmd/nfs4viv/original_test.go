package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// TestExportImportOriginalCars runs the export/import round trip against
// original car samples when they are present under original/nfs4viv/DATA/CARS.
// The samples are never part of the public repository: they live on the
// private original-data branch and CI copies them in before running the
// tests. When the samples are missing, or are still git-lfs pointers, the
// test skips itself and the synthetic fixtures from cartest remain the
// primary coverage.
func TestExportImportOriginalCars(t *testing.T) {
	root := filepath.Join("..", "..", "original", "nfs4viv", "DATA", "CARS")
	directories, err := os.ReadDir(root)
	if err != nil {
		t.Skip("original car samples are not available")
	}
	cars := 0
	for _, directory := range directories {
		if !directory.IsDir() {
			continue
		}
		vivPath := filepath.Join(root, directory.Name(), "CAR.VIV")
		if _, err := os.Stat(vivPath); err != nil {
			continue
		}
		if !isVivFile(vivPath) {
			t.Skipf("%s is not a materialized VIV archive; run git lfs pull to test against the original samples", vivPath)
		}
		cars++
		t.Run(directory.Name(), func(t *testing.T) {
			temp := t.TempDir()
			first := filepath.Join(temp, "first.json")
			second := filepath.Join(temp, "second.json")
			copyPath := filepath.Join(temp, "car.viv")

			if _, err := captureStdout(t, func() error {
				return run([]string{"fedata", "export", "-lang", "all", "-o", first, vivPath})
			}); err != nil {
				t.Fatalf("export: %v", err)
			}
			if _, err := captureStdout(t, func() error {
				return run([]string{"fedata", "import", "-lang", "all", "-o", copyPath, vivPath, first})
			}); err != nil {
				t.Fatalf("import: %v", err)
			}
			if _, err := captureStdout(t, func() error {
				return run([]string{"fedata", "export", "-lang", "all", "-o", second, copyPath})
			}); err != nil {
				t.Fatalf("export: %v", err)
			}
			before, err := os.ReadFile(first)
			if err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(second)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Errorf("round trip changed the export of %s", vivPath)
			}
			var list []map[string]any
			if err := json.Unmarshal(before, &list); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if len(list) != 7 {
				t.Errorf("%s: exported %d languages, want 7", directory.Name(), len(list))
			}
		})
	}
	if cars == 0 {
		t.Skip("original car samples are not available")
	}
}

// isVivFile reports whether path starts with the BIGF magic. Git-lfs pointer
// files and missing objects fail this check.
func isVivFile(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	header := make([]byte, 4)
	if _, err := io.ReadFull(file, header); err != nil {
		return false
	}
	return string(header) == "BIGF"
}
