package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gitlab.babun.tv/nfs4/nfs4-tools/nfs4viv/cartest"
	"gitlab.babun.tv/nfs4/nfs4-tools/nfs4viv/carviv"
	"gitlab.babun.tv/nfs4/nfs4-tools/nfs4viv/fedata"
	"gitlab.babun.tv/nfs4/nfs4-tools/nfs4viv/viv"
)

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	previous := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	os.Stdout = writer
	defer func() {
		os.Stdout = previous
		writer.Close()
	}()
	runErr := fn()
	writer.Close()
	os.Stdout = previous
	data, readErr := io.ReadAll(reader)
	if readErr != nil {
		t.Fatalf("ReadAll: %v", readErr)
	}
	return string(data), runErr
}

func captureStderr(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	previous := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	os.Stderr = writer
	defer func() {
		os.Stderr = previous
		writer.Close()
	}()
	runErr := fn()
	writer.Close()
	os.Stderr = previous
	data, readErr := io.ReadAll(reader)
	if readErr != nil {
		t.Fatalf("ReadAll: %v", readErr)
	}
	return string(data), runErr
}

func createCarViv(t *testing.T, directory, carID string, serial uint16, name string, class byte) string {
	t.Helper()
	file := fedata.New()
	for field, value := range map[string]string{
		"car_id":     carID,
		"serial":     strconv.Itoa(int(serial)),
		"car_name":   name,
		"class":      fedata.ClassName(class),
		"police":     "no",
		"bonus":      "no",
		"upgradable": "yes",
		"price":      "350000",
	} {
		if err := file.Set(field, value); err != nil {
			t.Fatalf("Set(%s): %v", field, err)
		}
	}
	archive := &viv.File{Entries: []viv.Entry{
		{Name: "car.fce", Data: []byte{1, 2, 3}},
		{Name: "fedata.eng", Data: file.Bytes()},
	}}
	encoded, err := archive.Bytes()
	if err != nil {
		t.Fatalf("archive.Bytes: %v", err)
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(directory, "car.viv")
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestSerialsCommand(t *testing.T) {
	root := t.TempDir()
	createCarViv(t, filepath.Join(root, "car00"), "F50", 1, "Ferrari F50", 0)
	createCarViv(t, filepath.Join(root, "car01"), "M5", 2, "BMW M5", 3)

	output, err := captureStdout(t, func() error {
		return run([]string{"serials", "-json", root})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var list []carviv.Info
	if err := json.Unmarshal([]byte(output), &list); err != nil {
		t.Fatalf("Unmarshal(%q): %v", output, err)
	}
	if len(list) != 2 {
		t.Fatalf("cars = %d", len(list))
	}
	if list[0].CarID != "F50" || list[0].Serial != 1 || list[0].Name != "Ferrari F50" || list[0].Class != "AAA" {
		t.Errorf("first car = %+v", list[0])
	}
	if list[1].Class != "B" {
		t.Errorf("second car class = %q", list[1].Class)
	}
}

func TestSerialsDuplicateWarning(t *testing.T) {
	root := t.TempDir()
	createCarViv(t, filepath.Join(root, "car00"), "F50", 1, "Ferrari F50", 0)
	createCarViv(t, filepath.Join(root, "car01"), "M5", 1, "BMW M5", 3)

	stderr, err := captureStderr(t, func() error {
		_, _ = captureStdout(t, func() error {
			return run([]string{"serials", root})
		})
		return nil
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stderr, "serial 1 is used by car00, car01") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestSerialsCommandText(t *testing.T) {
	root := t.TempDir()
	createCarViv(t, filepath.Join(root, "car00"), "F50", 1, "Ferrari F50", 0)
	output, err := captureStdout(t, func() error {
		return run([]string{"serials", root})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"FOLDER", "CAR_ID", "SERIAL", "CLASS", "POLICE", "BONUS", "UPGRADABLE", "PRICE", "car00", "F50", "Ferrari F50"} {
		if !strings.Contains(output, want) {
			t.Errorf("output does not contain %q:\n%s", want, output)
		}
	}
}

func TestSerialsSortFlag(t *testing.T) {
	root := t.TempDir()
	createCarViv(t, filepath.Join(root, "car00"), "AAA1", 4, "Zeta", 0)
	createCarViv(t, filepath.Join(root, "car01"), "BBB1", 3, "Alpha", 3)
	createCarViv(t, filepath.Join(root, "car02"), "CCC1", 2, "Beta", 1)
	createCarViv(t, filepath.Join(root, "car03"), "DDD1", 1, "Gamma", 2)

	output, err := captureStdout(t, func() error {
		return run([]string{"serials", "-sort", "class", "-json", root})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var list []carviv.Info
	if err := json.Unmarshal([]byte(output), &list); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	wantClasses := []string{"B", "A", "AA", "AAA"}
	for index, class := range wantClasses {
		if list[index].Class != class {
			t.Errorf("car %d class = %q, want %q", index, list[index].Class, class)
		}
	}

	output, err = captureStdout(t, func() error {
		return run([]string{"serials", "-json", root})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if err := json.Unmarshal([]byte(output), &list); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for index, serial := range []uint16{1, 2, 3, 4} {
		if list[index].Serial != serial {
			t.Errorf("car %d serial = %d, want %d", index, list[index].Serial, serial)
		}
	}
}

func TestSerialsCheckFlag(t *testing.T) {
	root := t.TempDir()
	createCarViv(t, filepath.Join(root, "car00"), "F50", 1, "Ferrari F50", 0)
	createCarViv(t, filepath.Join(root, "car01"), "M5", 1, "Ferrari F50", 0)
	createCarViv(t, filepath.Join(root, "car02"), "911", 3, "Porsche 911", 1)

	var stderr string
	output, err := captureStdout(t, func() error {
		var runErr error
		stderr, runErr = captureStderr(t, func() error {
			return run([]string{"serials", "-check", "-json", root})
		})
		return runErr
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var list []carviv.Info
	if err := json.Unmarshal([]byte(output), &list); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("cars = %d", len(list))
	}
	for _, car := range list[:2] {
		if strings.Join(car.Duplicates, ",") != "serial,name" {
			t.Errorf("%s duplicates = %v", car.Folder, car.Duplicates)
		}
	}
	if len(list[2].Duplicates) != 0 {
		t.Errorf("car02 duplicates = %v", list[2].Duplicates)
	}
	if !strings.Contains(stderr, "share the name") {
		t.Errorf("stderr = %q", stderr)
	}
	if !strings.Contains(stderr, "serial 1 is used by") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestSerialsDefaultDirectory(t *testing.T) {
	root := t.TempDir()
	createCarViv(t, filepath.Join(root, "DATA", "CARS", "car00"), "F50", 1, "Ferrari F50", 0)

	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(previous)

	output, err := captureStdout(t, func() error {
		return run([]string{"serials", "-json"})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var list []carviv.Info
	if err := json.Unmarshal([]byte(output), &list); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(list) != 1 || list[0].Folder != "car00" {
		t.Fatalf("list = %+v", list)
	}
}

func TestSerialsColorAlways(t *testing.T) {
	root := t.TempDir()
	createCarViv(t, filepath.Join(root, "car00"), "F50", 1, "Ferrari F50", 0)
	createCarViv(t, filepath.Join(root, "car01"), "M5", 1, "BMW M5", 0)

	output, err := captureStdout(t, func() error {
		return run([]string{"serials", "-check", "-color", "always", root})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := strings.Count(output, ansiConflict); got != 2 {
		t.Errorf("highlights = %d, want 2:\n%q", got, output)
	}
	if !strings.Contains(output, ansiConflict+"1"+ansiReset) {
		t.Errorf("serial value not highlighted:\n%q", output)
	}
	if strings.Contains(output, ansiConflict+"Ferrari F50"+ansiReset) || strings.Contains(output, ansiConflict+"BMW M5"+ansiReset) {
		t.Errorf("names must not be highlighted:\n%q", output)
	}
	if !strings.Contains(output, "SERIAL") || !strings.Contains(output, "DUPLICATES") {
		t.Errorf("output = %q", output)
	}
}

func TestSerialsColorNameDuplicate(t *testing.T) {
	root := t.TempDir()
	createCarViv(t, filepath.Join(root, "car00"), "F50", 1, "Ferrari F50", 0)
	createCarViv(t, filepath.Join(root, "car01"), "M5", 2, "Ferrari F50", 0)

	output, err := captureStdout(t, func() error {
		return run([]string{"serials", "-check", "-color", "always", root})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := strings.Count(output, ansiConflict); got != 2 {
		t.Errorf("highlights = %d, want 2:\n%q", got, output)
	}
	if !strings.Contains(output, ansiConflict+"Ferrari F50"+ansiReset) {
		t.Errorf("name value not highlighted:\n%q", output)
	}
	if strings.Contains(output, ansiConflict+"1"+ansiReset) || strings.Contains(output, ansiConflict+"2"+ansiReset) {
		t.Errorf("serials must not be highlighted:\n%q", output)
	}
}

func TestSerialsColorModes(t *testing.T) {
	root := t.TempDir()
	createCarViv(t, filepath.Join(root, "car00"), "F50", 1, "Ferrari F50", 0)
	createCarViv(t, filepath.Join(root, "car01"), "M5", 1, "BMW M5", 0)

	output, err := captureStdout(t, func() error {
		return run([]string{"serials", "-check", "-color", "never", root})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(output, "\x1b[") {
		t.Errorf("colors must be disabled with -color never:\n%q", output)
	}

	output, err = captureStdout(t, func() error {
		return run([]string{"serials", "-check", root})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(output, "\x1b[") {
		t.Errorf("auto mode must not color a pipe:\n%q", output)
	}

	if _, err := captureStdout(t, func() error {
		return run([]string{"serials", "-color", "sometimes", root})
	}); err == nil {
		t.Error("expected an error for an invalid -color value")
	}
}

func TestSerialsCheckEnabledByDefault(t *testing.T) {
	root := t.TempDir()
	createCarViv(t, filepath.Join(root, "car00"), "F50", 1, "Ferrari F50", 0)
	createCarViv(t, filepath.Join(root, "car01"), "M5", 1, "BMW M5", 0)

	output, err := captureStdout(t, func() error {
		return run([]string{"serials", "-json", root})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var list []carviv.Info
	if err := json.Unmarshal([]byte(output), &list); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("cars = %d", len(list))
	}
	for _, car := range list {
		if strings.Join(car.Duplicates, ",") != "serial" {
			t.Errorf("%s duplicates = %v", car.Folder, car.Duplicates)
		}
	}

	table, err := captureStdout(t, func() error {
		return run([]string{"serials", root})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(table, "DUPLICATES") {
		t.Errorf("table = %q", table)
	}
}

func TestSerialsCheckOff(t *testing.T) {
	root := t.TempDir()
	createCarViv(t, filepath.Join(root, "car00"), "F50", 1, "Ferrari F50", 0)
	createCarViv(t, filepath.Join(root, "car01"), "M5", 1, "BMW M5", 0)

	for _, args := range [][]string{
		{"serials", "-check-off", "-json", root},
		{"serials", "-check=false", "-json", root},
	} {
		output, err := captureStdout(t, func() error {
			return run(args)
		})
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		var list []carviv.Info
		if err := json.Unmarshal([]byte(output), &list); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		for _, car := range list {
			if len(car.Duplicates) != 0 {
				t.Errorf("%v: %s duplicates = %v", args, car.Folder, car.Duplicates)
			}
		}
	}

	table, err := captureStdout(t, func() error {
		return run([]string{"serials", "-check-off", root})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(table, "DUPLICATES") {
		t.Errorf("table = %q", table)
	}
}

func TestSerialsColorCarIDDuplicate(t *testing.T) {
	root := t.TempDir()
	createCarViv(t, filepath.Join(root, "BZ3R"), "BZ3R", 3, "BMW Z3", 3)
	createCarViv(t, filepath.Join(root, "BZ3R2"), "BZ3R", 4, "BMW M5", 3)

	var stderr string
	output, err := captureStdout(t, func() error {
		var runErr error
		stderr, runErr = captureStderr(t, func() error {
			return run([]string{"serials", "-color", "always", root})
		})
		return runErr
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := strings.Count(output, ansiConflict); got != 2 {
		t.Errorf("highlights = %d, want 2:\n%q", got, output)
	}
	if !strings.Contains(output, ansiConflict+"BZ3R"+ansiReset) {
		t.Errorf("car ID value not highlighted:\n%q", output)
	}
	if strings.Contains(output, ansiConflict+"BMW Z3"+ansiReset) || strings.Contains(output, ansiConflict+"BMW M5"+ansiReset) {
		t.Errorf("names must not be highlighted:\n%q", output)
	}
	if !strings.Contains(stderr, `share the car ID "BZ3R"`) {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestSerialsCarIDInJSON(t *testing.T) {
	root := t.TempDir()
	createCarViv(t, filepath.Join(root, "car00"), "F50", 1, "Ferrari F50", 0)
	createCarViv(t, filepath.Join(root, "car01"), "F50", 2, "BMW M5", 0)

	output, err := captureStdout(t, func() error {
		return run([]string{"serials", "-json", root})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var list []carviv.Info
	if err := json.Unmarshal([]byte(output), &list); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, car := range list {
		if strings.Join(car.Duplicates, ",") != "car_id" {
			t.Errorf("%s duplicates = %v", car.Folder, car.Duplicates)
		}
	}
}

func createMultilingualCarViv(t *testing.T, directory string, names map[string]string) string {
	t.Helper()
	entries := []viv.Entry{
		{Name: "car.fce", Data: []byte{1, 2, 3}},
		{Name: "fedata.fsh", Data: []byte("texture data")},
	}
	for _, language := range []string{"eng", "bri", "fre", "ger", "ita", "spa", "swe"} {
		carName, ok := names[language]
		if !ok {
			continue
		}
		file := fedata.New()
		for field, value := range map[string]string{"car_id": "F50", "serial": "1", "car_name": carName} {
			if err := file.Set(field, value); err != nil {
				t.Fatalf("Set(%s): %v", field, err)
			}
		}
		entries = append(entries, viv.Entry{Name: "fedata." + language, Data: file.Bytes()})
	}
	archive := &viv.File{Entries: entries}
	encoded, err := archive.Bytes()
	if err != nil {
		t.Fatalf("archive.Bytes: %v", err)
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(directory, "car.viv")
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestFedataSetAllLanguages(t *testing.T) {
	root := t.TempDir()
	path := createMultilingualCarViv(t, filepath.Join(root, "car00"), map[string]string{
		"eng": "Ferrari F50",
		"ger": "Ferrari F50 (DE)",
		"fre": "Ferrari F50 (FR)",
	})

	output, err := captureStdout(t, func() error {
		return run([]string{"fedata", "set", "-lang", "all", path, "serial", "9", "car_name", "Ferrari F50 GT"})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(output, "3 entries") {
		t.Errorf("output = %q", output)
	}

	archive, err := viv.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"fedata.eng", "fedata.fre", "fedata.ger"} {
		index, ok := archive.Find(name)
		if !ok {
			t.Fatalf("entry %s missing", name)
		}
		parsed, err := fedata.Parse(archive.Entries[index].Data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if parsed.Serial() != 9 || parsed.CarName() != "Ferrari F50 GT" {
			t.Errorf("%s = serial %d, name %q", name, parsed.Serial(), parsed.CarName())
		}
	}
	if data, _ := archive.Get("fedata.fsh"); string(data) != "texture data" {
		t.Errorf("fedata.fsh = %q", data)
	}
	if _, ok := archive.Find("car.fce"); !ok {
		t.Error("car.fce must survive the rewrite")
	}
}

func TestFedataShowAllLanguages(t *testing.T) {
	root := t.TempDir()
	path := createMultilingualCarViv(t, filepath.Join(root, "car00"), map[string]string{
		"eng": "Ferrari F50",
		"ger": "Ferrari F50 (DE)",
	})

	output, err := captureStdout(t, func() error {
		return run([]string{"fedata", "show", "-lang", "all", "-json", path})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(output), &list); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("entries = %d, want 2", len(list))
	}
	if list[0]["entry"] != "fedata.eng" || list[1]["entry"] != "fedata.ger" {
		t.Errorf("entries = %v, %v", list[0]["entry"], list[1]["entry"])
	}
	if list[0]["name"] != "Ferrari F50" || list[1]["name"] != "Ferrari F50 (DE)" {
		t.Errorf("names = %v, %v", list[0]["name"], list[1]["name"])
	}

	output, err = captureStdout(t, func() error {
		return run([]string{"fedata", "show", "-lang", "all", path})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(output, "entry: fedata.eng") || !strings.Contains(output, "entry: fedata.ger") {
		t.Errorf("output = %q", output)
	}
}

func TestFedataLangAllErrors(t *testing.T) {
	root := t.TempDir()
	file := fedata.New()
	if err := file.Set("car_name", "Ferrari F50"); err != nil {
		t.Fatal(err)
	}
	raw := filepath.Join(root, "fedata.eng")
	if err := os.WriteFile(raw, file.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := captureStdout(t, func() error {
		return run([]string{"fedata", "set", "-lang", "all", raw, "serial", "9"})
	}); err == nil {
		t.Error("expected -lang all to require a VIV archive")
	}

	path := createMultilingualCarViv(t, filepath.Join(root, "car00"), map[string]string{"eng": "Ferrari F50"})
	if _, err := captureStdout(t, func() error {
		return run([]string{"fedata", "set", "-lang", "all", "-entry", "fedata.eng", path, "serial", "9"})
	}); err == nil {
		t.Error("expected -lang all with -entry to fail")
	}

	if _, err := captureStdout(t, func() error {
		return run([]string{"serials", "-lang", "all", root})
	}); err == nil {
		t.Error("expected serials to reject -lang all")
	}
}

func withStdin(t *testing.T, data []byte, fn func() error) error {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "stdin-*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	if _, err := file.Write(data); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	previous := os.Stdin
	os.Stdin = file
	defer func() {
		os.Stdin = previous
		file.Close()
	}()
	return fn()
}

func TestFedataExportSingle(t *testing.T) {
	root := t.TempDir()
	path := createCarViv(t, filepath.Join(root, "car00"), "F50", 1, "Ferrari F50", 0)

	output, err := captureStdout(t, func() error {
		return run([]string{"fedata", "export", path})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(output), &fields); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	want := map[string]any{
		"entry":    "fedata.eng",
		"car_id":   "F50",
		"serial":   "1",
		"car_name": "Ferrari F50",
		"class":    "AAA",
		"price":    "350000",
	}
	for key, value := range want {
		if fields[key] != value {
			t.Errorf("%s = %v, want %v", key, fields[key], value)
		}
	}
	if len(fields) != len(fedata.AllFields())+1 {
		t.Errorf("fields = %d, want %d", len(fields), len(fedata.AllFields())+1)
	}

	exportPath := filepath.Join(root, "car.json")
	if _, err := captureStdout(t, func() error {
		return run([]string{"fedata", "export", "-o", exportPath, path})
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
	data, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) {
		t.Errorf("export file is not valid JSON")
	}
}

func TestFedataExportImportAllLanguages(t *testing.T) {
	root := t.TempDir()
	path := createMultilingualCarViv(t, filepath.Join(root, "car00"), map[string]string{
		"eng": "Ferrari F50",
		"ger": "Ferrari F50 (DE)",
		"fre": "Ferrari F50 (FR)",
	})

	output, err := captureStdout(t, func() error {
		return run([]string{"fedata", "export", "-lang", "all", path})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(output), &list); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("entries = %d, want 3", len(list))
	}
	if list[0]["entry"] != "fedata.eng" || list[1]["entry"] != "fedata.fre" || list[2]["entry"] != "fedata.ger" {
		t.Fatalf("entries = %v, %v, %v", list[0]["entry"], list[1]["entry"], list[2]["entry"])
	}

	for _, item := range list {
		item["serial"] = 9
		item["car_name"] = "Ferrari F50 GT"
		item["bonus"] = true
	}
	edited, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	// Importing the array back does not need -lang all: the entry names in
	// the document select the targets.
	if err := withStdin(t, edited, func() error {
		return run([]string{"fedata", "import", path})
	}); err != nil {
		t.Fatalf("import: %v", err)
	}

	output, err = captureStdout(t, func() error {
		return run([]string{"fedata", "export", "-lang", "all", path})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if err := json.Unmarshal([]byte(output), &list); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, item := range list {
		if item["serial"] != "9" || item["car_name"] != "Ferrari F50 GT" || item["bonus"] != "yes" {
			t.Errorf("%v = serial %v, name %v, bonus %v", item["entry"], item["serial"], item["car_name"], item["bonus"])
		}
	}
}

func TestFedataImportErrors(t *testing.T) {
	root := t.TempDir()
	path := createMultilingualCarViv(t, filepath.Join(root, "car00"), map[string]string{
		"eng": "Ferrari F50",
		"ger": "Ferrari F50 (DE)",
	})
	cases := []struct {
		name string
		args []string
		json string
	}{
		{"unknown field", []string{"fedata", "import", path}, `{"nope": "1"}`},
		{"invalid json", []string{"fedata", "import", path}, `{`},
		{"array for single entry", []string{"fedata", "import", path}, `[{"serial": "1"}, {"serial": "2"}]`},
		{"entry mismatch", []string{"fedata", "import", "-lang", "eng", path}, `{"entry": "fedata.ger", "serial": "1"}`},
		{"missing entry name", []string{"fedata", "import", "-lang", "all", path}, `[{"serial": "1"}]`},
		{"unknown entry name", []string{"fedata", "import", "-lang", "all", path}, `[{"entry": "fedata.ita", "serial": "1"}]`},
		{"nested value", []string{"fedata", "import", path}, `{"serial": {"a": 1}}`},
	}
	for _, test := range cases {
		if err := withStdin(t, []byte(test.json), func() error {
			return run(test.args)
		}); err == nil {
			t.Errorf("%s: expected an error", test.name)
		}
	}
}

func TestFedataExportImportRoundTrip(t *testing.T) {
	root := t.TempDir()
	path := createMultilingualCarViv(t, filepath.Join(root, "car00"), map[string]string{
		"eng": "Ferrari F50",
		"ger": "Ferrari F50 (DE)",
	})

	first := filepath.Join(root, "first.json")
	second := filepath.Join(root, "second.json")
	copyPath := filepath.Join(root, "copy.viv")

	if _, err := captureStdout(t, func() error {
		return run([]string{"fedata", "export", "-lang", "all", "-o", first, path})
	}); err != nil {
		t.Fatalf("export: %v", err)
	}
	if _, err := captureStdout(t, func() error {
		return run([]string{"fedata", "import", "-lang", "all", "-o", copyPath, path, first})
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
		t.Errorf("round trip changed the export:\n%s\n%s", before, after)
	}
}

func TestExportImportGeneratedCars(t *testing.T) {
	root := filepath.Join(t.TempDir(), "CARS")
	if err := cartest.Write(root, cartest.Cars()); err != nil {
		t.Fatalf("cartest.Write: %v", err)
	}
	for _, car := range cartest.Cars() {
		t.Run(car.Folder, func(t *testing.T) {
			name := "CAR.VIV"
			if car.LowercaseViv {
				name = "car.viv"
			}
			vivPath := filepath.Join(root, car.Folder, name)
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
			if len(list) != len(cartest.Languages) {
				t.Errorf("%s: exported %d languages, want %d", car.Folder, len(list), len(cartest.Languages))
			}
		})
	}
}

func TestSerialsGeneratedCars(t *testing.T) {
	root := filepath.Join(t.TempDir(), "CARS")
	if err := cartest.Write(root, cartest.Cars()); err != nil {
		t.Fatalf("cartest.Write: %v", err)
	}
	output, err := captureStdout(t, func() error {
		return run([]string{"serials", "-json", root})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var list []carviv.Info
	if err := json.Unmarshal([]byte(output), &list); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(list) != len(cartest.Cars()) {
		t.Fatalf("cars = %d, want %d", len(list), len(cartest.Cars()))
	}
	positions := map[string]carviv.Info{}
	for _, car := range list {
		positions[car.Folder] = car
	}
	if positions["FLCN"].PoliceFlag != 0xA0 || positions["FLCN"].Price != 225000 {
		t.Errorf("FLCN = %+v", positions["FLCN"])
	}
	if positions["NOVA"].PoliceFlag != 0x10 || positions["NOVA"].Name != "Nova Pursuit" {
		t.Errorf("NOVA = %+v", positions["NOVA"])
	}
	if positions["CMET"].Class != "unknown (0x07)" || !positions["CMET"].Bonus {
		t.Errorf("CMET = %+v", positions["CMET"])
	}
}

func TestFedataSetCommand(t *testing.T) {
	root := t.TempDir()
	path := createCarViv(t, filepath.Join(root, "car00"), "F50", 1, "Ferrari F50", 0)

	_, err := captureStdout(t, func() error {
		return run([]string{"fedata", "set", path, "serial", "42", "car_name", "Ferrari F50 GT", "bonus", "yes", "price", "500000"})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	archive, err := viv.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	entry, data, err := carviv.FindFedata(archive, "", "")
	if err != nil {
		t.Fatalf("FindFedata: %v", err)
	}
	if entry != "fedata.eng" {
		t.Fatalf("entry = %q", entry)
	}
	parsed, err := fedata.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Serial() != 42 {
		t.Errorf("serial = %d", parsed.Serial())
	}
	if parsed.CarName() != "Ferrari F50 GT" {
		t.Errorf("car name = %q", parsed.CarName())
	}
	if !parsed.Bonus() {
		t.Error("bonus flag not set")
	}
	if parsed.BasePrice() != 500000 {
		t.Errorf("price = %d", parsed.BasePrice())
	}
	if _, ok := archive.Find("car.fce"); !ok {
		t.Error("other VIV entries must survive a rewrite")
	}
}

func TestFedataSetOutputFile(t *testing.T) {
	root := t.TempDir()
	path := createCarViv(t, filepath.Join(root, "car00"), "F50", 1, "Ferrari F50", 0)
	output := filepath.Join(root, "patched.viv")

	if _, err := captureStdout(t, func() error {
		return run([]string{"fedata", "set", "-o", output, path, "serial", "7"})
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
	original, err := viv.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	patched, err := viv.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	_, originalData, _ := carviv.FindFedata(original, "", "")
	_, patchedData, _ := carviv.FindFedata(patched, "", "")
	before, _ := fedata.Parse(originalData)
	after, _ := fedata.Parse(patchedData)
	if before.Serial() != 1 || after.Serial() != 7 {
		t.Fatalf("serials = %d -> %d", before.Serial(), after.Serial())
	}
}

func TestFedataShowCommand(t *testing.T) {
	root := t.TempDir()
	path := createCarViv(t, filepath.Join(root, "car00"), "F50", 1, "Ferrari F50", 0)
	output, err := captureStdout(t, func() error {
		return run([]string{"fedata", "show", "-json", path})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(output), &fields); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if fields["car_id"] != "F50" || fields["name"] != "Ferrari F50" {
		t.Errorf("fields = %+v", fields)
	}
}

func TestVivCommands(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "car00")
	path := createCarViv(t, directory, "F50", 1, "Ferrari F50", 0)
	replacement := filepath.Join(root, "new.fce")
	if err := os.WriteFile(replacement, []byte("new geometry"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := captureStdout(t, func() error {
		return run([]string{"viv", "replace", path, "car.fce", replacement})
	}); err != nil {
		t.Fatalf("replace: %v", err)
	}
	archive, err := viv.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := archive.Get("car.fce"); string(data) != "new geometry" {
		t.Errorf("car.fce = %q", data)
	}

	if _, err := captureStdout(t, func() error {
		return run([]string{"viv", "add", path, "extra.bnk", replacement})
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := captureStdout(t, func() error {
		return run([]string{"viv", "rm", path, "extra.bnk"})
	}); err != nil {
		t.Fatalf("rm: %v", err)
	}
	archive, err = viv.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := archive.Find("extra.bnk"); ok {
		t.Error("extra.bnk must be removed")
	}

	entryOutput, err := captureStdout(t, func() error {
		return run([]string{"viv", "read", path, "car.fce"})
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if entryOutput != "new geometry" {
		t.Errorf("read output = %q", entryOutput)
	}

	outDirectory := filepath.Join(root, "out")
	if _, err := captureStdout(t, func() error {
		return run([]string{"viv", "extract", path, "-o", outDirectory})
	}); err != nil {
		t.Fatalf("extract: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDirectory, "fedata.eng")); err != nil {
		t.Errorf("extracted fedata.eng missing: %v", err)
	}
}

func TestVivListCommand(t *testing.T) {
	root := t.TempDir()
	path := createCarViv(t, filepath.Join(root, "car00"), "F50", 1, "Ferrari F50", 0)
	output, err := captureStdout(t, func() error {
		return run([]string{"viv", "ls", path})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(output, "fedata.eng") || !strings.Contains(output, "car.fce") {
		t.Errorf("output = %q", output)
	}
}
