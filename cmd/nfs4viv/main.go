// nfs4viv reads and edits Need for Speed 4 car.viv archives.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gitlab.babun.tv/nfs4/nfs4-tools/nfs4viv/appinfo"
	"gitlab.babun.tv/nfs4/nfs4-tools/nfs4viv/carviv"
	"gitlab.babun.tv/nfs4/nfs4-tools/nfs4viv/fedata"
	"gitlab.babun.tv/nfs4/nfs4-tools/nfs4viv/viv"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		usage(os.Stdout)
		return nil
	}
	if args[0] == "version" || args[0] == "--version" || args[0] == "-v" {
		appinfo.PrintVersion(os.Stdout, "nfs4viv")
		return nil
	}
	for _, arg := range args[1:] {
		if arg == "-h" || arg == "--help" {
			usage(os.Stdout)
			return nil
		}
	}
	switch args[0] {
	case "serials":
		return serials(args[1:])
	case "fedata":
		return fedataCommand(args[1:])
	case "viv":
		return vivCommand(args[1:])
	default:
		return fmt.Errorf("unknown command %q; run nfs4viv help", args[0])
	}
}

// defaultCarsDir is scanned by the serials command when no path is given.
const defaultCarsDir = "./DATA/CARS"

func serials(args []string) error {
	flags := flag.NewFlagSet("serials", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	jsonOutput := flags.Bool("json", false, "write the list as JSON")
	lang := flags.String("lang", "", "fedata language extension to read (default: first available)")
	sortColumn := flags.String("sort", "serial", "sort column: "+strings.Join(carviv.SortColumns(), ", "))
	check := flags.Bool("check", true, "check for duplicate serial numbers and name plus class pairs (default true)")
	checkOff := flags.Bool("check-off", false, "disable duplicate checking")
	color := flags.String("color", "auto", "colorize duplicate cells: auto, always or never")
	positional, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	if len(positional) > 1 {
		return fmt.Errorf("usage: nfs4viv serials [-json] [-lang EXT] [-sort COLUMN] [-check] [-check-off] [-color MODE] [DIR]")
	}
	if strings.EqualFold(*lang, "all") {
		return fmt.Errorf("-lang all is only supported by fedata show and fedata set; serials reads one localization per car")
	}
	useColor, err := resolveColor(*color)
	if err != nil {
		return err
	}
	directory := defaultCarsDir
	if len(positional) == 1 {
		directory = positional[0]
	}
	list, err := carviv.Scan(directory, *lang)
	if err != nil {
		return err
	}
	if err := carviv.SortBy(list, *sortColumn); err != nil {
		return err
	}
	performCheck := *check && !*checkOff
	if performCheck {
		carviv.MarkDuplicates(list)
	}
	warnSerials(list, performCheck)
	if *jsonOutput {
		return writeJSON(list)
	}
	printCarTable(list, performCheck, useColor)
	return nil
}

// ANSI sequences used to mark the exact field that collides with another car.
// The color is a pair of simple, single-attribute escapes so that even basic
// Windows consoles render them reliably; every conflicting field uses the
// same highlight because the column already tells what kind of duplicate it
// is.
const (
	ansiReset    = "\x1b[0m"
	ansiConflict = "\x1b[41m\x1b[37m" // light gray on red
)

// resolveColor turns the -color flag value into a decision. auto enables
// colors only when stdout is a character device and the terminal can
// interpret ANSI escapes; on Windows this also switches the console into
// virtual terminal mode so PowerShell and cmd.exe render the colors.
func resolveColor(mode string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "auto":
		if !isTerminal(os.Stdout) {
			return false, nil
		}
		return enableVirtualTerminal(os.Stdout), nil
	case "always", "on", "yes":
		_ = enableVirtualTerminal(os.Stdout)
		return true, nil
	case "never", "off", "no":
		return false, nil
	default:
		return false, fmt.Errorf("unknown -color value %q (use auto, always or never)", mode)
	}
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// warnSerials reports cars that share a serial number or still use serial 0,
// and duplicated name plus class pairs. It writes to stderr so stdout stays
// machine readable. Disabling duplicate checking silences every warning.
func warnSerials(list []carviv.Info, check bool) {
	if !check {
		return
	}
	bySerial := map[uint16][]string{}
	for _, car := range list {
		bySerial[car.Serial] = append(bySerial[car.Serial], car.Folder)
	}
	serials := make([]int, 0, len(bySerial))
	for serial := range bySerial {
		serials = append(serials, int(serial))
	}
	sort.Ints(serials)
	for _, serial := range serials {
		folders := bySerial[uint16(serial)]
		if len(folders) > 1 {
			sort.Strings(folders)
			fmt.Fprintf(os.Stderr, "warning: serial %d is used by %s\n", serial, strings.Join(folders, ", "))
		}
	}
	for _, car := range list {
		if car.Serial == 0 {
			fmt.Fprintf(os.Stderr, "warning: %s has serial 0\n", car.Folder)
		}
	}
	type idGroup struct {
		carID   string
		folders []string
	}
	byCarID := map[string]*idGroup{}
	for _, car := range list {
		key := strings.ToLower(strings.TrimSpace(car.CarID))
		group, ok := byCarID[key]
		if !ok {
			group = &idGroup{carID: car.CarID}
			byCarID[key] = group
		}
		group.folders = append(group.folders, car.Folder)
	}
	idKeys := make([]string, 0, len(byCarID))
	for key := range byCarID {
		idKeys = append(idKeys, key)
	}
	sort.Strings(idKeys)
	for _, key := range idKeys {
		group := byCarID[key]
		if len(group.folders) < 2 {
			continue
		}
		sort.Strings(group.folders)
		fmt.Fprintf(os.Stderr, "warning: %s share the car ID %q\n", strings.Join(group.folders, ", "), group.carID)
	}
	type nameGroup struct {
		name    string
		folders []string
	}
	byName := map[string]*nameGroup{}
	for _, car := range list {
		key := strings.ToLower(strings.TrimSpace(car.Name)) + "\x00" + car.Class
		group, ok := byName[key]
		if !ok {
			group = &nameGroup{name: car.Name}
			byName[key] = group
		}
		group.folders = append(group.folders, car.Folder)
	}
	keys := make([]string, 0, len(byName))
	for key := range byName {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		group := byName[key]
		if len(group.folders) < 2 {
			continue
		}
		_, class, _ := strings.Cut(key, "\x00")
		sort.Strings(group.folders)
		fmt.Fprintf(os.Stderr, "warning: %s share the name %q (class %s)\n", strings.Join(group.folders, ", "), group.name, class)
	}
}

func printCarTable(list []carviv.Info, check, color bool) {
	headers := []string{"FOLDER", "CAR_ID", "SERIAL", "CLASS", "POLICE", "BONUS", "UPGRADABLE", "PRICE", "NAME"}
	if check {
		headers = append(headers, "DUPLICATES")
	}
	rows := make([][]tableCell, len(list))
	for index, car := range list {
		carID := tableCell{Text: cleanCell(car.CarID)}
		serial := tableCell{Text: strconv.Itoa(int(car.Serial))}
		name := tableCell{Text: cleanCell(car.Name)}
		for _, duplicate := range car.Duplicates {
			switch duplicate {
			case "car_id":
				carID.Duplicate = duplicate
			case "serial":
				serial.Duplicate = duplicate
			case "name":
				name.Duplicate = duplicate
			}
		}
		rows[index] = []tableCell{
			{Text: cleanCell(car.Folder)},
			carID,
			serial,
			{Text: car.Class},
			{Text: car.Police},
			{Text: yesNo(car.Bonus)},
			{Text: yesNo(car.Upgradable)},
			{Text: strconv.FormatInt(int64(car.Price), 10)},
			name,
		}
		if check {
			rows[index] = append(rows[index], tableCell{Text: strings.Join(car.Duplicates, ",")})
		}
	}
	printTable(headers, rows, map[int]bool{2: true, 7: true}, color)
}

func fedataCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: nfs4viv fedata show|set|fields ...")
	}
	switch args[0] {
	case "show":
		return fedataShow(args[1:])
	case "set":
		return fedataSet(args[1:])
	case "export":
		return fedataExport(args[1:])
	case "import":
		return fedataImport(args[1:])
	case "fields":
		return fedataFields(args[1:])
	default:
		return fmt.Errorf("unknown fedata command %q; run nfs4viv help", args[0])
	}
}

