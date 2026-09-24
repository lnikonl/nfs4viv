// Package fedata reads and writes Need for Speed 4 Front-End Data (fedata)
// files. Each NFS4 car keeps a localized fedata file (fedata.eng and friends)
// inside its car.viv archive; that file carries the car ID, serial number,
// class, police/bonus/upgradable flags, prices and every localized text shown
// in the front end.
package fedata

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Magic is the first byte of every NFS4 fedata file.
const Magic = 0x04

const (
	// HeaderSize is the size of the fixed NFS4 fedata header.
	HeaderSize = 0x3C0
	// StringCount is the number of string entries NFS4 fedata files carry.
	StringCount = 41
)

// Header field offsets, as laid out by the game.
const (
	offCarID           = 0x112
	offSerial          = 0x31E
	offPoliceBonus     = 0x37A
	offUpgradableFlags = 0x37B
	offCarClass        = 0x382
	offCompareCount    = 0x388
	offCompareData     = 0x389
	offBasePrice       = 0x39E
	offUpgrade1Price   = 0x3A2
	offUpgrade2Price   = 0x3A6
	offUpgrade3Price   = 0x3AA
	offEngineLocation  = 0x3BB
	offStringEntries   = 0x3BE
)

// Bit masks used by the flag bytes.
const (
	// PoliceMask selects the police car flag, a PursuitFlag value.
	PoliceMask = 0xF0
	// BonusMask marks the car as an unlockable bonus car.
	BonusMask = 0x01
	// UpgradableMask is set when the car cannot be upgraded in career mode.
	UpgradableMask = 0x40
	// RoofMask selects the roof flag (solid, convertible, no roof).
	RoofMask = 0x03
	// DLCMask marks the car as downloaded content.
	DLCMask = 0x04
)

// Police flag values.
const (
	PoliceNo         = 0x00
	PoliceYes        = 0x10
	PoliceNoMercedes = 0x20
	PoliceNoFerrari  = 0xA0
)

const maxStringEntries = 0x1000

// File is a parsed NFS4 fedata file. The original header bytes are kept so
// that padding and unknown fields survive a rewrite.
type File struct {
	raw   [HeaderSize]byte
	texts [StringCount]string
}

// New returns an empty fedata file with the mandatory header fields filled in.
func New() *File {
	file := &File{}
	file.raw[0] = Magic
	file.raw[offCompareCount] = 10
	binary.LittleEndian.PutUint16(file.raw[offStringEntries:], StringCount)
	return file
}

// Parse reads a NFS4 fedata file from raw contents.
func Parse(data []byte) (*File, error) {
	if len(data) < HeaderSize {
		return nil, fmt.Errorf("fedata: file of %d bytes is too small (need at least %d)", len(data), HeaderSize)
	}
	if data[0] != Magic {
		return nil, fmt.Errorf("fedata: bad magic 0x%02X, expected 0x%02X (NFS4)", data[0], Magic)
	}
	file := &File{}
	copy(file.raw[:], data[:HeaderSize])
	entries := int(binary.LittleEndian.Uint16(file.raw[offStringEntries:]))
	if entries > maxStringEntries {
		return nil, fmt.Errorf("fedata: implausible string entry count %d", entries)
	}
	if headerSize := HeaderSize + entries*4; headerSize > len(data) {
		return nil, fmt.Errorf("fedata: truncated string offset table (%d entries)", entries)
	}
	for index := 0; index < entries && index < StringCount; index++ {
		offset := int64(binary.LittleEndian.Uint32(data[HeaderSize+index*4:]))
		if offset >= int64(len(data)) {
			continue
		}
		end := bytes.IndexByte(data[offset:], 0)
		if end < 0 {
			end = len(data) - int(offset)
		}
		file.texts[index] = decodeLatin1(data[offset : offset+int64(end)])
	}
	return file, nil
}

// ParseFile reads a NFS4 fedata file from disk.
func ParseFile(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	file, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return file, nil
}

