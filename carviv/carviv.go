// Package carviv scans Need for Speed 4 car folders and reads the car
// information embedded in each car.viv archive.
package carviv

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gitlab.babun.tv/nfs4/nfs4-tools/nfs4viv/fedata"
	"gitlab.babun.tv/nfs4/nfs4-tools/nfs4viv/viv"
)

// Languages lists the fedata localization extensions in preference order.
var Languages = []string{"eng", "bri", "fre", "ger", "ita", "spa", "swe"}

// Info describes a single car read from its car.viv archive.
type Info struct {
	Folder     string   `json:"folder"`
	Viv        string   `json:"viv"`
	Entry      string   `json:"entry"`
	CarID      string   `json:"car_id"`
	Serial     uint16   `json:"serial"`
	Name       string   `json:"name"`
	Class      string   `json:"class"`
	ClassRaw   uint8    `json:"class_raw"`
	Police     string   `json:"police"`
	PoliceFlag uint8    `json:"police_flag"`
	Bonus      bool     `json:"bonus"`
	Upgradable bool     `json:"upgradable"`
	DLC        bool     `json:"dlc"`
	Roof       string   `json:"roof"`
	Price      int32    `json:"price"`
	PriceText  string   `json:"price_text"`
	Duplicates []string `json:"duplicates,omitempty"`
}

// Scan reads car.viv from root and from every immediate subfolder of root.
// lang selects a specific fedata localization (for example "eng"); when it is
// empty the first available localization is used.
func Scan(root, lang string) ([]Info, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("carviv: %s is not a directory", root)
	}
	directories, err := carDirectories(root)
	if err != nil {
		return nil, err
	}
	if len(directories) == 0 {
		return nil, fmt.Errorf("carviv: no car.viv found in %s or its subfolders; point DIR at the folder holding car00, car01, ...", root)
	}
	result := make([]Info, 0, len(directories))
	for _, path := range directories {
		archive, err := viv.ReadFile(path)
		if err != nil {
			return nil, err
		}
		entry, data, err := FindFedata(archive, "", lang)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		parsed, err := fedata.Parse(data)
		if err != nil {
			return nil, fmt.Errorf("%s: entry %s: %w", path, entry, err)
		}
		result = append(result, Info{
			Folder:     filepath.Base(filepath.Dir(path)),
			Viv:        path,
			Entry:      entry,
			CarID:      parsed.CarID(),
			Serial:     parsed.Serial(),
			Name:       parsed.CarName(),
			Class:      parsed.ClassName(),
			ClassRaw:   parsed.CarClass(),
			Police:     parsed.PoliceName(),
			PoliceFlag: parsed.PoliceFlag(),
			Bonus:      parsed.Bonus(),
			Upgradable: parsed.Upgradable(),
			DLC:        parsed.DLC(),
			Roof:       parsed.RoofName(),
			Price:      parsed.BasePrice(),
			PriceText:  parsed.PriceText(),
		})
	}
	return result, nil
}

// FindFedata locates the fedata entry inside a VIV archive. When entry is not
// empty it must name an existing entry; otherwise lang chooses the
// localization and an empty lang falls back to the first available one.
func FindFedata(archive *viv.File, entry, lang string) (string, []byte, error) {
	if entry != "" {
		if index, ok := archive.Find(entry); ok {
			return archive.Entries[index].Name, archive.Entries[index].Data, nil
		}
		return "", nil, fmt.Errorf("entry %q not found (available: %s)", entry, entryList(archive))
	}
	if lang != "" {
		name := "fedata." + strings.ToLower(strings.TrimPrefix(lang, "."))
		if index, ok := findEntry(archive, name); ok {
			return archive.Entries[index].Name, archive.Entries[index].Data, nil
		}
		return "", nil, fmt.Errorf("entry %q not found (available: %s)", name, entryList(archive))
	}
	for _, language := range Languages {
		if index, ok := findEntry(archive, "fedata."+language); ok {
			return archive.Entries[index].Name, archive.Entries[index].Data, nil
		}
	}
	for index := range archive.Entries {
		if strings.HasPrefix(strings.ToLower(baseName(archive.Entries[index].Name)), "fedata.") {
			return archive.Entries[index].Name, archive.Entries[index].Data, nil
		}
	}
	return "", nil, fmt.Errorf("no fedata entry found (available: %s)", entryList(archive))
}

// findEntry looks for an exact entry name first, then for an entry whose base
// name matches, so that VIVs storing paths such as "car00/fedata.eng" work.
func findEntry(archive *viv.File, name string) (int, bool) {
	if index, ok := archive.Find(name); ok {
		return index, true
	}
	for index := range archive.Entries {
		if strings.EqualFold(baseName(archive.Entries[index].Name), name) {
			return index, true
		}
	}
	return 0, false
}

