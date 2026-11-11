package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lnikonl/nfs4viv/cartest"
	"github.com/lnikonl/nfs4viv/viv"
)

func assertVivExtractPackRoundTrip(t *testing.T, input string) {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "extracted")
	if _, err := captureStdout(t, func() error {
		return run([]string{"viv", "extract", input, "-o", directory})
	}); err != nil {
		t.Fatalf("extract: %v", err)
	}
	if _, err := captureStdout(t, func() error {
		return run([]string{"viv", "pack", directory})
	}); err != nil {
		t.Fatalf("pack: %v", err)
	}
	before, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(directory + ".viv")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("extract/pack changed archive bytes (%d -> %d)", len(before), len(after))
	}
}

// This fixture has nonzero gaps, trailing bytes, a noncanonical header, data
// ordered differently from the directory, duplicate names and colliding basenames.
func unusualViv(t *testing.T) []byte {
	t.Helper()
	archive := &viv.File{Entries: []viv.Entry{
		{Name: `z\car.fce`, Data: []byte("first")},
		{Name: "a/car.fce", Data: []byte("second")},
		{Name: `z\car.fce`, Data: []byte("duplicate")},
		{Name: "CAR.FCE", Data: []byte("case collision")},
		{Name: vivManifestName, Data: []byte("not metadata")},
		{Name: "empty.bnk"},
		{Name: string([]byte{0xff}) + "/raw" + string([]byte{0xfe}) + ".bin", Data: []byte("raw name")},
		{Name: "..", Data: []byte("parent name")},
	}}
	canonical, err := archive.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	directoryEnd := int(binary.BigEndian.Uint32(canonical[12:16]))
	data := make([]byte, 2048)
	for index := range data {
		data[index] = byte(index*37 + 11)
	}
	copy(data, canonical[:directoryEnd])
	binary.BigEndian.PutUint32(data[4:8], uint32(len(data)-11))
	binary.BigEndian.PutUint32(data[12:16], uint32(directoryEnd+32))
	position := 16
	for index, entry := range archive.Entries {
		offset := 1024 + 64*(len(archive.Entries)-index)
		binary.BigEndian.PutUint32(data[position:position+4], uint32(offset))
		copy(data[offset:], entry.Data)
		position += 9 + len(entry.Name)
	}
	return data
}

func overlappingViv(t *testing.T) []byte {
	t.Helper()
	data := unusualViv(t)
	firstOffset := binary.BigEndian.Uint32(data[16:20])
	secondPosition := 16 + 9 + len(`z\car.fce`)
	binary.BigEndian.PutUint32(data[secondPosition:secondPosition+4], firstOffset+1)
	return data
}

func TestVivExtractPackExactLayout(t *testing.T) {
	empty, err := (&viv.File{}).Bytes()
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"unusual": unusualViv(t), "overlapping": overlappingViv(t), "empty": empty} {
		t.Run(name, func(t *testing.T) {
			input := filepath.Join(t.TempDir(), "original.viv")
			if err := os.WriteFile(input, data, 0o644); err != nil {
				t.Fatal(err)
			}
			assertVivExtractPackRoundTrip(t, input)
		})
	}
}

func TestVivExtractPackGeneratedCars(t *testing.T) {
	root := t.TempDir()
	if err := cartest.Write(root, cartest.Cars()); err != nil {
		t.Fatal(err)
	}
	for _, car := range cartest.Cars() {
		t.Run(car.Folder, func(t *testing.T) {
			name := "CAR.VIV"
			if car.LowercaseViv {
				name = "car.viv"
			}
			assertVivExtractPackRoundTrip(t, filepath.Join(root, car.Folder, name))
		})
	}
}