// Bytes serializes the file, rebuilding the string offset table and pool.
func (f *File) Bytes() []byte {
	out := make([]byte, HeaderSize+StringCount*4)
	copy(out, f.raw[:])
	binary.LittleEndian.PutUint16(out[offStringEntries:], StringCount)
	for index := 0; index < StringCount; index++ {
		binary.LittleEndian.PutUint32(out[HeaderSize+index*4:], uint32(len(out)))
		out = append(out, encodeLatin1(f.texts[index])...)
		out = append(out, 0)
	}
	return out
}

// CarID returns the four character car ID without padding.
func (f *File) CarID() string {
	return strings.TrimRight(decodeLatin1(f.raw[offCarID:offCarID+4]), "\x00 ")
}

// Serial returns the car serial number.
func (f *File) Serial() uint16 {
	return binary.LittleEndian.Uint16(f.raw[offSerial:])
}

// PoliceFlag returns the raw pursuit flag.
func (f *File) PoliceFlag() byte {
	return f.raw[offPoliceBonus] & PoliceMask
}

// Police reports whether the car is a police car.
func (f *File) Police() bool {
	return f.PoliceFlag() == PoliceYes
}

// PoliceName describes the pursuit flag in words.
func (f *File) PoliceName() string {
	switch f.PoliceFlag() {
	case PoliceYes:
		return "yes"
	case PoliceNoMercedes:
		return "no (mercedes)"
	case PoliceNoFerrari:
		return "no (ferrari)"
	default:
		return "no"
	}
}

// Bonus reports whether the car is an unlockable bonus car.
func (f *File) Bonus() bool {
	return f.raw[offPoliceBonus]&BonusMask != 0
}

// Upgradable reports whether the car can be upgraded in career mode.
func (f *File) Upgradable() bool {
	return f.raw[offUpgradableFlags]&UpgradableMask == 0
}

// DLC reports whether the car came from a downloadable content pack.
func (f *File) DLC() bool {
	return f.raw[offUpgradableFlags]&DLCMask != 0
}

// Roof returns the raw roof flag.
func (f *File) Roof() byte {
	return f.raw[offUpgradableFlags] & RoofMask
}

// RoofName describes the roof flag in words.
func (f *File) RoofName() string {
	return RoofName(f.Roof())
}

// CarClass returns the raw vehicle performance class.
func (f *File) CarClass() byte {
	return f.raw[offCarClass]
}

// ClassName describes the vehicle performance class in words.
func (f *File) ClassName() string {
	return ClassName(f.CarClass())
}

// EngineLocation returns the raw engine location.
func (f *File) EngineLocation() byte {
	return f.raw[offEngineLocation]
}

// EngineLocationName describes the engine location in words.
func (f *File) EngineLocationName() string {
	return EngineLocationName(f.EngineLocation())
}

// BasePrice returns the default car price.
func (f *File) BasePrice() int32 {
	return int32(binary.LittleEndian.Uint32(f.raw[offBasePrice:]))
}

// UpgradePrice returns the price of the given upgrade level (1..3).
func (f *File) UpgradePrice(level int) int32 {
	if level < 1 || level > 3 {
		return 0
	}
	return int32(binary.LittleEndian.Uint32(f.raw[offUpgrade1Price+(level-1)*4:]))
}

// Rating returns one compare table value. Category 0..4 selects Acceleration,
// Top Speed, Handling, Braking or Overall; level 0..3 selects the default
// value or one of the three upgrade levels.
func (f *File) Rating(category, level int) byte {
	if category < 0 || category > 4 || level < 0 || level > 3 {
		return 0
	}
	return f.raw[compareOffset(category, level)]
}

// Text returns the localized string stored at the given offset table index.
func (f *File) Text(index int) string {
	if index < 0 || index >= StringCount {
		return ""
	}
	return f.texts[index]
}

// CarName returns the localized car name (offset table index 2).
func (f *File) CarName() string {
	return f.texts[2]
}

// PriceText returns the localized price string (offset table index 3).
func (f *File) PriceText() string {
	return f.texts[3]
}

// TextField maps a localized string name to its offset table index.
type TextField struct {
	Name  string
	Index int
}