func baseName(name string) string {
	if index := strings.LastIndexAny(name, `/\`); index >= 0 {
		return name[index+1:]
	}
	return name
}

// FindFedataLanguages returns the stored names of every known fedata
// localization entry found in the archive, in Languages preference order.
// Unlike FindFedata it never falls back to a single entry, so it can be used
// to update all localizations at once.
func FindFedataLanguages(archive *viv.File) []string {
	var names []string
	for _, language := range Languages {
		if index, ok := findEntry(archive, "fedata."+language); ok {
			names = append(names, archive.Entries[index].Name)
		}
	}
	return names
}

// carDirectories returns the car.viv paths found in root and in its
// immediate subfolders, sorted by folder name. The stored file name is
// preserved because the game data may spell it CAR.VIV.
func carDirectories(root string) ([]string, error) {
	var paths []string
	if path, ok := carVivPath(root); ok {
		paths = append(paths, path)
	}
	children, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, child := range children {
		if !child.IsDir() {
			continue
		}
		if path, ok := carVivPath(filepath.Join(root, child.Name())); ok {
			paths = append(paths, path)
		}
	}
	sort.Slice(paths, func(left, right int) bool {
		return strings.ToLower(filepath.Base(filepath.Dir(paths[left]))) < strings.ToLower(filepath.Base(filepath.Dir(paths[right])))
	})
	return paths, nil
}

// carVivPath returns the path of the car.viv file inside a folder, matching
// its name case-insensitively.
func carVivPath(directory string) (string, bool) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return "", false
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.EqualFold(entry.Name(), "car.viv") {
			return filepath.Join(directory, entry.Name()), true
		}
	}
	return "", false
}

func entryList(archive *viv.File) string {
	if len(archive.Entries) == 0 {
		return "none"
	}
	names := make([]string, len(archive.Entries))
	for index := range archive.Entries {
		names[index] = archive.Entries[index].Name
	}
	return strings.Join(names, ", ")
}

// sortColumns lists every column accepted by SortBy, in documentation order.
var sortColumns = []string{"folder", "car_id", "serial", "class", "police", "bonus", "upgradable", "price", "name"}

var sortComparators = map[string]func(left, right Info) int{
	"folder":     compareFolder,
	"car_id":     compareCarID,
	"serial":     func(left, right Info) int { return cmp.Compare(left.Serial, right.Serial) },
	"class":      compareClass,
	"police":     func(left, right Info) int { return cmp.Compare(left.PoliceFlag, right.PoliceFlag) },
	"bonus":      func(left, right Info) int { return cmp.Compare(boolRank(left.Bonus), boolRank(right.Bonus)) },
	"upgradable": func(left, right Info) int { return cmp.Compare(boolRank(left.Upgradable), boolRank(right.Upgradable)) },
	"price":      func(left, right Info) int { return cmp.Compare(left.Price, right.Price) },
	"name":       compareName,
}

// SortColumns lists the columns accepted by the -sort flag.
func SortColumns() []string {
	return append([]string{}, sortColumns...)
}

// SortBy sorts cars by the given column. The class column orders cars from B
// to AAA, names are compared case-insensitively, and numeric columns sort
// ascending. Class is always used as the second key and name as the third,
// unless they are the selected column themselves.
func SortBy(list []Info, column string) error {
	column = strings.ToLower(strings.TrimSpace(column))
	primary, ok := sortComparators[column]
	if !ok {
		return fmt.Errorf("carviv: unknown sort column %q (use %s)", column, strings.Join(sortColumns, ", "))
	}
	sort.Slice(list, func(left, right int) bool {
		if result := primary(list[left], list[right]); result != 0 {
			return result < 0
		}
		if column != "class" {
			if result := compareClass(list[left], list[right]); result != 0 {
				return result < 0
			}
		}
		if column != "name" {
			if result := compareName(list[left], list[right]); result != 0 {
				return result < 0
			}
		}
		return compareFolder(list[left], list[right]) < 0
	})
	return nil
}

// MarkDuplicates fills Duplicates on every car that shares its serial number,
// its car ID or its name plus class with another car. It exists so that the
// serials command can highlight conflicting cars with -check.
func MarkDuplicates(list []Info) {
	bySerial := map[uint16][]int{}
	byCarID := map[string][]int{}
	byName := map[string][]int{}
	for index := range list {
		list[index].Duplicates = nil
		bySerial[list[index].Serial] = append(bySerial[list[index].Serial], index)
		carID := duplicateCarIDKey(list[index])
		byCarID[carID] = append(byCarID[carID], index)
		key := duplicateNameKey(list[index])
		byName[key] = append(byName[key], index)
	}
	for _, indexes := range bySerial {
		if len(indexes) > 1 {
			for _, index := range indexes {
				list[index].Duplicates = append(list[index].Duplicates, "serial")
			}
		}
	}
	for _, indexes := range byCarID {
		if len(indexes) > 1 {
			for _, index := range indexes {
				list[index].Duplicates = append(list[index].Duplicates, "car_id")
			}
		}
	}
	for _, indexes := range byName {
		if len(indexes) > 1 {
			for _, index := range indexes {
				list[index].Duplicates = append(list[index].Duplicates, "name")
			}
		}
	}
}

// duplicateCarIDKey builds the case-insensitive car ID comparison key.
func duplicateCarIDKey(car Info) string {
	return strings.ToLower(strings.TrimSpace(car.CarID))
}

// duplicateNameKey builds the case-insensitive name plus class comparison key
// used to detect cars that would show up twice in the front end.
func duplicateNameKey(car Info) string {
	return strings.ToLower(strings.TrimSpace(car.Name)) + "\x00" + car.Class
}

func compareFolder(left, right Info) int {
	return strings.Compare(strings.ToLower(left.Folder), strings.ToLower(right.Folder))
}

func compareCarID(left, right Info) int {
	return strings.Compare(strings.ToLower(left.CarID), strings.ToLower(right.CarID))
}

func compareName(left, right Info) int {
	return strings.Compare(strings.ToLower(left.Name), strings.ToLower(right.Name))
}

// compareClass orders cars from B to AAA, keeping unknown classes last.
func compareClass(left, right Info) int {
	return cmp.Compare(classRank(left.ClassRaw), classRank(right.ClassRaw))
}

func classRank(class byte) int {
	switch class {
	case 3:
		return 0 // B
	case 2:
		return 1 // A
	case 1:
		return 2 // AA
	case 0:
		return 3 // AAA
	default:
		return 4
	}
}

func boolRank(value bool) int {
	if value {
		return 1
	}
	return 0
}