type target struct {
	path    string
	archive *viv.File
	entry   string
	file    *fedata.File
}

func loadTarget(path, entry, lang string) (*target, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) >= 4 && string(data[:4]) == "BIGF" {
		archive, err := viv.Read(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		name, raw, err := carviv.FindFedata(archive, entry, lang)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		parsed, err := fedata.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: entry %s: %w", path, name, err)
		}
		return &target{path: path, archive: archive, entry: name, file: parsed}, nil
	}
	if entry != "" || lang != "" {
		return nil, fmt.Errorf("%s is not a VIV archive; -entry and -lang require one", path)
	}
	parsed, err := fedata.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &target{path: path, file: parsed}, nil
}

func (t *target) save(path string) error {
	if t.archive != nil {
		if !t.archive.Set(t.entry, t.file.Bytes()) {
			return fmt.Errorf("entry %q disappeared from %s", t.entry, t.path)
		}
		data, err := t.archive.Bytes()
		if err != nil {
			return err
		}
		return writeFileAtomic(path, data)
	}
	return writeFileAtomic(path, t.file.Bytes())
}

// loadTargets returns the fedata file(s) selected by the entry and lang
// flags. lang "all" selects every known localization present in the archive.
func loadTargets(path, entry, lang string) ([]*target, error) {
	if entry != "" && strings.EqualFold(lang, "all") {
		return nil, fmt.Errorf("-entry cannot be combined with -lang all")
	}
	if !strings.EqualFold(lang, "all") {
		loaded, err := loadTarget(path, entry, lang)
		if err != nil {
			return nil, err
		}
		return []*target{loaded}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !(len(data) >= 4 && string(data[:4]) == "BIGF") {
		return nil, fmt.Errorf("%s is not a VIV archive; -lang all requires one", path)
	}
	archive, err := viv.Read(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	names := carviv.FindFedataLanguages(archive)
	if len(names) == 0 {
		return nil, fmt.Errorf("%s: no fedata language entries found (looked for %s)", path, fedataLanguageList())
	}
	targets := make([]*target, 0, len(names))
	for _, name := range names {
		_, raw, err := carviv.FindFedata(archive, name, "")
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		parsed, err := fedata.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: entry %s: %w", path, name, err)
		}
		targets = append(targets, &target{path: path, archive: archive, entry: name, file: parsed})
	}
	return targets, nil
}

func fedataLanguageList() string {
	return strings.Join(carviv.Languages, ", ")
}

func printFedata(loaded *target) error {
	if loaded.archive != nil {
		fmt.Printf("entry: %s\n", loaded.entry)
	}
	for _, name := range fedata.AllFields() {
		value, err := loaded.file.Get(name)
		if err != nil {
			return err
		}
		fmt.Printf("%-22s %s\n", name+":", cleanCell(value))
	}
	return nil
}

func fedataShow(args []string) error {
	flags := flag.NewFlagSet("fedata show", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	jsonOutput := flags.Bool("json", false, "write the fields as JSON")
	entry := flags.String("entry", "", "read this VIV entry instead of the first fedata entry")
	lang := flags.String("lang", "", "fedata language extension or all (default: first available)")
	positional, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return fmt.Errorf("usage: nfs4viv fedata show [-json] [-entry NAME] [-lang EXT|all] INPUT.VIV|FEDATA")
	}
	loaded, err := loadTargets(positional[0], *entry, *lang)
	if err != nil {
		return err
	}
	if *jsonOutput {
		if len(loaded) == 1 {
			return writeJSON(fedataJSONFrom(loaded[0]))
		}
		list := make([]fedataJSON, len(loaded))
		for index, item := range loaded {
			list[index] = fedataJSONFrom(item)
		}
		return writeJSON(list)
	}
	for index, item := range loaded {
		if index > 0 {
			fmt.Println()
		}
		if err := printFedata(item); err != nil {
			return err
		}
	}
	return nil
}

func fedataSet(args []string) error {
	flags := flag.NewFlagSet("fedata set", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	output := flags.String("o", "", "write the result to this file instead of overwriting the input")
	entry := flags.String("entry", "", "edit this VIV entry instead of the first fedata entry")
	lang := flags.String("lang", "", "fedata language extension or all (default: first available)")
	rest, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	if len(rest) < 3 || len(rest)%2 == 0 {
		return fmt.Errorf("usage: nfs4viv fedata set [-o OUTPUT] [-entry NAME] [-lang EXT|all] INPUT.VIV|FEDATA FIELD VALUE [FIELD VALUE ...]")
	}
	loaded, err := loadTargets(rest[0], *entry, *lang)
	if err != nil {
		return err
	}
	for _, item := range loaded {
		for index := 1; index+1 < len(rest); index += 2 {
			if err := item.file.Set(rest[index], rest[index+1]); err != nil {
				return err
			}
		}
	}
	out := *output
	if out == "" {
		out = rest[0]
	}
	if err := saveTargets(loaded, out); err != nil {
		return err
	}
	if len(loaded) == 1 {
		fmt.Printf("updated %d field(s) in %s\n", (len(rest)-1)/2, out)
	} else {
		fmt.Printf("updated %d field(s) in %d entries in %s\n", (len(rest)-1)/2, len(loaded), out)
	}
	return nil
}

func fedataFields(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: nfs4viv fedata fields")
	}
	for _, name := range fedata.AllFields() {
		fmt.Println(name)
	}
	return nil
}

// exportedFedata is the flat, editable JSON representation used by fedata
// export and import: one key per field accepted by fedata set.
type exportedFedata struct {
	Entry  string
	Fields map[string]string
}

// MarshalJSON writes "entry" first (when present) followed by the fields in
// fedata.AllFields order, so exports are stable and easy to review.
func (e exportedFedata) MarshalJSON() ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteByte('{')
	first := true
	write := func(name, value string) error {
		key, err := json.Marshal(name)
		if err != nil {
			return err
		}
		text, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if !first {
			buffer.WriteByte(',')
		}
		first = false
		buffer.Write(key)
		buffer.WriteByte(':')
		buffer.Write(text)
		return nil
	}
	if e.Entry != "" {
		if err := write("entry", e.Entry); err != nil {
			return nil, err
		}
	}
	for _, name := range fedata.AllFields() {
		if err := write(name, e.Fields[name]); err != nil {
			return nil, err
		}
	}
	buffer.WriteByte('}')
	return buffer.Bytes(), nil
}