var textFields = []TextField{
	{"manufacturer", 0},
	{"model", 1},
	{"car_name", 2},
	{"price_text", 3},
	{"status", 4},
	{"weight", 5},
	{"weight_distribution", 6},
	{"length", 7},
	{"width", 8},
	{"height", 9},
	{"engine", 10},
	{"displacement", 11},
	{"hp", 12},
	{"torque", 13},
	{"max_engine_speed", 14},
	{"brakes", 15},
	{"tires", 16},
	{"dynamic_stability", 17},
	{"top_speed", 18},
	{"accel_0_to_60", 19},
	{"accel_0_to_100", 20},
	{"transmission", 21},
	{"gearbox", 22},
	{"history_1", 23},
	{"history_2", 24},
	{"history_3", 25},
	{"history_4", 26},
	{"history_5", 27},
	{"history_6", 28},
	{"history_7", 29},
	{"history_8", 30},
	{"color_1", 31},
	{"color_2", 32},
	{"color_3", 33},
	{"color_4", 34},
	{"color_5", 35},
	{"color_6", 36},
	{"color_7", 37},
	{"color_8", 38},
	{"color_9", 39},
	{"color_10", 40},
}

var textIndexes = func() map[string]int {
	indexes := make(map[string]int, len(textFields))
	for _, field := range textFields {
		indexes[field.Name] = field.Index
	}
	return indexes
}()

var scalarFields = []string{
	"car_id", "serial", "class", "police", "bonus", "upgradable", "dlc", "roof",
	"engine_location", "price", "upgrade_1_price", "upgrade_2_price", "upgrade_3_price",
	"rating_acceleration", "rating_acceleration_1", "rating_acceleration_2", "rating_acceleration_3",
	"rating_top_speed", "rating_top_speed_1", "rating_top_speed_2", "rating_top_speed_3",
	"rating_handling", "rating_handling_1", "rating_handling_2", "rating_handling_3",
	"rating_braking", "rating_braking_1", "rating_braking_2", "rating_braking_3",
	"rating_overall", "rating_overall_1", "rating_overall_2", "rating_overall_3",
}

// TextFields lists every localized string field in offset table order.
func TextFields() []TextField {
	return append([]TextField{}, textFields...)
}

// AllFields lists every editable field in a stable order.
func AllFields() []string {
	names := append([]string{}, scalarFields...)
	for _, field := range textFields {
		names = append(names, field.Name)
	}
	return names
}

// Get returns the current value of a field as display text.
func (f *File) Get(name string) (string, error) {
	if index, ok := textIndexes[name]; ok {
		return f.texts[index], nil
	}
	switch name {
	case "car_id":
		return f.CarID(), nil
	case "serial", "serial_number":
		return strconv.FormatUint(uint64(f.Serial()), 10), nil
	case "class", "car_class":
		return f.ClassName(), nil
	case "police":
		return f.PoliceName(), nil
	case "bonus":
		return yesNo(f.Bonus()), nil
	case "upgradable":
		return yesNo(f.Upgradable()), nil
	case "dlc":
		return yesNo(f.DLC()), nil
	case "roof":
		return f.RoofName(), nil
	case "engine_location":
		return f.EngineLocationName(), nil
	case "price":
		return strconv.FormatInt(int64(f.BasePrice()), 10), nil
	case "upgrade_1_price":
		return strconv.FormatInt(int64(f.UpgradePrice(1)), 10), nil
	case "upgrade_2_price":
		return strconv.FormatInt(int64(f.UpgradePrice(2)), 10), nil
	case "upgrade_3_price":
		return strconv.FormatInt(int64(f.UpgradePrice(3)), 10), nil
	}
	if category, level, ok := ratingField(name); ok {
		return strconv.Itoa(int(f.Rating(category, level))), nil
	}
	return "", fmt.Errorf("unknown field %q; run nfs4viv fedata fields", name)
}

