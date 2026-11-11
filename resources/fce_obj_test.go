package resources

import (
	"bytes"
	"encoding/binary"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lnikonl/nfs4viv/cartest"
	"github.com/lnikonl/nfs4viv/viv"
)

// Two named parts, local triangle indices, distinct origins, and per-vertex
// animation markers. The second part has the same local mesh as the first.
func multipartFCE() []byte {
	source := cartest.ResourceFiles()["car.fce"]
	header := append([]byte(nil), source[:fceHeaderSize]...)
	fcePut(header, 8, 2)
	fcePut(header, 0xc, 6)
	fcePut(header, 0x11c, 2)
	fcePut(header, 0x424, 3)
	fcePut(header, 0x524, 3)
	fcePut(header, 0x624, 1)
	fcePut(header, 0x724, 1)
	fceWriteVector(header, 0x120, fceVec{10, 20, 30})
	fceWriteVector(header, 0x12c, fceVec{-10, -20, -30})
	copy(header[0xe68:], ":HLFW")
	out := header
	for _, table := range fceGeometryTables {
		fcePut(out, table.field, len(out)-fceHeaderSize)
		count := 3
		if table.triangles {
			count = 1
		}
		off := fceTable(source, table.field)
		for i := 0; i < 2; i++ {
			out = append(out, source[off:off+count*table.stride]...)
		}
	}
	fcePut(out, 0x2c, fceU32(out, 0x14))
	fcePut(out, 0x30, fceU32(out, 0x18))
	for i := 0; i < 6; i++ {
		fcePut(out, fceTable(out, 0x40)+4*i, 10+i)
	}
	return out
}

func TestFCEOBJPartsPivotsAndDamage(t *testing.T) {
	original := multipartFCE()
	files, err := Extract("fce", original)
	if err != nil {
		t.Fatal(err)
	}
	obj := string(files["model.obj"])
	for _, want := range []string{"o part_000__HB", "o part_001__HLFW", "v 11 20 30", "v -9 -20 -30", "f 4/4/4 5/5/5 6/6/6"} {
		if !strings.Contains(obj, want) {
			t.Fatalf("missing %q in OBJ", want)
		}
	}
	files["model.obj"] = bytes.Replace(files["model.obj"], []byte("v -9 -20 -30"), []byte("v -8 -20 -30"), 1)
	files["damaged.obj"] = bytes.Replace(files["damaged.obj"], []byte("v -9 -20 -30"), []byte("v -7 -20 -30"), 1)
	out, err := Pack("fce", files)
	if err != nil {
		t.Fatal(err)
	}
	if got := fceVector(out, fceTable(out, 0x14)+36); got != (fceVec{2, 0, 0}) {
		t.Fatalf("part-local base vertex = %v", got)
	}
	if got := fceVector(out, fceTable(out, 0x34)+36); got != (fceVec{3, 0, 0}) {
		t.Fatalf("part-local damage vertex = %v", got)
	}
	if !bytes.Equal(out[:fceHeaderSize], original[:fceHeaderSize]) {
		t.Fatal("part names, pivots or game metadata changed")
	}
	if got := binary.LittleEndian.Uint32(out[fceTable(out, 0x40)+12:]); got != 13 {
		t.Fatalf("animation marker = %d", got)
	}
}

func TestFCEOBJTopologyAndCornerNormals(t *testing.T) {
	files, err := Extract("fce", multipartFCE())
	if err != nil {
		t.Fatal(err)
	}
	// Add one vertex and one triangle to the first part. Give a shared vertex
	// a second corner normal: FCE must split it, while keeping UVs per corner.
	obj := string(files["model.obj"])
	obj = strings.Replace(obj, "\no part_001", "\nv 11 21 30\nvn 0 0 1\nvt 0.25 0.75\nf 1/1/4 3/3/3 4/4/4\n\no part_001", 1)
	obj = strings.Replace(obj, "f 4/4/4 5/5/5 6/6/6", "f 5/5/5 6/6/6 7/7/7", 1)
	files["model.obj"] = []byte(obj)
	out, err := Pack("fce", files)
	if err != nil {
		t.Fatal(err)
	}
	if fceU32(out, 8) != 3 || fceU32(out, 0xc) != 8 || fceU32(out, 0x524) != 3 || fceU32(out, 0x424) != 5 || fceU32(out, 0x624) != 2 {
		t.Fatal("resized geometry counts/part ranges were not rebuilt")
	}
	if fceU32(out, 0x2c) != fceU32(out, 0x14) || fceU32(out, 0x30) != fceU32(out, 0x18) {
		t.Fatal("undamaged aliases were not relocated")
	}
	if got := fceVector(out, fceTable(out, 0x14)+5*12); got != (fceVec{1, 0, 0}) {
		t.Fatalf("second part geometry moved: %v", got)
	}
	if got := fceU32(out, fceTable(out, 0x40)+5*4); got != 13 {
		t.Fatalf("animation leaked between parts: %d", got)
	}
	if err := Validate("fce", out, 0); err != nil {
		t.Fatal(err)
	}
}

