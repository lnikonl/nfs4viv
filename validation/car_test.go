package validation

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/lnikonl/nfs4viv/cartest"
	"github.com/lnikonl/nfs4viv/viv"
)

func TestCarProfile(t *testing.T) {
	report, err := Check("car", cartest.ResourceArchive(), Options{MaxTextureSize: 256})
	if err != nil || !report.Valid || len(report.Issues) != 0 || report.Checked != 9 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	if _, err := Check("typo", &viv.File{}, Options{}); err == nil {
		t.Fatal("unknown validation profile accepted")
	}
}

func TestCarRejectsBadResources(t *testing.T) {
	for _, test := range []struct {
		name, entry string
		change      func([]byte)
	}{
		{"24-bit TGA", "car00.tga", func(b []byte) { b[16] = 24 }},
		{"RLE TGA", "car00.tga", func(b []byte) { b[2] = 10 }},
		{"TGA color map", "car00.tga", func(b []byte) { b[1] = 1 }},
		{"FCE vertices", "car.fce", func(b []byte) { binary.LittleEndian.PutUint32(b[0xc:], 0xffffffff) }},
		{"FCE indices", "car.fce", func(b []byte) {
			off := 0x2038 + int(binary.LittleEndian.Uint32(b[0x1c:]))
			binary.LittleEndian.PutUint32(b[off+4:], 99)
		}},
		{"FSH offset", "dash.fsh", func(b []byte) { binary.LittleEndian.PutUint32(b[20:], 0xffffffff) }},
		{"overlapping FSH", "dash.fsh", func(b []byte) { binary.LittleEndian.PutUint32(b[28:], 33) }},
		{"ART zero size", "cop.art", func(b []byte) { binary.LittleEndian.PutUint32(b, 0) }},
		{"ART truncated pixels", "cop.art", func(b []byte) { binary.LittleEndian.PutUint32(b, 0xffffffff) }},
		{"BNK offset", "careng.bnk", func(b []byte) { binary.LittleEndian.PutUint32(b[20:], 0xffffffff) }},
		{"CTB offset", "careng.ctb", func(b []byte) { binary.LittleEndian.PutUint32(b[0x128:], 0xffffffff) }},
		{"LTB version", "careng.ltb", func(b []byte) { b[4] = 0 }},
		{"fedata string", "fedata.eng", func(b []byte) { binary.LittleEndian.PutUint32(b[0x3c0:], 0xffffffff) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			archive := cartest.ResourceArchive()
			data, ok := archive.Get(test.entry)
			if !ok {
				t.Fatal("missing fixture entry")
			}
			test.change(data)
			report, err := Check("car", archive, Options{})
			if err != nil || report.Valid {
				t.Fatalf("invalid resource accepted: report=%+v err=%v", report, err)
			}
			found := false
			for _, issue := range report.Issues {
				if issue.Entry == test.entry && issue.Severity == "error" {
					found = true
				}
			}
			if !found {
				t.Fatalf("report did not identify %s: %+v", test.entry, report)
			}
		})
	}
}

func TestOptionalTextureLimit(t *testing.T) {
	archive := cartest.ResourceArchive()
	tga := make([]byte, 18+512*4)
	tga[2], tga[16], tga[17] = 2, 32, 8
	binary.LittleEndian.PutUint16(tga[12:], 512)
	binary.LittleEndian.PutUint16(tga[14:], 1)
	archive.Set("car00.tga", tga)
	report, err := Check("car", archive, Options{})
	if err != nil || !report.Valid {
		t.Fatalf("default profile rejected larger textures: %+v %v", report, err)
	}
	report, err = Check("car", archive, Options{MaxTextureSize: 256})
	if err != nil || report.Valid || !strings.Contains(report.Issues[0].Message, "512x1") {
		t.Fatalf("texture limit not enforced: %+v %v", report, err)
	}
}

func TestCarRequiredEntriesAndDuplicates(t *testing.T) {
	archive := cartest.ResourceArchive()
	archive.Remove("car00.tga")
	archive.Entries = append(archive.Entries, viv.Entry{Name: "CAR.FCE", Data: archive.Entries[0].Data})
	report, err := Check("car", archive, Options{})
	if err != nil || report.Valid || len(report.Issues) != 2 {
		t.Fatalf("missing texture/duplicate not detected: %+v %v", report, err)
	}
}

func TestOriginalEmptyFedataAndTerminatedCarpVariants(t *testing.T) {
	archive := cartest.ResourceArchive()
	data, _ := archive.Get("fedata.eng")
	binary.LittleEndian.PutUint32(data[0x3c0:], 0x3c0+4*41-1)
	carp, _ := archive.Get("carp.txt")
	archive.Set("carp.txt", append(carp, 0))
	report, err := Check("car", archive, Options{})
	if err != nil || !report.Valid {
		t.Fatalf("original empty-string/CARP conventions rejected: %+v %v", report, err)
	}
}
