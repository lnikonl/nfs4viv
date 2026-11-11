package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lnikonl/nfs4viv/cartest"
	"github.com/lnikonl/nfs4viv/resources"
	"github.com/lnikonl/nfs4viv/validation"
	"github.com/lnikonl/nfs4viv/viv"
)

func writeResourceCar(t *testing.T) string {
	t.Helper()
	data, err := cartest.ResourceArchive().Bytes()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "car.viv")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResourceCommands(t *testing.T) {
	for name, original := range cartest.ResourceFiles() {
		format := resourceFormat(name)
		if !resources.Supported(format) {
			continue
		}
		t.Run(format, func(t *testing.T) {
			root := t.TempDir()
			input := filepath.Join(root, name)
			if err := os.WriteFile(input, original, 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := captureStdout(t, func() error {
				return run([]string{format, "extract", input})
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(input+".d", resources.ManifestName)); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(root, "copy."+format)
			if _, err := captureStdout(t, func() error {
				return run([]string{format, "pack", input + ".d", "-o", output})
			}); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, original) {
				t.Fatal("standalone extract/pack changed bytes")
			}
		})
	}
}

func TestFCECreateCommand(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "new_car.obj")
	obj := []byte("o HB\nv -1 0 -1\nv 1 0 -1\nv 0 1 1\nvt 0 0\nvt 1 0\nvt 0.5 1\nf 1/1 2/2 3/3\n")
	if err := os.WriteFile(input, obj, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := captureStdout(t, func() error { return run([]string{"fce", "create", input, "--damaged", input}) }); err != nil {
		t.Fatal(err)
	}
	fcePath := filepath.Join(root, "new_car.fce")
	data, err := os.ReadFile(fcePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := resources.Validate("fce", data, 0); err != nil {
		t.Fatal(err)
	}
	archive := cartest.ResourceArchive()
	archive.Set("car.fce", data)
	report, err := validation.Check("car", archive, validation.Options{})
	if err != nil || !report.Valid {
		t.Fatalf("created FCE cannot be used in CAR: %v %+v", err, report)
	}
	directory := filepath.Join(root, "project")
	if _, err := captureStdout(t, func() error { return run([]string{"fce", "extract", fcePath, "-o", directory}) }); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "copy.fce")
	if _, err := captureStdout(t, func() error { return run([]string{"fce", "pack", directory, "-o", output}) }); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(output)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("created FCE project round trip: %v", err)
	}
}

