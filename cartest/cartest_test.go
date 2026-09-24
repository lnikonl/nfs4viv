package cartest

import (
	"os"
	"path/filepath"
	"testing"

	"gitlab.babun.tv/nfs4/nfs4-tools/nfs4viv/carviv"
	"gitlab.babun.tv/nfs4/nfs4-tools/nfs4viv/fedata"
	"gitlab.babun.tv/nfs4/nfs4-tools/nfs4viv/viv"
)

func TestWriteAndScan(t *testing.T) {
	root := t.TempDir()
	if err := Write(root, Cars()); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// The lower case fixture must be written as car.viv, the others as
	// CAR.VIV, and every car must carry all seven localizations.
	for _, car := range Cars() {
		name := "CAR.VIV"
		if car.LowercaseViv {
			name = "car.viv"
		}
		path := filepath.Join(root, car.Folder, name)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s: %v", path, err)
			continue
		}
		archive, err := viv.ReadFile(path)
		if err != nil {
			t.Errorf("%s: %v", path, err)
			continue
		}
		names := carviv.FindFedataLanguages(archive)
		if len(names) != len(Languages) {
			t.Errorf("%s: %d languages, want %d", path, len(names), len(Languages))
		}
		if _, ok := archive.Find("fedata.fsh"); !ok {
			t.Errorf("%s: fedata.fsh missing", path)
		}
	}

	list, err := carviv.Scan(root, "")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(list) != len(Cars()) {
		t.Fatalf("cars = %d, want %d", len(list), len(Cars()))
	}
	positions := map[string]carviv.Info{}
	for _, car := range list {
		positions[car.Folder] = car
	}
	falcon := positions["FLCN"]
	if falcon.CarID != "FLCN" || falcon.Serial != 1 || falcon.Name != "Falcon XR" {
		t.Errorf("FLCN = %+v", falcon)
	}
	if falcon.Police != "no (ferrari)" || falcon.PoliceFlag != 0xA0 {
		t.Errorf("FLCN police = %q (0x%02X)", falcon.Police, falcon.PoliceFlag)
	}
	nova := positions["NOVA"]
	if nova.Police != "yes" || nova.PoliceFlag != 0x10 {
		t.Errorf("NOVA police = %q (0x%02X)", nova.Police, nova.PoliceFlag)
	}
	comet := positions["CMET"]
	if comet.Class != "unknown (0x07)" || !comet.Bonus || !comet.DLC {
		t.Errorf("CMET = %+v", comet)
	}
	orion := positions["ORON"]
	if !orion.Upgradable || orion.Class != "B" {
		t.Errorf("ORON = %+v", orion)
	}
}

func TestGeneratedFedataRoundTrip(t *testing.T) {
	for _, car := range Cars() {
		archive, err := Archive(car)
		if err != nil {
			t.Fatalf("%s: %v", car.Folder, err)
		}
		for _, language := range Languages {
			index, ok := archive.Find("fedata." + language)
			if !ok {
				t.Fatalf("%s: fedata.%s missing", car.Folder, language)
			}
			parsed, err := fedata.Parse(archive.Entries[index].Data)
			if err != nil {
				t.Fatalf("%s: fedata.%s: %v", car.Folder, language, err)
			}
			reparsed, err := fedata.Parse(parsed.Bytes())
			if err != nil {
				t.Fatalf("%s: fedata.%s: %v", car.Folder, language, err)
			}
			for _, name := range fedata.AllFields() {
				before, err := parsed.Get(name)
				if err != nil {
					t.Fatalf("Get(%s): %v", name, err)
				}
				after, err := reparsed.Get(name)
				if err != nil {
					t.Fatalf("Get(%s): %v", name, err)
				}
				if before != after {
					t.Errorf("%s fedata.%s %s = %q -> %q", car.Folder, language, name, before, after)
				}
			}
		}
	}
}

func TestArchiveEntryNames(t *testing.T) {
	archive, err := Archive(Cars()[0])
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"car.fce":    true,
		"car.geo":    true,
		"dash.qfs":   true,
		"car.bnk":    true,
		"fedata.fsh": true,
	}
	for _, language := range Languages {
		allowed["fedata."+language] = true
	}
	for _, entry := range archive.Entries {
		if !allowed[entry.Name] {
			t.Errorf("unexpected entry %q", entry.Name)
		}
	}
}