func newExportedFedata(loaded *target) (exportedFedata, error) {
	fields := make(map[string]string, len(fedata.AllFields()))
	for _, name := range fedata.AllFields() {
		value, err := loaded.file.Get(name)
		if err != nil {
			return exportedFedata{}, err
		}
		fields[name] = value
	}
	entry := ""
	if loaded.archive != nil {
		entry = loaded.entry
	}
	return exportedFedata{Entry: entry, Fields: fields}, nil
}

// saveTargets writes the edited fedata entries back. Several targets share
// one VIV archive and are repacked in a single pass.
func saveTargets(targets []*target, out string) error {
	if len(targets) == 1 {
		return targets[0].save(out)
	}
	archive := targets[0].archive
	for _, item := range targets {
		if !archive.Set(item.entry, item.file.Bytes()) {
			return fmt.Errorf("entry %q disappeared from %s", item.entry, item.path)
		}
	}
	data, err := archive.Bytes()
	if err != nil {
		return err
	}
	return writeFileAtomic(out, data)
}

func fedataExport(args []string) error {
	flags := flag.NewFlagSet("fedata export", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	output := flags.String("o", "", "write the JSON to this file instead of stdout")
	entry := flags.String("entry", "", "export this VIV entry instead of the first fedata entry")
	lang := flags.String("lang", "", "fedata language extension or all (default: first available)")
	positional, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return fmt.Errorf("usage: nfs4viv fedata export [-o OUTPUT] [-entry NAME] [-lang EXT|all] INPUT.VIV|FEDATA")
	}
	loaded, err := loadTargets(positional[0], *entry, *lang)
	if err != nil {
		return err
	}
	exports := make([]exportedFedata, len(loaded))
	for index, item := range loaded {
		exported, err := newExportedFedata(item)
		if err != nil {
			return err
		}
		exports[index] = exported
	}
	var value any = exports[0]
	if strings.EqualFold(*lang, "all") || len(exports) > 1 {
		value = exports
	}
	if *output == "" {
		return writeJSON(value)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := writeFileAtomic(*output, data); err != nil {
		return err
	}
	fmt.Printf("exported %d fedata %s to %s\n", len(exports), entryWord(len(exports)), *output)
	return nil
}

func fedataImport(args []string) error {
	flags := flag.NewFlagSet("fedata import", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	output := flags.String("o", "", "write the result to this file instead of overwriting the input")
	entry := flags.String("entry", "", "edit this VIV entry instead of the first fedata entry")
	lang := flags.String("lang", "", "fedata language extension or all (default: first available)")
	positional, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	if len(positional) < 1 || len(positional) > 2 {
		return fmt.Errorf("usage: nfs4viv fedata import [-o OUTPUT] [-entry NAME] [-lang EXT|all] INPUT.VIV|FEDATA [JSON]")
	}
	raw, err := readImportJSON(positional)
	if err != nil {
		return err
	}
	imported, isArray, err := decodeExportedFedata(raw)
	if err != nil {
		return err
	}
	allLanguages := strings.EqualFold(*lang, "all")
	if isArray && !allLanguages {
		if *entry != "" || *lang != "" {
			return fmt.Errorf("a JSON array of entries cannot be combined with -entry or -lang EXT")
		}
		// An array names its target entries, so importing it back does not
		// require repeating -lang all.
		allLanguages = true
		*lang = "all"
	}
	loaded, err := loadTargets(positional[0], *entry, *lang)
	if err != nil {
		return err
	}
	applied, err := applyImportedFedata(loaded, imported, isArray, allLanguages)
	if err != nil {
		return err
	}
	out := *output
	if out == "" {
		out = positional[0]
	}
	if err := saveTargets(loaded, out); err != nil {
		return err
	}
	fmt.Printf("imported %d field(s) into %d fedata %s in %s\n", applied, len(loaded), entryWord(len(loaded)), out)
	return nil
}

// readImportJSON loads the import document from the second positional
// argument or, when it is omitted or "-", from standard input.
func readImportJSON(positional []string) ([]byte, error) {
	if len(positional) == 2 && positional[1] != "-" {
		return os.ReadFile(positional[1])
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil, fmt.Errorf("reading JSON from stdin: %w", err)
	}
	return data, nil
}

func decodeExportedFedata(data []byte) ([]exportedFedata, bool, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, false, fmt.Errorf("invalid JSON: %w", err)
	}
	switch typed := value.(type) {
	case map[string]any:
		item, err := decodeExportedObject(typed)
		if err != nil {
			return nil, false, err
		}
		return []exportedFedata{item}, false, nil
	case []any:
		if len(typed) == 0 {
			return nil, true, fmt.Errorf("JSON array is empty")
		}
		result := make([]exportedFedata, 0, len(typed))
		for index, element := range typed {
			object, ok := element.(map[string]any)
			if !ok {
				return nil, true, fmt.Errorf("JSON entry %d must be an object", index)
			}
			item, err := decodeExportedObject(object)
			if err != nil {
				return nil, true, fmt.Errorf("JSON entry %d: %w", index, err)
			}
			result = append(result, item)
		}
		return result, true, nil
	default:
		return nil, false, fmt.Errorf("JSON must be an object or an array of objects")
	}
}

func decodeExportedObject(object map[string]any) (exportedFedata, error) {
	item := exportedFedata{Fields: make(map[string]string, len(object))}
	for key, value := range object {
		if key == "entry" {
			text, ok := value.(string)
			if !ok {
				return exportedFedata{}, fmt.Errorf(`"entry" must be a string`)
			}
			item.Entry = text
			continue
		}
		text, err := exportedValueText(key, value)
		if err != nil {
			return exportedFedata{}, err
		}
		item.Fields[key] = text
	}
	return item, nil
}

// exportedValueText accepts strings, numbers and booleans so that hand-edited
// JSON stays convenient.
func exportedValueText(name string, value any) (string, error) {
	switch typed := value.(type) {
	case string:
		return typed, nil
	case bool:
		if typed {
			return "yes", nil
		}
		return "no", nil
	case json.Number:
		return typed.String(), nil
	default:
		return "", fmt.Errorf("field %q must be a string, number or boolean", name)
	}
}

func applyImportedFedata(targets []*target, imported []exportedFedata, isArray, allLanguages bool) (int, error) {
	if !allLanguages {
		if isArray {
			return 0, fmt.Errorf("JSON contains an array of entries but a single fedata entry was selected")
		}
		item := imported[0]
		if item.Entry != "" && !sameEntryName(item.Entry, targets[0].entry) {
			return 0, fmt.Errorf("JSON entry %q does not match the selected entry %q", item.Entry, targets[0].entry)
		}
		return applyFields(targets[0].file, item.Fields)
	}
	if !isArray {
		total := 0
		for _, item := range targets {
			count, err := applyFields(item.file, imported[0].Fields)
			if err != nil {
				return 0, err
			}
			total += count
		}
		return total, nil
	}
	byName := make(map[string]*target, len(targets))
	for _, item := range targets {
		byName[strings.ToLower(entryBaseName(item.entry))] = item
	}
	applied := 0
	for index, item := range imported {
		if item.Entry == "" {
			return 0, fmt.Errorf("JSON entry %d needs an \"entry\" name when importing several languages", index)
		}
		target, ok := byName[strings.ToLower(entryBaseName(item.Entry))]
		if !ok {
			return 0, fmt.Errorf("JSON entry %q is not a fedata language entry of the archive", item.Entry)
		}
		count, err := applyFields(target.file, item.Fields)
		if err != nil {
			return 0, err
		}
		applied += count
	}
	return applied, nil
}

func applyFields(file *fedata.File, fields map[string]string) (int, error) {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := file.Set(name, fields[name]); err != nil {
			return 0, err
		}
	}
	return len(names), nil
}