func TestFCEOBJRejectsInvalidEdits(t *testing.T) {
	for _, tc := range []struct{ name, old, new, message string }{
		{"missing part", "o part_000__HB", "o body", "unknown object"},
		{"material", "usemtl fce_t00000000_f00000001", "usemtl Material", "unknown material"},
		{"nonfinite", "v 1 0 0", "v NaN 0 0", "invalid coordinate"},
		{"index", "f 1/1/1", "f 999/1/1", "outside table"},
		{"UV", "f 1/1/1", "f 1//1", "missing UV"},
		{"quad", "f 1/1/1 2/2/2 3/3/3", "f 1/1/1 2/2/2 3/3/3 1/1/1", "triangulate"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files, err := Extract("fce", cartest.ResourceFiles()["car.fce"])
			if err != nil {
				t.Fatal(err)
			}
			files["model.obj"] = bytes.Replace(files["model.obj"], []byte(tc.old), []byte(tc.new), 1)
			if _, err := Pack("fce", files); err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("expected %q, got %v", tc.message, err)
			}
		})
	}
}

func TestFCEOBJNegativeIndicesAndRegeneratedNormals(t *testing.T) {
	data := cartest.ResourceFiles()["car.fce"]
	files, _ := Extract("fce", data)
	files["model.obj"] = bytes.Replace(files["model.obj"], []byte("f 1/1/1 2/2/2 3/3/3"), []byte("f -3/-3 -2/-2 -1/-1"), 1)
	out, err := Pack("fce", files)
	if err != nil {
		t.Fatal(err)
	}
	n := fceVector(out, fceTable(out, 0x18))
	if n[0] < 0.57 || n[1] < 0.57 || n[2] < 0.57 {
		t.Fatalf("generated normal = %v", n)
	}
}

func TestFCEOBJIndependentUndamagedTables(t *testing.T) {
	original := cartest.ResourceFiles()["car.fce"]
	for _, pair := range [][2]int{{0x2c, 0x14}, {0x30, 0x18}} {
		offset := fceTable(original, pair[1])
		fcePut(original, pair[0], len(original)-fceHeaderSize)
		original = append(original, original[offset:offset+36]...)
	}
	// Preserve the table's own values, even if not identical to active geometry.
	fceWriteVector(original, fceTable(original, 0x2c), fceVec{0.5, 0, 0})
	files, err := Extract("fce", original)
	if err != nil {
		t.Fatal(err)
	}
	files["model.obj"] = bytes.Replace(files["model.obj"], []byte("v 1 0 0"), []byte("v 2.5 0 0"), 1)
	out, err := Pack("fce", files)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(original) || fceU32(out, 0x2c) != fceU32(original, 0x2c) || fceU32(out, 0x30) != fceU32(original, 0x30) {
		t.Fatal("independent undamaged tables were duplicated instead of updated")
	}
	if v := fceVector(out, fceTable(out, 0x2c)); v != (fceVec{2, 0, 0}) {
		t.Fatalf("undamaged table edit delta lost: %v", v)
	}
}

func TestFCEOBJGlobalVertexPoolAndDamageMismatch(t *testing.T) {
	original := multipartFCE()
	files, _ := Extract("fce", original)
	var pool, elements strings.Builder
	for _, line := range strings.Split(string(files["model.obj"]), "\n") {
		if strings.HasPrefix(line, "v ") || strings.HasPrefix(line, "vn ") || strings.HasPrefix(line, "vt ") {
			pool.WriteString(line + "\n")
		} else {
			elements.WriteString(line + "\n")
		}
	}
	// A valid OBJ can put all vertex declarations under its first object.
	files["model.obj"] = []byte(strings.Replace(elements.String(), "o part_000__HB\n", "o part_000__HB\n"+pool.String(), 1))
	out, err := Pack("fce", files)
	if err != nil {
		t.Fatal(err)
	}
	if fceU32(out, 0x520) != 3 || fceU32(out, 0x524) != 3 {
		t.Fatal("global OBJ vertices leaked between parts")
	}
	files["damaged.obj"] = bytes.Replace(files["damaged.obj"], []byte("f 1/1/1 2/2/2 3/3/3\n"), nil, 1)
	if _, err := Pack("fce", files); err == nil || !strings.Contains(err.Error(), "topology must match") {
		t.Fatalf("damage topology mismatch not diagnosed: %v", err)
	}
}

func TestFCEOBJLegacyProject(t *testing.T) {
	data := cartest.ResourceFiles()["car.fce"]
	p := &project{Version: 1, Format: "fce"}
	if err := p.parseFCE(data); err != nil {
		t.Fatal(err)
	}
	if err := p.cover(data); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, s := range p.Sections {
		b, err := exportSection(s, data[s.Offset:s.Offset+s.Size])
		if err != nil {
			t.Fatal(err)
		}
		files[s.File] = b
	}
	files[ManifestName], _ = marshalJSON(p)
	out, err := Pack("fce", files)
	if err != nil || !bytes.Equal(out, data) {
		t.Fatalf("legacy project round trip: %v", err)
	}
}