// Set updates a field from a display value.
func (f *File) Set(name, value string) error {
	if index, ok := textIndexes[name]; ok {
		if strings.IndexByte(value, 0) >= 0 {
			return fmt.Errorf("field %s must not contain a NUL byte", name)
		}
		f.texts[index] = value
		return nil
	}
	switch name {
	case "car_id":
		if strings.IndexByte(value, 0) >= 0 {
			return fmt.Errorf("field car_id must not contain a NUL byte")
		}
		encoded := encodeLatin1(value)
		if len(encoded) > 4 {
			return fmt.Errorf("field car_id holds at most 4 characters, got %q", value)
		}
		var carID [4]byte
		copy(carID[:], encoded)
		copy(f.raw[offCarID:offCarID+4], carID[:])
	case "serial", "serial_number":
		number, err := parseUint(name, value, 16)
		if err != nil {
			return err
		}
		binary.LittleEndian.PutUint16(f.raw[offSerial:], uint16(number))
	case "class", "car_class":
		class, err := parseClass(value)
		if err != nil {
			return err
		}
		f.raw[offCarClass] = class
	case "police":
		flag, err := parsePolice(value)
		if err != nil {
			return err
		}
		f.raw[offPoliceBonus] = flag | (f.raw[offPoliceBonus] & BonusMask)
	case "bonus":
		enabled, err := parseBool(name, value)
		if err != nil {
			return err
		}
		f.raw[offPoliceBonus] = setBit(f.raw[offPoliceBonus], BonusMask, enabled)
	case "upgradable":
		enabled, err := parseBool(name, value)
		if err != nil {
			return err
		}
		f.raw[offUpgradableFlags] = setBit(f.raw[offUpgradableFlags], UpgradableMask, !enabled)
	case "dlc":
		enabled, err := parseBool(name, value)
		if err != nil {
			return err
		}
		f.raw[offUpgradableFlags] = setBit(f.raw[offUpgradableFlags], DLCMask, enabled)
	case "roof":
		roof, err := parseRoof(value)
		if err != nil {
			return err
		}
		f.raw[offUpgradableFlags] = (f.raw[offUpgradableFlags] &^ RoofMask) | roof
	case "engine_location":
		location, err := parseEngineLocation(value)
		if err != nil {
			return err
		}
		f.raw[offEngineLocation] = location
	case "price":
		price, err := parseInt(name, value)
		if err != nil {
			return err
		}
		binary.LittleEndian.PutUint32(f.raw[offBasePrice:], uint32(price))
	case "upgrade_1_price", "upgrade_2_price", "upgrade_3_price":
		level := int(name[len("upgrade_")] - '0')
		price, err := parseInt(name, value)
		if err != nil {
			return err
		}
		binary.LittleEndian.PutUint32(f.raw[offUpgrade1Price+(level-1)*4:], uint32(price))
	default:
		if category, level, ok := ratingField(name); ok {
			rating, err := parseUint(name, value, 8)
			if err != nil {
				return err
			}
			f.raw[compareOffset(category, level)] = byte(rating)
			return nil
		}
		return fmt.Errorf("unknown field %q; run nfs4viv fedata fields", name)
	}
	return nil
}

// ClassName names a vehicle performance class.
func ClassName(class byte) string {
	switch class {
	case 0:
		return "AAA"
	case 1:
		return "AA"
	case 2:
		return "A"
	case 3:
		return "B"
	default:
		return fmt.Sprintf("unknown (0x%02X)", class)
	}
}

// RoofName names a roof flag.
func RoofName(roof byte) string {
	switch roof {
	case 0:
		return "solid"
	case 1:
		return "convertible"
	case 2:
		return "no roof"
	default:
		return fmt.Sprintf("unknown (0x%02X)", roof)
	}
}

// EngineLocationName names an engine location.
func EngineLocationName(location byte) string {
	switch location {
	case 0:
		return "front"
	case 1:
		return "mid"
	case 2:
		return "rear"
	default:
		return fmt.Sprintf("unknown (0x%02X)", location)
	}
}

var ratingCategories = map[string]int{
	"acceleration": 0,
	"top_speed":    1,
	"handling":     2,
	"braking":      3,
	"overall":      4,
}

func ratingField(name string) (category, level int, ok bool) {
	for prefix, index := range ratingCategories {
		if name == "rating_"+prefix {
			return index, 0, true
		}
		for level := 1; level <= 3; level++ {
			if name == fmt.Sprintf("rating_%s_%d", prefix, level) {
				return index, level, true
			}
		}
	}
	return 0, 0, false
}

func compareOffset(category, level int) int {
	return offCompareData + category*4 + level
}