func TestFedataCreateCommand(t *testing.T) {
	output := filepath.Join(t.TempDir(), "fedata.eng")
	if _, err := captureStdout(t, func() error {
		return run([]string{"fedata", "create", output, "car_id", "NEW1", "serial", "42", "car_name", "Aurora GT", "class", "A", "upgradable", "no"})
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	archive := cartest.ResourceArchive()
	archive.Set("fedata.eng", data)
	report, err := validation.Check("car", archive, validation.Options{})
	if err != nil || !report.Valid {
		t.Fatalf("created fedata: %v %+v", err, report)
	}
	text, err := captureStdout(t, func() error { return run([]string{"fedata", "show", output}) })
	if err != nil || !strings.Contains(text, "Aurora GT") {
		t.Fatalf("created fields not readable: %v %s", err, text)
	}
	if _, err := captureStdout(t, func() error { return run([]string{"fedata", "create", output, "serial", "not-a-number"}) }); err == nil {
		t.Fatal("accepted invalid field value")
	}
	after, _ := os.ReadFile(output)
	if !bytes.Equal(after, data) {
		t.Fatal("invalid create overwrote existing fedata")
	}
}

func TestVivSubRoundTripAndEdits(t *testing.T) {
	input := writeResourceCar(t)
	root := t.TempDir()
	directory := filepath.Join(root, "CAR")
	if _, err := captureStdout(t, func() error {
		return run([]string{"viv", "extract", input, "--sub", "--validate", "car", "-o", directory})
	}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"car.fce/model.obj", "car.fce/damaged.obj", "car.fce/model.mtl", "car.fce/original.fce", "cop.art/image_000.png",
		"dash.fsh/000_idx0.tga", "careng.bnk/sample_000.wav", "careng.ctb/curve_00.json", "careng.ltb/curve_00.json", "car00.tga",
	} {
		if _, err := os.Stat(filepath.Join(directory, filepath.FromSlash(path))); err != nil {
			t.Errorf("missing extracted resource %s: %v", path, err)
		}
	}
	output := filepath.Join(root, "copy.viv")
	if _, err := captureStdout(t, func() error {
		return run([]string{"viv", "pack", directory, "--sub", "--validate", "car", "-o", output})
	}); err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(output)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("sub round trip changed the VIV: %v", err)
	}
	if _, err := captureStdout(t, func() error {
		return run([]string{"viv", "pack", directory, "-o", output})
	}); err == nil || !strings.Contains(err.Error(), "--sub") {
		t.Fatalf("missing --sub was not diagnosed: %v", err)
	}
	model := filepath.Join(directory, "car.fce", "model.obj")
	obj, err := os.ReadFile(model)
	if err != nil {
		t.Fatal(err)
	}
	obj = bytes.Replace(obj, []byte("v 1 0 0\n"), []byte("v 2.5 0 0\n"), 1)
	if err := os.WriteFile(model, obj, 0o644); err != nil {
		t.Fatal(err)
	}
	wavPath := filepath.Join(directory, "careng.bnk", "sample_000.wav")
	wav, err := os.ReadFile(wavPath)
	if err != nil {
		t.Fatal(err)
	}
	wav = append(wav, 1, 2, 3, 4)
	binary.LittleEndian.PutUint32(wav[4:], uint32(len(wav)-8))
	binary.LittleEndian.PutUint32(wav[40:], uint32(len(wav)-44))
	if err := os.WriteFile(wavPath, wav, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := captureStdout(t, func() error {
		return run([]string{"viv", "pack", directory, "--sub", "--validate", "car", "-o", output})
	}); err != nil {
		t.Fatal(err)
	}
	archive, err := viv.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	fce, _ := archive.Get("car.fce")
	if binary.LittleEndian.Uint32(fce[0x2038:]) != 0x40200000 {
		t.Fatal("nested geometry edit did not reach CAR.VIV")
	}
	bnk, _ := archive.Get("careng.bnk")
	files, err := resources.Extract("bnk", bnk)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(files["sample_000.wav"], wav) {
		t.Fatal("resized nested WAV did not reach CAR.VIV")
	}
}

func TestVivValidateCommands(t *testing.T) {
	input := writeResourceCar(t)
	output, err := captureStdout(t, func() error {
		return run([]string{"viv", "validate", input, "--max-texture-size", "256", "-json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	var report validation.Report
	if err := json.Unmarshal([]byte(output), &report); err != nil || !report.Valid || report.Profile != "car" {
		t.Fatalf("report = %s, error = %v", output, err)
	}
	archive, err := viv.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	tga, _ := archive.Get("car00.tga")
	tga[16] = 24
	data, err := archive.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, data, 0o644); err != nil {
		t.Fatal(err)
	}
	output, err = captureStdout(t, func() error {
		return run([]string{"viv", "validate", input, "-json"})
	})
	if err == nil {
		t.Fatal("bad CAR was accepted")
	}
	if err := json.Unmarshal([]byte(output), &report); err != nil || report.Valid {
		t.Fatalf("bad CAR JSON report missing: %s %v", output, err)
	}
	directory := filepath.Join(t.TempDir(), "invalid")
	if _, err := captureStdout(t, func() error {
		return run([]string{"viv", "extract", input, "--validate", "car", "-o", directory})
	}); err == nil {
		t.Fatal("bad CAR was extracted with validation enabled")
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("validation failure created the extraction directory")
	}
	if _, err := captureStdout(t, func() error {
		return run([]string{"viv", "extract", input, "-o", directory})
	}); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "existing.viv")
	marker := []byte("existing output must survive validation failure")
	if err := os.WriteFile(destination, marker, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := captureStdout(t, func() error {
		return run([]string{"viv", "pack", directory, "--validate", "car", "-o", destination})
	}); err == nil {
		t.Fatal("invalid CAR was packed with validation enabled")
	}
	got, err := os.ReadFile(destination)
	if err != nil || !bytes.Equal(got, marker) {
		t.Fatal("failed validation overwrote the output")
	}
}

func TestOriginalCarSubRoundTrips(t *testing.T) {
	root := os.Getenv("NFS4_CARS")
	if root == "" {
		for _, candidate := range []string{"../../original/nfs4viv/DATA/CARS", "../../../nfs4net/original/NFS4/DATA/CARS"} {
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
		if entry.IsDir() || !strings.EqualFold(entry.Name(), "car.viv") || !isVivFile(path) {
			return nil
		}
		count++
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		t.Run(relative, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "CAR")
			if _, err := captureStdout(t, func() error {
				return run([]string{"viv", "extract", path, "--sub", "--validate", "car", "--max-texture-size", "256", "-o", directory})
			}); err != nil {
				t.Fatalf("extract: %v", err)
			}
			if _, err := captureStdout(t, func() error {
				return run([]string{"viv", "pack", directory, "--sub", "--validate", "car"})
			}); err != nil {
				t.Fatalf("pack: %v", err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(directory + ".viv")
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("original CAR --sub round trip changed bytes")
			}
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Skip("no materialized original CAR archives")
	}
	t.Logf("validated and round-tripped %d original CAR archives with --sub", count)
}