func TestOriginalFCEOBJRebuilds(t *testing.T) {
	root := os.Getenv("NFS4_CARS")
	if root == "" {
		root = "../../nfs4net/original/NFS4/DATA/CARS"
		if _, err := os.Stat(root); err != nil {
			t.Skip("set NFS4_CARS to the original DATA/CARS directory")
		}
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
		if !bytes.HasPrefix(data, viv.Magic) {
			return nil
		} // Unmaterialized LFS files.
		archive, err := viv.Read(data)
		if err != nil {
			return err
		}
		for _, entry := range archive.Entries {
			if !strings.HasSuffix(strings.ToLower(entry.Name), ".fce") {
				continue
			}
			count++
			t.Run(filepath.Base(filepath.Dir(path))+"/"+entry.Name, func(t *testing.T) {
				files, err := Extract("fce", entry.Data)
				if err != nil {
					t.Fatal(err)
				}
				// Force the OBJ parser/rebuilder, bypassing original-byte reuse.
				files["model.obj"] = append(files["model.obj"], []byte("# reserialized\n")...)
				files["damaged.obj"] = append(files["damaged.obj"], []byte("# reserialized\n")...)
				out, err := Pack("fce", files)
				if err != nil {
					t.Fatal(err)
				}
				oldParts, newParts := fceParts(entry.Data), fceParts(out)
				if len(oldParts) != len(newParts) {
					t.Fatal("part count changed")
				}
				for i, old := range oldParts {
					new := newParts[i]
					if old.name != new.name || old.origin != new.origin || old.numV != new.numV || old.numT != new.numT {
						t.Fatalf("part %d metadata changed", i)
					}
					for _, field := range []int{0x14, 0x18, 0x2c, 0x30, 0x34, 0x38} {
						for j := 0; j < old.numV; j++ {
							a := fceVector(entry.Data, fceTable(entry.Data, field)+12*(old.firstV+j))
							b := fceVector(out, fceTable(out, field)+12*(new.firstV+j))
							for axis := range a {
								if math.Abs(float64(a[axis])-float64(b[axis])) > 1e-7 {
									t.Fatalf("%s table 0x%x vertex %d: %v -> %v", old.name, field, j, a, b)
								}
							}
						}
					}
					for _, table := range fceGeometryTables {
						if table.field == 0x14 || table.field == 0x18 || table.field == 0x34 || table.field == 0x38 {
							continue
						}
						count, oldFirst, newFirst := old.numV, old.firstV, new.firstV
						if table.triangles {
							count, oldFirst, newFirst = old.numT, old.firstT, new.firstT
						}
						aOff := fceTable(entry.Data, table.field) + table.stride*oldFirst
						bOff := fceTable(out, table.field) + table.stride*newFirst
						for j := 0; j < count*table.stride; j += 4 {
							a := binary.LittleEndian.Uint32(entry.Data[aOff+j:])
							b := binary.LittleEndian.Uint32(out[bOff+j:])
							if table.field == 0x1c && j%56 >= 32 {
								if math.Abs(float64(math.Float32frombits(a))-float64(math.Float32frombits(b))) <= 1e-7 {
									continue
								}
							}
							if a != b {
								t.Fatalf("%s table 0x%x byte %d changed: %08x -> %08x", old.name, table.field, j, a, b)
							}
						}
					}
				}
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Skip("no materialized original FCE samples")
	}
	t.Logf("rebuilt %d original FCE files through OBJ", count)
}

// Optional real Blender integration, using the official bpy Python module.
// NFS4_BLENDER_PYTHON=/path/to/venv/bin/python go test ./resources -run Blender
func TestFCEOBJBlender(t *testing.T) {
	python := os.Getenv("NFS4_BLENDER_PYTHON")
	if python == "" {
		t.Skip("set NFS4_BLENDER_PYTHON to a Python interpreter with bpy")
	}
	original := multipartFCE()
	files, err := Extract("fce", original)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(python, "testdata/fce_blender.py", dir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Blender: %v\n%s", err, output)
	} else {
		t.Log(string(output))
	}
	for _, name := range []string{"model.obj", "model.mtl"} {
		files[name], err = os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
	}
	out, err := Pack("fce", files)
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate("fce", out, 0); err != nil {
		t.Fatal(err)
	}
	if fceU32(out, 0x720) <= 1 || fceU32(out, 0x724) != 1 {
		t.Fatal("Blender topology edit or part separation lost")
	}
	// Translation is baked by OBJ export; FCE must subtract the original pivot.
	if got := fceVector(out, fceTable(out, 0x14)); got[0] < 2.99 {
		t.Fatalf("Blender transform lost: %v", got)
	}
}