func TestVivPackEditedDirectory(t *testing.T) {
	for _, change := range []string{"same size", "resize", "add", "remove", "overlap"} {
		t.Run(change, func(t *testing.T) {
			root := t.TempDir()
			input := filepath.Join(root, "original.viv")
			original := unusualViv(t)
			if change == "overlap" {
				original = overlappingViv(t)
			}
			if err := os.WriteFile(input, original, 0o644); err != nil {
				t.Fatal(err)
			}
			directory := filepath.Join(root, "extracted")
			if _, err := captureStdout(t, func() error {
				return run([]string{"viv", "extract", input, "-o", directory})
			}); err != nil {
				t.Fatal(err)
			}
			manifest, err := readVivManifest(directory)
			if err != nil {
				t.Fatal(err)
			}
			want, err := viv.Read(original)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, manifest.Entries[1].File)
			switch change {
			case "same size", "resize", "overlap":
				data := []byte("edited")
				if change == "resize" {
					data = []byte("edited with a different length")
				}
				if err := os.WriteFile(path, data, 0o644); err != nil {
					t.Fatal(err)
				}
				want.Entries[1].Data = data
			case "add":
				if err := os.Mkdir(filepath.Join(directory, "sub"), 0o755); err != nil {
					t.Fatal(err)
				}
				data := []byte("new file")
				if err := os.WriteFile(filepath.Join(directory, "sub", "extra.bin"), data, 0o644); err != nil {
					t.Fatal(err)
				}
				want.Entries = append(want.Entries, viv.Entry{Name: "sub/extra.bin", Data: data})
			case "remove":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				want.Entries = append(want.Entries[:1], want.Entries[2:]...)
			}
			output := filepath.Join(root, "packed.viv")
			if _, err := captureStdout(t, func() error {
				return run([]string{"viv", "pack", "-o", output, directory})
			}); err != nil {
				t.Fatal(err)
			}
			got, err := viv.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Entries) != len(want.Entries) {
				t.Fatalf("entry count = %d, want %d", len(got.Entries), len(want.Entries))
			}
			for index, entry := range want.Entries {
				if got.Entries[index].Name != entry.Name || !bytes.Equal(got.Entries[index].Data, entry.Data) {
					t.Errorf("entry %d = %+v, want %+v", index, got.Entries[index], entry)
				}
			}
			if change == "same size" {
				encoded, err := os.ReadFile(output)
				if err != nil {
					t.Fatal(err)
				}
				offset := manifest.Entries[1].Offset
				copy(original[offset:], []byte("edited"))
				if !bytes.Equal(encoded, original) {
					t.Error("same-size edit changed unrelated layout bytes")
				}
			}
		})
	}
}

func TestVivPackPlainDirectory(t *testing.T) {
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"z.bin": "last", "sub/a.bin": "nested", "empty": ""} {
		if err := os.WriteFile(filepath.Join(directory, filepath.FromSlash(name)), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	output := filepath.Join(directory, "packed.viv")
	var first []byte
	for iteration := 0; iteration < 2; iteration++ {
		if _, err := captureStdout(t, func() error {
			return run([]string{"viv", "pack", directory, "-o", output})
		}); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		if iteration == 0 {
			first = data
		} else if !bytes.Equal(first, data) {
			t.Error("repacking included the output archive or changed the order")
		}
		archive, err := viv.Read(data)
		if err != nil {
			t.Fatal(err)
		}
		if len(archive.Entries) != 3 {
			t.Fatalf("entries = %d, want 3", len(archive.Entries))
		}
		for index, name := range []string{"empty", "sub/a.bin", "z.bin"} {
			if archive.Entries[index].Name != name {
				t.Errorf("entry %d name = %q, want %q", index, archive.Entries[index].Name, name)
			}
		}
	}
}

func TestVivPartialExtract(t *testing.T) {
	root := t.TempDir()
	input := createCarViv(t, filepath.Join(root, "car"), "F50", 1, "Test", 0)
	directory := filepath.Join(root, "selected")
	if _, err := captureStdout(t, func() error {
		return run([]string{"viv", "extract", input, "CAR.FCE", "-o", directory})
	}); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name() != "car.fce" {
		t.Fatalf("partial extract files = %v", files)
	}
	if _, err := captureStdout(t, func() error {
		return run([]string{"viv", "pack", directory})
	}); err != nil {
		t.Fatal(err)
	}
	archive, err := viv.ReadFile(directory + ".viv")
	if err != nil {
		t.Fatal(err)
	}
	if len(archive.Entries) != 1 || archive.Entries[0].Name != "car.fce" {
		t.Errorf("packed entries = %+v", archive.Entries)
	}
}

func TestVivPackRejectsBadManifest(t *testing.T) {
	for name, data := range map[string]string{
		"invalid json": "{",
		"version":      `{"version": 2}`,
		"parent path":  `{"version": 1, "entries": [{"file": "../outside"}]}`,
		"metadata":     `{"version": 1, "entries": [{"file": ".nfs4viv.json"}]}`,
		"duplicate":    `{"version": 1, "entries": [{"file": "a"}, {"file": "A"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			if err := os.WriteFile(filepath.Join(directory, vivManifestName), []byte(data), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := captureStdout(t, func() error {
				return run([]string{"viv", "pack", directory})
			})
			if err == nil || !strings.Contains(err.Error(), vivManifestName) {
				t.Fatalf("expected manifest error, got %v", err)
			}
		})
	}
}