func sameEntryName(left, right string) bool {
	return strings.EqualFold(left, right) || strings.EqualFold(entryBaseName(left), entryBaseName(right))
}

func entryBaseName(name string) string {
	if index := strings.LastIndexAny(name, `/\`); index >= 0 {
		return name[index+1:]
	}
	return name
}

func entryWord(count int) string {
	if count == 1 {
		return "entry"
	}
	return "entries"
}

type fedataJSON struct {
	Entry          string            `json:"entry,omitempty"`
	CarID          string            `json:"car_id"`
	Serial         uint16            `json:"serial"`
	Name           string            `json:"name"`
	Class          string            `json:"class"`
	ClassRaw       uint8             `json:"class_raw"`
	Police         string            `json:"police"`
	PoliceFlag     uint8             `json:"police_flag"`
	Bonus          bool              `json:"bonus"`
	Upgradable     bool              `json:"upgradable"`
	DLC            bool              `json:"dlc"`
	Roof           string            `json:"roof"`
	RoofRaw        uint8             `json:"roof_raw"`
	EngineLocation string            `json:"engine_location"`
	Price          int32             `json:"price"`
	Upgrade1Price  int32             `json:"upgrade_1_price"`
	Upgrade2Price  int32             `json:"upgrade_2_price"`
	Upgrade3Price  int32             `json:"upgrade_3_price"`
	Ratings        map[string]uint8  `json:"ratings"`
	Text           map[string]string `json:"text"`
}

func fedataJSONFrom(loaded *target) fedataJSON {
	file := loaded.file
	ratings := make(map[string]uint8, len(fedata.AllFields()))
	for _, name := range fedata.AllFields() {
		if !strings.HasPrefix(name, "rating_") {
			continue
		}
		value, err := file.Get(name)
		if err != nil {
			continue
		}
		number, _ := strconv.ParseUint(value, 10, 8)
		ratings[name] = uint8(number)
	}
	text := make(map[string]string, len(fedata.TextFields()))
	for _, field := range fedata.TextFields() {
		text[field.Name] = file.Text(field.Index)
	}
	return fedataJSON{
		Entry:          loaded.entry,
		CarID:          file.CarID(),
		Serial:         file.Serial(),
		Name:           file.CarName(),
		Class:          file.ClassName(),
		ClassRaw:       file.CarClass(),
		Police:         file.PoliceName(),
		PoliceFlag:     file.PoliceFlag(),
		Bonus:          file.Bonus(),
		Upgradable:     file.Upgradable(),
		DLC:            file.DLC(),
		Roof:           file.RoofName(),
		RoofRaw:        file.Roof(),
		EngineLocation: file.EngineLocationName(),
		Price:          file.BasePrice(),
		Upgrade1Price:  file.UpgradePrice(1),
		Upgrade2Price:  file.UpgradePrice(2),
		Upgrade3Price:  file.UpgradePrice(3),
		Ratings:        ratings,
		Text:           text,
	}
}

func vivCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: nfs4viv viv ls|read|extract|replace|add|rm ...")
	}
	switch args[0] {
	case "ls":
		return vivList(args[1:])
	case "read":
		return vivRead(args[1:])
	case "extract":
		return vivExtract(args[1:])
	case "replace":
		return vivWrite(args[1:], false)
	case "add":
		return vivWrite(args[1:], true)
	case "rm":
		return vivRemove(args[1:])
	default:
		return fmt.Errorf("unknown viv command %q; run nfs4viv help", args[0])
	}
}

func vivList(args []string) error {
	flags := flag.NewFlagSet("viv ls", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	jsonOutput := flags.Bool("json", false, "write the directory as JSON")
	positional, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return fmt.Errorf("usage: nfs4viv viv ls [-json] INPUT.VIV")
	}
	archive, err := viv.ReadFile(positional[0])
	if err != nil {
		return err
	}
	type jsonEntry struct {
		Name string `json:"name"`
		Size int    `json:"size"`
	}
	if *jsonOutput {
		entries := make([]jsonEntry, len(archive.Entries))
		for index, entry := range archive.Entries {
			entries[index] = jsonEntry{Name: entry.Name, Size: len(entry.Data)}
		}
		return writeJSON(entries)
	}
	for _, entry := range archive.Entries {
		fmt.Printf("%10d  %s\n", len(entry.Data), entry.Name)
	}
	return nil
}

func vivRead(args []string) error {
	flags := flag.NewFlagSet("viv read", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	output := flags.String("o", "", "write the entry to this file instead of stdout")
	positional, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	if len(positional) != 2 {
		return fmt.Errorf("usage: nfs4viv viv read INPUT.VIV ENTRY [-o FILE]")
	}
	archive, err := viv.ReadFile(positional[0])
	if err != nil {
		return err
	}
	index, ok := archive.Find(positional[1])
	if !ok {
		return fmt.Errorf("entry %q not found in %s", positional[1], positional[0])
	}
	data := archive.Entries[index].Data
	if *output == "" {
		_, err = os.Stdout.Write(data)
		return err
	}
	if err := writeFileAtomic(*output, data); err != nil {
		return err
	}
	fmt.Printf("wrote %d bytes to %s\n", len(data), *output)
	return nil
}

func vivExtract(args []string) error {
	flags := flag.NewFlagSet("viv extract", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	output := flags.String("o", ".", "output directory")
	positional, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	if len(positional) < 1 {
		return fmt.Errorf("usage: nfs4viv viv extract INPUT.VIV [ENTRY ...] [-o DIR]")
	}
	archive, err := viv.ReadFile(positional[0])
	if err != nil {
		return err
	}
	selected := positional[1:]
	if len(selected) == 0 {
		for index := range archive.Entries {
			selected = append(selected, archive.Entries[index].Name)
		}
	}
	if err := os.MkdirAll(*output, 0o755); err != nil {
		return err
	}
	used := map[string]int{}
	for _, name := range selected {
		index, ok := archive.Find(name)
		if !ok {
			return fmt.Errorf("entry %q not found in %s", name, positional[0])
		}
		path := filepath.Join(*output, uniqueName(used, safeName(archive.Entries[index].Name)))
		if err := writeFileAtomic(path, archive.Entries[index].Data); err != nil {
			return err
		}
		fmt.Printf("extracted %s\n", path)
	}
	return nil
}

func vivWrite(args []string, add bool) error {
	name := "viv replace"
	if add {
		name = "viv add"
	}
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	output := flags.String("o", "", "write the archive to this file instead of overwriting the input")
	positional, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	if len(positional) != 3 {
		return fmt.Errorf("usage: nfs4viv %s INPUT.VIV ENTRY FILE [-o OUTPUT.VIV]", name)
	}
	archive, err := viv.ReadFile(positional[0])
	if err != nil {
		return err
	}
	data, err := os.ReadFile(positional[2])
	if err != nil {
		return err
	}
	entry := positional[1]
	if add {
		if !archive.Add(entry, data) {
			return fmt.Errorf("entry %q already exists in %s", entry, positional[0])
		}
	} else if !archive.Set(entry, data) {
		return fmt.Errorf("entry %q not found in %s", entry, positional[0])
	}
	out := *output
	if out == "" {
		out = positional[0]
	}
	encoded, err := archive.Bytes()
	if err != nil {
		return err
	}
	if err := writeFileAtomic(out, encoded); err != nil {
		return err
	}
	fmt.Printf("%s %s (%d bytes) in %s\n", map[bool]string{true: "added", false: "replaced"}[add], entry, len(data), out)
	return nil
}

func vivRemove(args []string) error {
	flags := flag.NewFlagSet("viv rm", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	output := flags.String("o", "", "write the archive to this file instead of overwriting the input")
	positional, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	if len(positional) != 2 {
		return fmt.Errorf("usage: nfs4viv viv rm INPUT.VIV ENTRY [-o OUTPUT.VIV]")
	}
	archive, err := viv.ReadFile(positional[0])
	if err != nil {
		return err
	}
	if !archive.Remove(positional[1]) {
		return fmt.Errorf("entry %q not found in %s", positional[1], positional[0])
	}
	out := *output
	if out == "" {
		out = positional[0]
	}
	encoded, err := archive.Bytes()
	if err != nil {
		return err
	}
	if err := writeFileAtomic(out, encoded); err != nil {
		return err
	}
	fmt.Printf("removed %s from %s\n", flags.Arg(1), out)
	return nil
}

// tableCell is one rendered table value. Duplicate holds "serial" or "name"
// when the value collides with another car and should be colorized.
type tableCell struct {
	Text      string
	Duplicate string
}

func printTable(headers []string, rows [][]tableCell, rightAligned map[int]bool, color bool) {
	widths := make([]int, len(headers))
	for index, header := range headers {
		widths[index] = len(header)
	}
	for _, row := range rows {
		for index, cell := range row {
			if len(cell.Text) > widths[index] {
				widths[index] = len(cell.Text)
			}
		}
	}
	for index, header := range headers {
		if index > 0 {
			fmt.Print("  ")
		}
		fmt.Print(pad(header, widths[index], rightAligned[index]))
	}
	fmt.Println()
	for _, row := range rows {
		var line strings.Builder
		for index, cell := range row {
			if index > 0 {
				line.WriteString("  ")
			}
			value := cell.Text
			if color && cell.Duplicate != "" {
				value = ansiConflict + cell.Text + ansiReset
			}
			padding := strings.Repeat(" ", widths[index]-len(cell.Text))
			if rightAligned[index] {
				line.WriteString(padding)
			}
			line.WriteString(value)
			if !rightAligned[index] {
				line.WriteString(padding)
			}
		}
		fmt.Println(strings.TrimRight(line.String(), " "))
	}
}

func pad(value string, width int, right bool) string {
	padding := strings.Repeat(" ", width-len(value))
	if right {
		return padding + value
	}
	return value + padding
}

func cleanCell(value string) string {
	return strings.Map(func(char rune) rune {
		if char == '\r' || char == '\n' || char == '\t' {
			return ' '
		}
		return char
	}, value)
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func safeName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name)
	if name == "." || name == "/" || name == "" {
		return "entry"
	}
	return name
}

func uniqueName(used map[string]int, name string) string {
	used[name]++
	if used[name] == 1 {
		return name
	}
	extension := filepath.Ext(name)
	base := strings.TrimSuffix(name, extension)
	for {
		candidate := fmt.Sprintf("%s (%d)%s", base, used[name], extension)
		if used[candidate] == 0 {
			used[candidate]++
			return candidate
		}
		used[name]++
	}
}

// parseFlags parses flag set flags that may appear before, between or after
// positional arguments, returning the positional arguments in order.
func parseFlags(flags *flag.FlagSet, args []string) ([]string, error) {
	var flagArgs, positional []string
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			positional = append(positional, args[index+1:]...)
			break
		}
		if len(arg) < 2 || arg[0] != '-' {
			positional = append(positional, arg)
			continue
		}
		trimmed := strings.TrimLeft(arg, "-")
		name, _, hasValue := strings.Cut(trimmed, "=")
		defined := flags.Lookup(name)
		if defined == nil {
			if _, err := strconv.ParseFloat(trimmed, 64); err == nil {
				positional = append(positional, arg)
				continue
			}
			return nil, fmt.Errorf("flag provided but not defined: -%s", name)
		}
		flagArgs = append(flagArgs, arg)
		if !hasValue && !isBoolFlag(defined) {
			if index+1 >= len(args) {
				return nil, fmt.Errorf("flag needs an argument: -%s", name)
			}
			index++
			flagArgs = append(flagArgs, args[index])
		}
	}
	if err := flags.Parse(flagArgs); err != nil {
		return nil, err
	}
	return positional, nil
}

func isBoolFlag(defined *flag.Flag) bool {
	boolean, ok := defined.Value.(interface{ IsBoolFlag() bool })
	return ok && boolean.IsBoolFlag()
}

func writeJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func writeFileAtomic(path string, data []byte) error {
	directory := filepath.Dir(path)
	temp, err := os.CreateTemp(directory, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	perm := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
	}
	_ = os.Chmod(name, perm)
	return os.Rename(name, path)
}

func usage(output *os.File) {
	fmt.Fprint(output, `nfs4viv reads and edits Need for Speed 4 car.viv archives.

Copyright (c) nfs4.com

Usage:
  nfs4viv serials [-json] [-lang EXT] [-sort COLUMN] [-check] [-check-off] [-color MODE] [DIR]
      List every car found in DIR. Each immediate subfolder (car00, car01,
      ...) is expected to contain a car.viv archive. The list shows the
      folder, four character car ID, serial number, car name, class, police
      and bonus flags, upgradability and the base price.
      DIR defaults to ./DATA/CARS relative to the working directory.
      -sort selects the first sort column (default serial); class is always
      used as the second key and name as the third unless selected.
      Duplicate checking is enabled by default: a DUPLICATES column reports
      cars sharing a serial number, a car ID or a name plus class pair; the
      conflicting CAR_ID, SERIAL and NAME cells are highlighted with a red
      background and the conflicts are also reported on stderr. -check-off
      (or -check=false) disables it.
      -color accepts auto (default: only on a terminal), always or never. On
      Windows the console is switched into virtual terminal mode, so the
      colors also work in PowerShell and cmd.exe.

  nfs4viv fedata show [-json] [-entry NAME] [-lang EXT|all] INPUT.VIV|FEDATA
      Print every editable text and numeric field of a fedata file, either
      read directly or from inside a VIV archive. -lang all reads every known
      localization (eng, bri, fre, ger, ita, spa, swe) present in the
      archive; without it the first available localization is used.

  nfs4viv fedata set [-o OUTPUT] [-entry NAME] [-lang EXT|all] INPUT.VIV|FEDATA FIELD VALUE [FIELD VALUE ...]
      Change one or more fedata fields. The input may be a car.viv archive
      (the fedata entry is updated and the archive repacked) or a standalone
      fedata file. -lang all applies the same changes to every known
      localization present in the archive. Without -o the input file is
      overwritten.

  nfs4viv fedata export [-o OUTPUT] [-entry NAME] [-lang EXT|all] INPUT.VIV|FEDATA
      Write the editable fields as flat JSON, to OUTPUT or stdout. One object
      is written for a single fedata entry; -lang all writes an array of
      objects, each carrying its "entry" name.

  nfs4viv fedata import [-o OUTPUT] [-entry NAME] [-lang EXT|all] INPUT.VIV|FEDATA [JSON]
      Apply fields from JSON produced by fedata export. The JSON is read from
      the given file, or from stdin when omitted or "-". Values may be
      strings, numbers or booleans; unknown fields are rejected. With
      -lang all a single object updates every localization, while an array
      updates only the entries named in it (no -lang needed for an array).
      Without -o the input is overwritten.

  nfs4viv fedata fields
      List every field accepted by show and set.

  nfs4viv viv ls [-json] INPUT.VIV
  nfs4viv viv read INPUT.VIV ENTRY [-o FILE]
  nfs4viv viv extract INPUT.VIV [ENTRY ...] [-o DIR]
  nfs4viv viv replace INPUT.VIV ENTRY FILE [-o OUTPUT.VIV]
  nfs4viv viv add INPUT.VIV ENTRY FILE [-o OUTPUT.VIV]
  nfs4viv viv rm INPUT.VIV ENTRY [-o OUTPUT.VIV]
      Generic VIV archive utilities. replace, add and rm overwrite the input
      unless -o is given.

  nfs4viv version
  nfs4viv help

Fields use snake_case names: car_id, serial, car_name, class, police, bonus,
upgradable, dlc, roof, engine_location, price, upgrade_1_price,
upgrade_2_price, upgrade_3_price, rating_acceleration, rating_top_speed,
rating_handling, rating_braking, rating_overall (each rating also accepts a
_1, _2 or _3 upgrade suffix), plus the localized strings: manufacturer,
model, price_text, status, weight, weight_distribution, length, width,
height, engine, displacement, hp, torque, max_engine_speed, brakes, tires,
dynamic_stability, top_speed, accel_0_to_60, accel_0_to_100, transmission,
gearbox, history_1..history_8 and color_1..color_10.

Boolean fields accept yes/no; class accepts AAA, AA, A, B or 0..3; roof
accepts solid, convertible, no_roof or 0..2; engine_location accepts front,
mid, rear or 0..2; police accepts yes, no, no_mercedes, no_ferrari or a raw
0x-prefixed flag; numbers accept decimal or 0x-prefixed values.

The police field is the high nibble of the fedata flag byte. Only 0x10 marks
a police car; no_mercedes (0x20) and no_ferrari (0xA0) are non-police cars
whose flag carries a manufacturer specific value used by the game.

Examples:
  nfs4viv serials "C:\Games\NFS4\DATA\CARS"
  nfs4viv serials -sort class -check
  nfs4viv serials -json -lang ger "C:\Games\NFS4\DATA\CARS" > carviv.json
  nfs4viv fedata set "C:\Games\NFS4\DATA\CARS\car07\car.viv" serial 12
  nfs4viv fedata set "C:\Games\NFS4\DATA\CARS\car07\car.viv" -o patched.viv car_name "Ferrari F50 GT" price 500000
`)
}
