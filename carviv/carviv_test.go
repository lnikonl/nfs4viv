package carviv

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gitlab.babun.tv/nfs4/nfs4-tools/nfs4viv/fedata"
	"gitlab.babun.tv/nfs4/nfs4-tools/nfs4viv/viv"
)

func writeCarViv(t *testing.T, directory, carID string, serial uint16, name string, class byte) {
	t.Helper()
	file := fedata.New()
	for field, value := range map[string]string{
		"car_id":   carID,
		"serial":   strconv.Itoa(int(serial)),
		"car_name": name,
		"class":    fedata.ClassName(class),
		"price":    "350000",
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
	if err := os.WriteFile(filepath.Join(directory, "car.viv"), encoded, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestScan(t *testing.T) {
	root := t.TempDir()
	writeCarViv(t, filepath.Join(root, "car00"), "F50", 1, "Ferrari F50", 0)
	writeCarViv(t, filepath.Join(root, "car01"), "M5", 2, "BMW M5", 2)
	if err := os.MkdirAll(filepath.Join(root, "backup"), 0o755); err != nil {
		t.Fatal(err)
	}

	list, err := Scan(root, "")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("cars = %d, want 2", len(list))
	}
	first := list[0]
	if first.Folder != "car00" || first.CarID != "F50" || first.Serial != 1 || first.Name != "Ferrari F50" || first.Class != "AAA" || first.Price != 350000 {
		t.Errorf("unexpected first car: %+v", first)
	}
	if first.Entry != "fedata.eng" {
		t.Errorf("entry = %q", first.Entry)
	}
	if list[1].Folder != "car01" || list[1].Class != "A" {
		t.Errorf("unexpected second car: %+v", list[1])
	}
}

func TestScanSingleCarFolder(t *testing.T) {
	root := t.TempDir()
	writeCarViv(t, root, "911", 5, "Porsche 911 Turbo", 1)

	list, err := Scan(root, "")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(list) != 1 || list[0].Serial != 5 {
		t.Fatalf("list = %+v", list)
	}
}

func TestScanUppercaseCarViv(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "BZ3R")
	writeCarViv(t, directory, "BZ3R", 3, "BMW Z3", 3)
	if err := os.Rename(filepath.Join(directory, "car.viv"), filepath.Join(directory, "CAR.VIV")); err != nil {
		t.Fatal(err)
	}

	list, err := Scan(root, "")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(list) != 1 || list[0].Folder != "BZ3R" || list[0].CarID != "BZ3R" {
		t.Fatalf("list = %+v", list)
	}
	if !strings.HasSuffix(list[0].Viv, "CAR.VIV") {
		t.Errorf("viv = %q", list[0].Viv)
	}
}

func TestScanMissing(t *testing.T) {
	if _, err := Scan(t.TempDir(), ""); err == nil {
		t.Fatal("expected an error for a folder without car.viv")
	}
}

func TestFindFedataLanguages(t *testing.T) {
	archive := &viv.File{Entries: []viv.Entry{
		{Name: "fedata.fsh", Data: []byte("texture")},
		{Name: "fedata.ger", Data: []byte{4}},
		{Name: "fedata.eng", Data: []byte{4}},
		{Name: `car00\fedata.fre`, Data: []byte{4}},
		{Name: "fedata.spa", Data: []byte("not fedata data")},
	}}
	names := FindFedataLanguages(archive)
	want := []string{"fedata.eng", `car00\fedata.fre`, "fedata.ger", "fedata.spa"}
	if len(names) != len(want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
	for index := range want {
		if names[index] != want[index] {
			t.Errorf("name %d = %q, want %q", index, names[index], want[index])
		}
	}
}

func TestFindFedataLanguage(t *testing.T) {
	archive := &viv.File{Entries: []viv.Entry{
		{Name: "fedata.ger", Data: []byte{4}},
		{Name: "fedata.eng", Data: []byte{4, 1}},
	}}
	name, _, err := FindFedata(archive, "", "")
	if err != nil || name != "fedata.eng" {
		t.Fatalf("default language = %q, %v", name, err)
	}
	name, _, err = FindFedata(archive, "", "ger")
	if err != nil || name != "fedata.ger" {
		t.Fatalf("german language = %q, %v", name, err)
	}
	if _, _, err := FindFedata(archive, "", "ita"); err == nil {
		t.Fatal("expected an error for a missing language")
	}
	name, _, err = FindFedata(archive, "FEDATA.GER", "")
	if err != nil || name != "fedata.ger" {
		t.Fatalf("explicit entry = %q, %v", name, err)
	}
}

func TestFindFedataNestedPath(t *testing.T) {
	archive := &viv.File{Entries: []viv.Entry{
		{Name: `car00\fedata.eng`, Data: []byte{4}},
	}}
	name, _, err := FindFedata(archive, "", "")
	if err != nil || name != `car00\fedata.eng` {
		t.Fatalf("nested entry = %q, %v", name, err)
	}
}

func TestSortByClassOrder(t *testing.T) {
	list := []Info{
		{Folder: "d", ClassRaw: 0, Class: "AAA"},
		{Folder: "a", ClassRaw: 3, Class: "B"},
		{Folder: "c", ClassRaw: 1, Class: "AA"},
		{Folder: "b", ClassRaw: 2, Class: "A"},
	}
	if err := SortBy(list, "class"); err != nil {
		t.Fatalf("SortBy: %v", err)
	}
	want := []string{"B", "A", "AA", "AAA"}
	for index, class := range want {
		if list[index].Class != class {
			t.Errorf("car %d class = %q, want %q", index, list[index].Class, class)
		}
	}
}

func TestSortBySecondaryKeys(t *testing.T) {
	list := []Info{
		{Folder: "car00", Name: "Zeta", Bonus: true, ClassRaw: 0, Class: "AAA", Serial: 9},
		{Folder: "car01", Name: "Alpha", Bonus: true, ClassRaw: 3, Class: "B", Serial: 1},
		{Folder: "car02", Name: "Beta", Bonus: false, ClassRaw: 2, Class: "A", Serial: 2},
	}
	if err := SortBy(list, "bonus"); err != nil {
		t.Fatalf("SortBy: %v", err)
	}
	want := []string{"car02", "car01", "car00"}
	for index, folder := range want {
		if list[index].Folder != folder {
			t.Errorf("car %d folder = %q, want %q", index, list[index].Folder, folder)
		}
	}
}

func TestSortByNameTieBreak(t *testing.T) {
	list := []Info{
		{Folder: "b", Name: "Zeta", ClassRaw: 0, Class: "AAA", Serial: 1},
		{Folder: "a", Name: "Alpha", ClassRaw: 0, Class: "AAA", Serial: 1},
	}
	if err := SortBy(list, "serial"); err != nil {
		t.Fatalf("SortBy: %v", err)
	}
	if list[0].Folder != "a" || list[1].Folder != "b" {
		t.Errorf("folders = %q, %q", list[0].Folder, list[1].Folder)
	}
}

func TestSortByUnknownColumn(t *testing.T) {
	if err := SortBy(nil, "nope"); err == nil {
		t.Fatal("expected an error for an unknown column")
	}
}

func TestMarkDuplicates(t *testing.T) {
	list := []Info{
		{Folder: "car00", CarID: "F50", Serial: 1, Name: "Ferrari F50", Class: "AAA"},
		{Folder: "car01", CarID: "M5", Serial: 1, Name: "BMW M5", Class: "A"},
		{Folder: "car02", CarID: "F50", Serial: 2, Name: "ferrari f50", Class: "AAA"},
		{Folder: "car03", CarID: "911", Serial: 3, Name: "Unique", Class: "B"},
	}
	MarkDuplicates(list)
	if got := strings.Join(list[0].Duplicates, ","); got != "serial,car_id,name" {
		t.Errorf("car00 duplicates = %q", got)
	}
	if got := strings.Join(list[1].Duplicates, ","); got != "serial" {
		t.Errorf("car01 duplicates = %q", got)
	}
	if got := strings.Join(list[2].Duplicates, ","); got != "car_id,name" {
		t.Errorf("car02 duplicates = %q", got)
	}
	if len(list[3].Duplicates) != 0 {
		t.Errorf("car03 duplicates = %v", list[3].Duplicates)
	}
}
