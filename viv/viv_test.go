package viv

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	original := &File{Entries: []Entry{
		{Name: "fedata.eng", Data: []byte("hello fedata")},
		{Name: "car.fce", Data: []byte{0, 1, 2, 3, 0xFF}},
		{Name: "empty.bnk", Data: nil},
	}}
	encoded, err := original.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if !bytes.Equal(encoded[:4], Magic) {
		t.Fatalf("magic = %q", encoded[:4])
	}
	if got := binary.BigEndian.Uint32(encoded[4:8]); int(got) != len(encoded) {
		t.Fatalf("declared length = %d, actual %d", got, len(encoded))
	}
	if got := binary.BigEndian.Uint32(encoded[8:12]); got != 3 {
		t.Fatalf("entry count = %d", got)
	}
	parsed, err := Read(encoded)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(parsed.Entries) != len(original.Entries) {
		t.Fatalf("entries = %d, want %d", len(parsed.Entries), len(original.Entries))
	}
	for index := range original.Entries {
		if parsed.Entries[index].Name != original.Entries[index].Name {
			t.Errorf("entry %d name = %q, want %q", index, parsed.Entries[index].Name, original.Entries[index].Name)
		}
		if !bytes.Equal(parsed.Entries[index].Data, original.Entries[index].Data) {
			t.Errorf("entry %d data = %x, want %x", index, parsed.Entries[index].Data, original.Entries[index].Data)
		}
	}
}

func TestDirectoryOperations(t *testing.T) {
	file := &File{Entries: []Entry{{Name: "car.fce", Data: []byte("1")}}}
	if _, ok := file.Get("CAR.FCE"); !ok {
		t.Fatal("Get must be case insensitive")
	}
	if !file.Set("CAR.FCE", []byte("2")) {
		t.Fatal("Set must find existing entries ignoring case")
	}
	if file.Add("Car.Fce", []byte("3")) {
		t.Fatal("Add must refuse duplicate names")
	}
	if !file.Add("fedata.eng", []byte("4")) {
		t.Fatal("Add must accept new names")
	}
	if len(file.Entries) != 2 {
		t.Fatalf("entries = %d", len(file.Entries))
	}
	if !file.Remove("car.fce") {
		t.Fatal("Remove must delete entries")
	}
	if file.Remove("car.fce") {
		t.Fatal("Remove must report missing entries")
	}
	if len(file.Entries) != 1 || file.Entries[0].Name != "fedata.eng" {
		t.Fatalf("entries after remove = %+v", file.Entries)
	}
}

func TestReadRejectsBadInput(t *testing.T) {
	cases := map[string][]byte{
		"short": []byte("BIGF"),
		"magic": []byte("NOPEAAAAAAAAAAAA"),
		"truncated": func() []byte {
			data := make([]byte, 16)
			copy(data, "BIGF")
			binary.BigEndian.PutUint32(data[8:12], 1)
			return data
		}(),
	}
	for name, data := range cases {
		if _, err := Read(data); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestReadRejectsOutOfBoundsEntry(t *testing.T) {
	data := make([]byte, 26)
	copy(data, "BIGF")
	binary.BigEndian.PutUint32(data[4:8], uint32(len(data)))
	binary.BigEndian.PutUint32(data[8:12], 1)
	binary.BigEndian.PutUint32(data[12:16], uint32(len(data)))
	binary.BigEndian.PutUint32(data[16:20], 0xFFFF)
	binary.BigEndian.PutUint32(data[20:24], 10)
	data[24] = 'a'
	if _, err := Read(data); err == nil {
		t.Fatal("expected an out of bounds error")
	}
}

func TestBytesRejectsEmptyName(t *testing.T) {
	if _, err := (&File{Entries: []Entry{{Name: ""}}}).Bytes(); err == nil {
		t.Fatal("expected a name error")
	}
}
