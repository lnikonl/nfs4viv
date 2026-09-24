// Package cartest builds synthetic Need for Speed 4 car folders for tests and
// manual experiments. The fixtures mimic the structure of the real game data
// (a CAR.VIV archive with seven fedata localizations plus unrelated entries
// and sibling files) but contain no original game assets: every blob is
// generated locally from a seed, so the fixtures are safe to publish.
package cartest

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"gitlab.babun.tv/nfs4/nfs4-tools/nfs4viv/fedata"
	"gitlab.babun.tv/nfs4/nfs4-tools/nfs4viv/viv"
)

// Languages are the localizations written into every fixture archive.
var Languages = []string{"eng", "bri", "fre", "ger", "ita", "spa", "swe"}

// Car describes one synthetic car fixture.
type Car struct {
	Folder         string
	CarID          string
	Serial         uint16
	Name           string
	Manufacturer   string
	Model          string
	Class          string
	Police         string
	Bonus          bool
	Upgradable     bool
	DLC            bool
	Roof           string
	EngineLocation string
	Price          int32
	// LowercaseViv writes car.viv instead of CAR.VIV.
	LowercaseViv bool
	// SiblingFiles adds unrelated VIV files next to the car archive, the way
	// the game folder carries IPSPCHxx.VIV files.
	SiblingFiles bool
}

// Cars returns the default fixture set. The values deliberately cover the
// interesting cases: police flag variants, bonus/non-upgradable cars, mixed
// case file names and values outside the documented ranges.
func Cars() []Car {
	return []Car{
		{
			Folder: "FLCN", CarID: "FLCN", Serial: 1, Name: "Falcon XR",
			Manufacturer: "Falcon", Model: "XR", Class: "AAA", Police: "no (ferrari)",
			Roof: "convertible", EngineLocation: "front", Price: 225000,
			Upgradable: true,
		},
		{
			Folder: "ORON", CarID: "ORON", Serial: 3, Name: "Orion 500",
			Manufacturer: "Orion", Model: "500", Class: "B", Police: "no",
			Roof: "solid", EngineLocation: "mid", Price: 20000,
			Upgradable: true, LowercaseViv: true,
		},
		{
			Folder: "NOVA", CarID: "NOVA", Serial: 12, Name: "Nova Pursuit",
			Manufacturer: "Nova", Model: "Interceptor", Class: "AA", Police: "yes",
			Roof: "solid", EngineLocation: "rear", Price: 50000,
			SiblingFiles: true,
		},
		{
			Folder: "CMET", CarID: "CMET", Serial: 42, Name: "Comet Rallye",
			Manufacturer: "Comet", Model: "Rallye", Class: "unknown (0x07)", Police: "no_mercedes",
			Bonus: true, DLC: true, Roof: "unknown (0x03)", EngineLocation: "front", Price: 200000,
		},
	}
}

// Archive builds the synthetic CAR.VIV archive for one car. The archive holds
// stand-in blobs for geometry, textures and audio plus one fedata file per
// localization.
func Archive(car Car) (*viv.File, error) {
	entries := []viv.Entry{
		{Name: "car.fce", Data: blob(0x14, 8192, car.Serial)},
		{Name: "car.geo", Data: blob(0x10, 4096, car.Serial+1)},
		{Name: "dash.qfs", Data: blob(0x51, 2048, car.Serial+2)},
		{Name: "car.bnk", Data: blob(0x42, 16384, car.Serial+3)},
		{Name: "fedata.fsh", Data: blob(0x53, 4096, car.Serial+4)},
	}
	for index, language := range Languages {
		file, err := fedataFile(car, index, language)
		if err != nil {
			return nil, err
		}
		entries = append(entries, viv.Entry{Name: "fedata." + language, Data: file.Bytes()})
	}
	return &viv.File{Entries: entries}, nil
}

// Write creates a car folder tree for the given cars under root.
func Write(root string, cars []Car) error {
	for _, car := range cars {
		archive, err := Archive(car)
		if err != nil {
			return fmt.Errorf("cartest: %s: %w", car.Folder, err)
		}
		data, err := archive.Bytes()
		if err != nil {
			return fmt.Errorf("cartest: %s: %w", car.Folder, err)
		}
		directory := filepath.Join(root, car.Folder)
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return fmt.Errorf("cartest: %w", err)
		}
		name := "CAR.VIV"
		if car.LowercaseViv {
			name = "car.viv"
		}
		if err := os.WriteFile(filepath.Join(directory, name), data, 0o644); err != nil {
			return fmt.Errorf("cartest: %w", err)
		}
		if car.SiblingFiles {
			if err := writeSiblings(directory, car); err != nil {
				return fmt.Errorf("cartest: %s: %w", car.Folder, err)
			}
		}
	}
	return nil
}

// writeSiblings adds small unrelated archives that scanners must ignore.
func writeSiblings(directory string, car Car) error {
	for _, name := range []string{"IPSPCHNB.VIV", "IPSPCHNE.VIV"} {
		sibling := &viv.File{Entries: []viv.Entry{
			{Name: "dummy.qfs", Data: blob(0x51, 256, car.Serial)},
		}}
		data, err := sibling.Bytes()
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(directory, name), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func fedataFile(car Car, languageIndex int, language string) (*fedata.File, error) {
	file := fedata.New()
	name := car.Name
	if language != "eng" {
		name = fmt.Sprintf("%s [%s]", car.Name, language)
	}
	fields := map[string]string{
		"car_id":              car.CarID,
		"serial":              strconv.Itoa(int(car.Serial)),
		"car_name":            name,
		"manufacturer":        car.Manufacturer,
		"model":               car.Model,
		"class":               car.Class,
		"police":              car.Police,
		"bonus":               yesNo(car.Bonus),
		"upgradable":          yesNo(car.Upgradable),
		"dlc":                 yesNo(car.DLC),
		"roof":                car.Roof,
		"engine_location":     car.EngineLocation,
		"price":               strconv.FormatInt(int64(car.Price), 10),
		"price_text":          fmt.Sprintf("$%d", car.Price),
		"status":              "Synthetic fixture",
		"weight":              fmt.Sprintf("%d kg", 1200+languageIndex),
		"hp":                  fmt.Sprintf("%d hp", 300+languageIndex),
		"color_1":             "Synthetic Red",
		"color_2":             "Synthetic Blue",
		"history_1":           fmt.Sprintf("Generated fixture, language %s", language),
		"rating_acceleration": strconv.Itoa(10 + languageIndex),
		"rating_top_speed":    strconv.Itoa(12 + languageIndex),
		"rating_handling":     strconv.Itoa(8 + languageIndex),
		"rating_braking":      strconv.Itoa(9 + languageIndex),
		"rating_overall":      strconv.Itoa(11 + languageIndex),
	}
	for name, value := range fields {
		if err := file.Set(name, value); err != nil {
			return nil, fmt.Errorf("field %s: %w", name, err)
		}
	}
	return file, nil
}

// blob builds deterministic filler data that stands in for textures,
// geometry and audio. It uses a small LCG so the bytes are stable and
// reproducible without importing an RNG.
func blob(magic byte, size int, seed uint16) []byte {
	data := make([]byte, size)
	state := uint32(seed)*2654435761 + 1
	for index := range data {
		state = state*1664525 + 1013904223
		data[index] = byte(state >> 24)
	}
	data[0] = magic
	return data
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
