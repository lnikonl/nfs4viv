package resources

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const scratchOBJ = `o HB
v -1 0 -2
v 1 0 -2
v 0 1 2
vt 0 0
vt 1 0
vt 0.5 1
usemtl BodyPaint
f 1/1 2/2 3/3
o HLFW
v -2 0 -1
v -1 0 -1
v -1.5 1 -1
usemtl fce_t00000002_f00000009
f 4/1 5/2 6/3
`

func TestCreateFCEWithoutDonor(t *testing.T) {
	out, err := CreateFCE([]byte(scratchOBJ), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate("fce", out, 0); err != nil {
		t.Fatal(err)
	}
	if fceU32(out, 0x11c) != 2 || fceU32(out, 0xc) != 6 || fceU32(out, 8) != 2 || fceU32(out, 0x10) != 3 {
		t.Fatal("new FCE counts are wrong")
	}
	if fceName(out, 0xe28) != ":HB" || fceName(out, 0xe68) != ":HLFW" {
		t.Fatal("game part names were not created")
	}
	if out[0x826] != 255 || out[0x827] != 255 {
		t.Fatal("initial paint must be opaque white")
	}
	if v := fceVector(out, 0x12c); v != (fceVec{-1.5, 0.5, -1}) {
		t.Fatalf("wheel origin = %v", v)
	}
	if v := fceVector(out, 0x4c); v != (fceVec{2, 1, 2}) {
		t.Fatalf("automatic half-size = %v", v)
	}
	base := fceTable(out, 0x1c)
	if fceU32(out, base+28) != 1 || fceU32(out, base+56) != 2 || fceU32(out, base+56+28) != 9 {
		t.Fatal("generic/encoded materials lost")
	}
	for _, pair := range [][2]int{{0x14, 0x34}, {0x18, 0x38}} {
		a, b := fceTable(out, pair[0]), fceTable(out, pair[1])
		if !bytes.Equal(out[a:a+72], out[b:b+72]) {
			t.Fatal("new model should have identical base and damage states")
		}
	}
	files, err := Extract("fce", out)
	if err != nil {
		t.Fatal(err)
	}
	rebuilt, err := Pack("fce", files)
	if err != nil || !bytes.Equal(rebuilt, out) {
		t.Fatalf("new FCE project round trip: %v", err)
	}
}

func TestFCEMetadataPivotsDummiesAndPartReplacement(t *testing.T) {
	original := multipartFCE()
	files, _ := Extract("fce", original)
	var p project
	if err := json.Unmarshal(files[ManifestName], &p); err != nil {
		t.Fatal(err)
	}
	// Replace one slot with a brand new part and move the surviving slot's
	// pivot. World-space OBJ geometry must stay in place after both operations.
	p.FCE.Parts = p.FCE.Parts[1:]
	p.FCE.Parts[0].Origin = fceVec{-12, -21, -30}
	p.FCE.Parts = append(p.FCE.Parts, fcePartMetadata{Object: "new_body", Name: ":HB", Origin: fceVec{10, 20, 30}})
	p.FCE.Dummies = []fceDummyMetadata{{Name: ":EXHAUST", Position: fceVec{0, 0.2, 2}}}
	p.FCE.HalfSize = nil
	p.FCE.Colors = append(p.FCE.Colors, fceColorMetadata{Primary: [4]uint8{0, 255, 255, 255}})
	files[ManifestName], _ = marshalJSON(p)
	files["model.obj"] = bytes.Replace(files["model.obj"], []byte("o part_000__HB"), []byte("o new_body"), 1)
	// This is a new deformation model, not transferred donor damage.
	files["damaged.obj"] = append([]byte(nil), files["model.obj"]...)
	out, err := Pack("fce", files)
	if err != nil {
		t.Fatal(err)
	}
	if fceName(out, 0xe28) != ":HLFW" || fceName(out, 0xe68) != ":HB" || fceU32(out, 0x58) != 1 || fceName(out, 0xa28) != ":EXHAUST" {
		t.Fatal("part/dummy editing lost")
	}
	if fceU32(out, 0x820) != 2 || !bytes.Equal(out[0x828:0x82c], []byte{0, 255, 255, 255}) {
		t.Fatal("paint scheme editing lost")
	}
	if v := fceVector(out, fceTable(out, 0x14)); v != (fceVec{3, 1, 0}) {
		t.Fatalf("pivot move changed world geometry: %v", v)
	}
	if v := fceVector(out, 0x4c); v != (fceVec{11, 21, 31}) {
		t.Fatalf("half-size not recomputed: %v", v)
	}
	if fceU32(out, fceTable(out, 0x40)) != 13 || fceU32(out, fceTable(out, 0x40)+12) != 0 {
		t.Fatal("source-part/new-part animation mapping is wrong")
	}
}

func TestFCEMetadataRemovePart(t *testing.T) {
	files, _ := Extract("fce", multipartFCE())
	var p project
	if err := json.Unmarshal(files[ManifestName], &p); err != nil {
		t.Fatal(err)
	}
	p.FCE.Parts = p.FCE.Parts[1:]
	files[ManifestName], _ = marshalJSON(p)
	obj := string(files["model.obj"])
	obj = obj[strings.Index(obj, "\no part_001"):]
	obj = strings.Replace(obj, "f 4/4/4 5/5/5 6/6/6", "f 1/1/1 2/2/2 3/3/3", 1)
	files["model.obj"] = []byte(obj)
	out, err := Pack("fce", files)
	if err != nil {
		t.Fatal(err)
	}
	if fceU32(out, 0x11c) != 1 || fceU32(out, 0xc) != 3 || fceU32(out, 8) != 1 || fceName(out, 0xe28) != ":HLFW" || fceName(out, 0xe68) != "" {
		t.Fatal("part removal did not clear stale header entries")
	}
	if fceU32(out, fceTable(out, 0x40)) != 13 {
		t.Fatal("removal changed the surviving part's data source")
	}
}

func TestFCEAutomaticBoundsIncludeTransferredDamage(t *testing.T) {
	original := multipartFCE()
	fceWriteVector(original, fceTable(original, 0x34), fceVec{3, 0, 0})
	files, _ := Extract("fce", original)
	var p project
	if err := json.Unmarshal(files[ManifestName], &p); err != nil {
		t.Fatal(err)
	}
	p.FCE.HalfSize = nil
	files[ManifestName], _ = marshalJSON(p)
	out, err := Pack("fce", files)
	if err != nil {
		t.Fatal(err)
	}
	if v := fceVector(out, 0x4c); v != (fceVec{13, 21, 31}) {
		t.Fatalf("inherited damage outside automatic bounds: %v", v)
	}
}

func TestCreateFCEDamageAndInvalidInputs(t *testing.T) {
	damage := strings.Replace(scratchOBJ, "v 0 1 2", "v 0 0.5 2", 1)
	out, err := CreateFCE([]byte(scratchOBJ), []byte(damage))
	if err != nil {
		t.Fatal(err)
	}
	if v := fceVector(out, fceTable(out, 0x34)+24); v[1] != 0 {
		t.Fatalf("explicit damaged vertex = %v", v)
	}
	for _, input := range []string{
		"", strings.Replace(scratchOBJ, "1/1", "1", 1),
		strings.Replace(scratchOBJ, "o HLFW", "o HB", 1),
		strings.Replace(scratchOBJ, "fce_t00000002_f00000009", "fce_invalid", 1),
	} {
		if _, err := CreateFCE([]byte(input), nil); err == nil {
			t.Fatalf("accepted invalid model %q", input)
		}
	}
	if _, err := CreateFCE([]byte(scratchOBJ), []byte(strings.Replace(damage, "f 4/1 5/2 6/3", "", 1))); err == nil {
		t.Fatal("accepted mismatched damage topology")
	}
	files, _ := Extract("fce", out)
	var p project
	json.Unmarshal(files[ManifestName], &p)
	bad := 64
	p.FCE.Parts[0].SourcePart = &bad
	files[ManifestName], _ = marshalJSON(p)
	if _, err := Pack("fce", files); err == nil || !strings.Contains(err.Error(), "source_part") {
		t.Fatalf("invalid source_part: %v", err)
	}
}

func TestCreateFCEBlender(t *testing.T) {
	python := os.Getenv("NFS4_BLENDER_PYTHON")
	if python == "" {
		t.Skip("set NFS4_BLENDER_PYTHON to a Python interpreter with bpy")
	}
	dir := t.TempDir()
	if output, err := exec.Command(python, "testdata/fce_blender_create.py", dir).CombinedOutput(); err != nil {
		t.Fatalf("Blender: %v\n%s", err, output)
	} else {
		t.Log(string(output))
	}
	model, err := os.ReadFile(filepath.Join(dir, "new_car.obj"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := CreateFCE(model, nil)
	if err != nil {
		t.Fatal(err)
	}
	if fceU32(out, 0x11c) != 5 || fceU32(out, 8) < 20 {
		t.Fatal("Blender's body and four wheels were not created")
	}
	cfg := metadataFromFCE(out)
	names := map[string]bool{}
	for _, p := range cfg.Parts {
		names[p.Name] = true
	}
	for _, name := range []string{":HB", ":HLFW", ":HRFW", ":HLRW", ":HRRW"} {
		if !names[name] {
			t.Fatalf("missing %s", name)
		}
	}
}