func parseBool(name, value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes", "true", "1", "on":
		return true, nil
	case "no", "false", "0", "off":
		return false, nil
	}
	return false, fmt.Errorf("field %s expects yes or no, got %q", name, value)
}

func parseUint(name, value string, bits int) (uint64, error) {
	number, err := strconv.ParseUint(strings.TrimSpace(value), 0, bits)
	if err != nil {
		return 0, fmt.Errorf("field %s expects an integer, got %q", name, value)
	}
	return number, nil
}

func parseInt(name, value string) (int32, error) {
	number, err := strconv.ParseInt(strings.TrimSpace(value), 0, 32)
	if err != nil {
		return 0, fmt.Errorf("field %s expects an integer, got %q", name, value)
	}
	return int32(number), nil
}

func parseClass(value string) (byte, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "AAA":
		return 0, nil
	case "AA":
		return 1, nil
	case "A":
		return 2, nil
	case "B":
		return 3, nil
	}
	if number, ok := parseUnknownHex(value); ok {
		return number, nil
	}
	number, err := strconv.ParseUint(strings.TrimSpace(value), 0, 8)
	if err != nil || number > 3 {
		return 0, fmt.Errorf("field class expects AAA, AA, A, B or 0..3, got %q", value)
	}
	return byte(number), nil
}

func parseRoof(value string) (byte, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "solid", "solid_roof", "solid roof":
		return 0, nil
	case "convertible":
		return 1, nil
	case "none", "no_roof", "no roof":
		return 2, nil
	}
	if number, ok := parseUnknownHex(value); ok {
		return number, nil
	}
	number, err := strconv.ParseUint(strings.TrimSpace(value), 0, 8)
	if err != nil || number > 2 {
		return 0, fmt.Errorf("field roof expects solid, convertible, no_roof or 0..2, got %q", value)
	}
	return byte(number), nil
}

func parseEngineLocation(value string) (byte, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "front":
		return 0, nil
	case "mid", "middle":
		return 1, nil
	case "rear":
		return 2, nil
	}
	if number, ok := parseUnknownHex(value); ok {
		return number, nil
	}
	number, err := strconv.ParseUint(strings.TrimSpace(value), 0, 8)
	if err != nil || number > 2 {
		return 0, fmt.Errorf("field engine_location expects front, mid, rear or 0..2, got %q", value)
	}
	return byte(number), nil
}

func parsePolice(value string) (byte, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes", "true", "1", "on", "police":
		return PoliceYes, nil
	case "no", "false", "0", "off":
		return PoliceNo, nil
	case "no_mercedes", "mercedes", "no (mercedes)":
		return PoliceNoMercedes, nil
	case "no_ferrari", "ferrari", "no (ferrari)":
		return PoliceNoFerrari, nil
	}
	if number, ok := parseUnknownHex(value); ok {
		return number & PoliceMask, nil
	}
	number, err := strconv.ParseUint(strings.TrimSpace(value), 0, 8)
	if err != nil {
		return 0, fmt.Errorf("field police expects yes, no, no_mercedes, no_ferrari or a raw flag, got %q", value)
	}
	return byte(number) & PoliceMask, nil
}

// parseUnknownHex accepts the "unknown (0xNN)" wording produced by the
// display helpers, so an exported file imports back unchanged even when it
// contains values outside the documented ranges.
func parseUnknownHex(value string) (byte, bool) {
	text := strings.ToLower(strings.TrimSpace(value))
	const prefix = "unknown (0x"
	if !strings.HasPrefix(text, prefix) || !strings.HasSuffix(text, ")") {
		return 0, false
	}
	number, err := strconv.ParseUint(text[len(prefix):len(text)-1], 16, 8)
	if err != nil {
		return 0, false
	}
	return byte(number), true
}

func setBit(value, mask byte, enabled bool) byte {
	if enabled {
		return value | mask
	}
	return value &^ mask
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func decodeLatin1(data []byte) string {
	runes := make([]rune, len(data))
	for index, char := range data {
		runes[index] = rune(char)
	}
	return string(runes)
}

func encodeLatin1(value string) []byte {
	encoded := make([]byte, 0, len(value))
	for _, char := range value {
		if char > 0xFF {
			char = '?'
		}
		encoded = append(encoded, byte(char))
	}
	return encoded
}
