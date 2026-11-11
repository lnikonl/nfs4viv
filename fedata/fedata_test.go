package fedata

import (
	"encoding/binary"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	file := New()
	settings := map[string]string{
		"car_id":              "F50",
		"serial":              "17",
		"car_name":            "Ferrari F50",
		"manufacturer":        "Ferrari",
		"model":               "F50",
		"price_text":          "$350,000",
		"class":               "AAA",
		"police":              "yes",
		"bonus":               "yes",
		"upgradable":          "no",
		"roof":                "convertible",
		"engine_location":     "rear",
		"price":               "350000",
		"upgrade_1_price":     "10000",
		"rating_acceleration": "18",
		"rating_top_speed_3":  "20",
		"history_1":           "First win",
		"color_10":            "Atlanta Blue",
	}
	for name, value := range settings {
		if err := file.Set(name, value); err != nil {
			t.Fatalf("Set(%s): %v", name, err)
		}
	}
	encoded := file.Bytes()
	if encoded[0] != Magic {
		t.Fatalf("magic = 0x%02X", encoded[0])
	}
	if got := binary.LittleEndian.Uint16(encoded[offSerial:]); got != 17 {
		t.Fatalf("serial = %d", got)
	}
	if got := binary.LittleEndian.Uint16(encoded[offStringEntries:]); got != StringCount {
		t.Fatalf("string entries = %d", got)
	}
	if len(encoded) <= HeaderSize+StringCount*4 {
		t.Fatalf("size = %d, expected a string pool", len(encoded))
	}
	parsed, err := Parse(encoded)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for name, value := range settings {
		got, err := parsed.Get(name)
		if err != nil {
			t.Fatalf("Get(%s): %v", name, err)
		}
		if got != value {
			t.Errorf("Get(%s) = %q, want %q", name, got, value)
		}
	}
}

func TestDefaults(t *testing.T) {
	file := New()
	if file.CarID() != "" {
		t.Errorf("car id = %q", file.CarID())
	}
	if file.Serial() != 0 {
		t.Errorf("serial = %d", file.Serial())
	}
	if file.ClassName() != "AAA" {
		t.Errorf("class = %q", file.ClassName())
	}
	if !file.Upgradable() {
		t.Error("new cars must be upgradable")
	}
	if file.Bonus() || file.Police() || file.DLC() {
		t.Error("new cars must not be bonus, police or DLC")
	}
	if file.RoofName() != "solid" {
		t.Errorf("roof = %q", file.RoofName())
	}
}

func TestFlagBits(t *testing.T) {
	file := New()
	for name, value := range map[string]string{
		"police": "no_mercedes",
		"bonus":  "yes",
		"roof":   "convertible",
		"dlc":    "yes",
	} {
		if err := file.Set(name, value); err != nil {
			t.Fatalf("Set(%s): %v", name, err)
		}
	}
	parsed, err := Parse(file.Bytes())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.PoliceFlag() != PoliceNoMercedes {
		t.Errorf("police flag = 0x%02X", parsed.PoliceFlag())
	}
	if parsed.Police() {
		t.Error("mercedes flag must not count as a police car")
	}
	if !parsed.Bonus() {
		t.Error("bonus bit not set")
	}
	if !parsed.DLC() {
		t.Error("dlc bit not set")
	}
	if parsed.Roof() != 1 {
		t.Errorf("roof = %d", parsed.Roof())
	}
	if !parsed.Upgradable() {
		t.Error("upgradable bit must remain clear")
	}
}

func TestUpgradableInvertedBit(t *testing.T) {
	file := New()
	if err := file.Set("upgradable", "no"); err != nil {
		t.Fatal(err)
	}
	if file.raw[offUpgradableFlags]&UpgradableMask == 0 {
		t.Fatal("upgradable=no must set the flag bit")
	}
	parsed, _ := Parse(file.Bytes())
	if parsed.Upgradable() {
		t.Fatal("car must not be upgradable")
	}
}

func TestLatin1Strings(t *testing.T) {
	file := New()
	if err := file.Set("car_name", "Citroën C4"); err != nil {
		t.Fatal(err)
	}
	if err := file.Set("manufacturer", "Café"); err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(file.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if parsed.CarName() != "Citroën C4" {
		t.Errorf("car name = %q", parsed.CarName())
	}
	if parsed.Text(0) != "Café" {
		t.Errorf("manufacturer = %q", parsed.Text(0))
	}
}

func TestCompareValues(t *testing.T) {
	file := New()
	for name, value := range map[string]string{
		"rating_acceleration_2": "19",
		"rating_braking":        "7",
	} {
		if err := file.Set(name, value); err != nil {
			t.Fatal(err)
		}
	}
	if file.Rating(0, 2) != 19 {
		t.Errorf("acceleration upgrade 2 = %d", file.Rating(0, 2))
	}
	if file.Rating(3, 0) != 7 {
		t.Errorf("braking = %d", file.Rating(3, 0))
	}
}

func TestParseErrors(t *testing.T) {
	if _, err := Parse(make([]byte, 10)); err == nil {
		t.Error("expected a size error")
	}
	bad := New().Bytes()
	bad[0] = 0x03
	if _, err := Parse(bad); err == nil {
		t.Error("expected a magic error")
	}
	truncated := make([]byte, HeaderSize)
	copy(truncated, New().Bytes()[:HeaderSize])
	if _, err := Parse(truncated); err == nil {
		t.Error("expected a truncated string table error")
	}
}

func TestSetDisplayForms(t *testing.T) {
	file := New()
	for name, value := range map[string]string{
		"police": "no (ferrari)",
		"class":  "unknown (0x07)",
		"roof":   "unknown (0x03)",
	} {
		if err := file.Set(name, value); err != nil {
			t.Fatalf("Set(%s, %s): %v", name, value, err)
		}
	}
	if file.PoliceFlag() != PoliceNoFerrari {
		t.Errorf("police flag = 0x%02X", file.PoliceFlag())
	}
	if file.CarClass() != 7 {
		t.Errorf("class = %d", file.CarClass())
	}
	if file.Roof() != 3 {
		t.Errorf("roof = %d", file.Roof())
	}
	if err := file.Set("police", "no (mercedes)"); err != nil {
		t.Fatalf("Set(police): %v", err)
	}
	if file.PoliceFlag() != PoliceNoMercedes {
		t.Errorf("police flag = 0x%02X", file.PoliceFlag())
	}
}

func TestSetErrors(t *testing.T) {
	file := New()
	for name, value := range map[string]string{
		"car_id":   "TOOLONG",
		"serial":   "not-a-number",
		"class":    "Z",
		"police":   "maybe",
		"roof":     "sunroof",
		"unknown":  "1",
		"price":    "abc",
		"rating_x": "1",
	} {
		if err := file.Set(name, value); err == nil {
			t.Errorf("Set(%s, %s) must fail", name, value)
		}
	}
	if _, err := file.Get("unknown"); err == nil {
		t.Error("Get must reject unknown fields")
	}
}
