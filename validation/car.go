// Package validation implements named game-asset validation profiles.
package validation

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"path"
	"strings"

	"github.com/lnikonl/nfs4viv/fedata"
	"github.com/lnikonl/nfs4viv/resources"
	"github.com/lnikonl/nfs4viv/viv"
)

type Options struct {
	MaxTextureSize int
}

type Issue struct {
	Entry    string `json:"entry"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type Report struct {
	Profile string  `json:"profile"`
	Valid   bool    `json:"valid"`
	Checked int     `json:"checked"`
	Issues  []Issue `json:"issues"`
}

// Check runs a named profile. Optional passenger-car resources are checked
// when present, allowing the smaller original traffic and helicopter archives.
func Check(profile string, archive *viv.File, options Options) (Report, error) {
	report := Report{Profile: profile, Valid: true, Issues: []Issue{}}
	if profile != "car" {
		return report, fmt.Errorf("unknown validation profile %q (available: car)", profile)
	}
	if options.MaxTextureSize < 0 {
		return report, fmt.Errorf("max-texture-size cannot be negative")
	}
	add := func(entry, severity, message string) {
		report.Issues = append(report.Issues, Issue{Entry: entry, Severity: severity, Message: message})
		if severity == "error" {
			report.Valid = false
		}
	}
	names := map[string]bool{}
	for _, entry := range archive.Entries {
		name := strings.ToLower(entry.Name)
		if names[name] {
			add(entry.Name, "error", "duplicate entry name (case-insensitive)")
		}
		names[name] = true
		if strings.ContainsAny(name, `/\`) || name == "." || name == ".." || name == "" {
			add(entry.Name, "error", "CAR resources must have flat, nonempty entry names")
		}
		ext := strings.TrimPrefix(path.Ext(name), ".")
		var err error
		switch {
		case ext == "tga" || resources.Supported(ext):
			err = resources.Validate(ext, entry.Data, options.MaxTextureSize)
		case isFedata(name):
			err = checkFedata(entry.Data)
		case strings.HasPrefix(name, "carp") && ext == "txt":
			text := bytes.TrimRight(entry.Data, "\x00")
			if !bytes.HasPrefix(text, []byte("Serial Number(0)")) || bytes.IndexByte(text, 0) >= 0 {
				err = fmt.Errorf("expected NFS4 CARP parameter text beginning with Serial Number(0)")
			}
		default:
			add(entry.Name, "warning", "format is preserved but has no CAR structural validator")
			continue
		}
		report.Checked++
		if err != nil {
			add(entry.Name, "error", err.Error())
		}
	}
	if !names["car.fce"] && !names["hel.fce"] {
		add("car.fce", "error", "missing primary car.fce (or hel.fce for a helicopter)")
	}
	for _, pair := range [][2]string{
		{"car.fce", "car00.tga"}, {"car.fce", "carp.txt"}, {"hel.fce", "hel00.tga"},
		{"dash.fce", "dash00.tga"}, {"car1.fce", "car100.tga"}, {"car2.fce", "car200.tga"}, {"car3.fce", "car300.tga"},
	} {
		if names[pair[0]] && !names[pair[1]] {
			add(pair[1], "error", fmt.Sprintf("required by %s", pair[0]))
		}
	}
	return report, nil
}

func isFedata(name string) bool {
	for _, language := range []string{"eng", "bri", "fre", "ger", "ita", "spa", "swe"} {
		if name == "fedata."+language {
			return true
		}
	}
	return false
}

func checkFedata(data []byte) error {
	if _, err := fedata.Parse(data); err != nil {
		return err
	}
	count := int(binary.LittleEndian.Uint16(data[0x3be:]))
	if count != fedata.StringCount {
		return fmt.Errorf("NFS4 fedata needs %d strings; got %d", fedata.StringCount, count)
	}
	end := fedata.HeaderSize + 4*count
	for index := 0; index < count; index++ {
		offset := int(binary.LittleEndian.Uint32(data[fedata.HeaderSize+4*index:]))
		// Original ELNI/PELN and French PP91 reuse the final zero byte of the
		// offset table as the empty manufacturer string.
		if offset == end-1 && data[offset] == 0 {
			continue
		}
		if offset < end || offset >= len(data) || bytes.IndexByte(data[offset:], 0) < 0 {
			return fmt.Errorf("fedata string %d has an invalid offset or lacks a NUL terminator", index)
		}
	}
	return nil
}
