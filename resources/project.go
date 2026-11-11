// Package resources reads the FCE4, SHPI/FSH, ART, BNK4 and CRDl resources used
// by NFS4 cars. Directory projects retain opaque fields and physical layout.
package resources

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
)

const ManifestName = ".nfs4resource.json"

type section struct {
	File     string `json:"file"`
	Offset   int    `json:"offset"`
	Size     int    `json:"size"`
	Encoding string `json:"encoding"`
	Header   []byte `json:"header,omitempty"`
	Footer   []byte `json:"footer,omitempty"`
	Audio    *pcm   `json:"audio,omitempty"`
}

type fixup struct {
	Position int    `json:"position"`
	Width    int    `json:"width"`
	Endian   string `json:"endian"`
	Target   int    `json:"target"`
	Divisor  int    `json:"divisor,omitempty"` // Nonzero: size of target / divisor.
}

type project struct {
	Version    int          `json:"version"`
	Format     string       `json:"format"`
	Sections   []section    `json:"sections,omitempty"`
	Fixups     []fixup      `json:"fixups,omitempty"`
	Compressed bool         `json:"compressed,omitempty"`
	DecodedSHA string       `json:"decoded_sha256,omitempty"`
	FCE        *fceMetadata `json:"fce,omitempty"`
}

// Supported reports whether a resource has a directory codec.
func Supported(format string) bool {
	switch strings.ToLower(strings.TrimPrefix(format, ".")) {
	case "fce", "fsh", "art", "bnk", "ctb", "ltb":
		return true
	}
	return false
}

// Extract returns the files of an editable, lossless directory project.
func Extract(format string, data []byte) (map[string][]byte, error) {
	format = strings.ToLower(strings.TrimPrefix(format, "."))
	if format == "fce" {
		return extractFCEOBJ(data)
	}
	p := &project{Version: 1, Format: format}
	files := map[string][]byte{}
	if format == "fsh" && !bytes.HasPrefix(data, []byte("SHPI")) {
		decoded, err := decodeRefPack(data)
		if err != nil {
			return nil, err
		}
		p.Compressed = true
		p.DecodedSHA = digest(decoded)
		files["original.refpack"] = data
		data = decoded
	}
	if err := p.parse(data); err != nil {
		return nil, fmt.Errorf("%s: %w", format, err)
	}
	if err := p.cover(data); err != nil {
		return nil, fmt.Errorf("%s: %w", format, err)
	}
	for _, s := range p.Sections {
		encoded, err := exportSection(s, data[s.Offset:s.Offset+s.Size])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", s.File, err)
		}
		files[s.File] = encoded
	}
	encoded, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return nil, err
	}
	files[ManifestName] = append(encoded, '\n')
	return files, nil
}

func (p *project) parse(data []byte) error {
	switch p.Format {
	case "fce":
		return p.parseFCE(data)
	case "fsh":
		return p.parseFSH(data)
	case "art":
		return p.parseART(data)
	case "bnk":
		return p.parseBNK(data)
	case "ctb", "ltb":
		return p.parseCRD(data)
	default:
		return fmt.Errorf("unsupported resource format %q", p.Format)
	}
}

func (p *project) cover(data []byte) error {
	sort.Slice(p.Sections, func(i, j int) bool { return p.Sections[i].Offset < p.Sections[j].Offset })
	var sections []section
	position := 0
	for _, s := range p.Sections {
		if s.Offset < position || s.Size < 0 || s.Offset > len(data) || s.Size > len(data)-s.Offset {
			return fmt.Errorf("invalid or overlapping section %s", s.File)
		}
		if s.Offset > position {
			name := fmt.Sprintf("opaque_%08x.bin", position)
			if position == 0 {
				name = "header.bin"
			}
			sections = append(sections, section{File: name, Offset: position, Size: s.Offset - position, Encoding: "raw"})
		}
		sections = append(sections, s)
		position = s.Offset + s.Size
	}
	if position < len(data) {
		sections = append(sections, section{File: fmt.Sprintf("opaque_%08x.bin", position), Offset: position, Size: len(data) - position, Encoding: "raw"})
	}
	p.Sections = sections
	return nil
}

// Pack rebuilds a resource from the files returned by Extract. Missing and
// unreferenced files are errors, so edits cannot disappear silently.
func Pack(format string, files map[string][]byte) ([]byte, error) {
	var p project
	if err := json.Unmarshal(files[ManifestName], &p); err != nil {
		return nil, fmt.Errorf("%s: %w", ManifestName, err)
	}
	if p.Version == 2 && p.Format == "fce" && format == "fce" {
		return packFCEOBJ(files)
	}
	if p.Version != 1 || p.Format != format || !Supported(format) || p.Compressed && format != "fsh" {
		return nil, fmt.Errorf("unsupported resource project version/format %d/%q (expected %s)", p.Version, p.Format, format)
	}
	used := map[string]bool{ManifestName: true}
	positions := map[int]int{}
	lengths := map[int]int{}
	var out []byte
	oldPosition := 0
	resized := false
	for _, s := range p.Sections {
		if !validProjectName(s.File) || used[s.File] || s.Offset != oldPosition || s.Size < 0 {
			return nil, fmt.Errorf("invalid project section %q", s.File)
		}
		input, ok := files[s.File]
		if !ok {
			return nil, fmt.Errorf("missing project file %s", s.File)
		}
		used[s.File] = true
		data, err := importSection(s, input)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", s.File, err)
		}
		if len(data) != s.Size {
			if s.Encoding != "fsh-image" && s.Encoding != "art-image" && s.Encoding != "wav" {
				return nil, fmt.Errorf("%s: changing section size is not supported (%d -> %d)", s.File, s.Size, len(data))
			}
			resized = true
		}
		if p.Format == "bnk" && strings.HasPrefix(s.File, "sample_") && s.Offset%4 == 0 {
			for len(out)%4 != 0 {
				out = append(out, 0)
			}
		}
		positions[s.Offset] = len(out)
		lengths[s.Offset] = len(data)
		out = append(out, data...)
		oldPosition += s.Size
	}
	positions[oldPosition] = len(out)
	if resized {
		for _, f := range p.Fixups {
			value, ok := positions[f.Target]
			if f.Divisor != 0 {
				length, found := lengths[f.Target]
				if !found || f.Divisor < 1 || length%f.Divisor != 0 {
					return nil, fmt.Errorf("invalid sample size fixup")
				}
				value, ok = length/f.Divisor, true
			}
			if !ok || f.Position < 0 || f.Width < 1 || f.Width > 4 || f.Position > len(out)-f.Width || uint64(value) >= uint64(1)<<(8*f.Width) {
				return nil, fmt.Errorf("resource fixup at 0x%x cannot be represented", f.Position)
			}
			for i := 0; i < f.Width; i++ {
				index := i
				if f.Endian == "big" {
					index = f.Width - 1 - i
				}
				out[f.Position+index] = byte(value >> (8 * i))
			}
		}
		if p.Format == "fsh" {
			if len(out) < 16 {
				return nil, fmt.Errorf("rebuilt SHPI header is truncated")
			}
			binary.LittleEndian.PutUint32(out[4:8], uint32(len(out)))
		}
		if p.Format == "bnk" {
			if len(out) < 20 {
				return nil, fmt.Errorf("rebuilt BNK header is truncated")
			}
			pool := int(binary.LittleEndian.Uint32(out[8:12]))
			if pool < 20 || pool > len(out) {
				return nil, fmt.Errorf("rebuilt BNK pool lies outside file")
			}
			binary.LittleEndian.PutUint32(out[12:16], uint32(len(out)-pool))
		}
	}
	if p.Compressed {
		used["original.refpack"] = true
	}
	for name := range files {
		if !used[name] {
			return nil, fmt.Errorf("unreferenced project file %s", name)
		}
	}
	if err := Validate(p.Format, out, 0); err != nil {
		return nil, fmt.Errorf("rebuilt %s: %w", p.Format, err)
	}
	if p.Compressed {
		original, err := decodeRefPack(files["original.refpack"])
		if err != nil {
			return nil, fmt.Errorf("original.refpack: %w", err)
		}
		if digest(original) != p.DecodedSHA {
			return nil, fmt.Errorf("original.refpack does not match project checksum")
		}
		if bytes.Equal(original, out) {
			return files["original.refpack"], nil
		}
		return encodeRefPack(out)
	}
	return out, nil
}

func validProjectName(name string) bool {
	return name != "" && name != "." && !strings.HasPrefix(name, "/") && !strings.ContainsAny(name, `\:`) && path.Clean(name) == name && name != ".." && !strings.HasPrefix(name, "../")
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Validate checks resource headers, tables, payload boundaries and references.
// A zero maxTextureSize imposes no dimension limit.
func Validate(format string, data []byte, maxTextureSize int) error {
	if format == "tga" {
		return validateCarTGA(data, maxTextureSize)
	}
	if format == "fsh" && !bytes.HasPrefix(data, []byte("SHPI")) {
		var err error
		data, err = decodeRefPack(data)
		if err != nil {
			return err
		}
	}
	p := &project{Format: format}
	if err := p.parse(data); err != nil {
		return err
	}
	if (format == "fsh" || format == "art") && maxTextureSize > 0 {
		for _, s := range p.Sections {
			var w, h int
			switch s.Encoding {
			case "fsh-image":
				w, h = int(binary.LittleEndian.Uint16(s.Header[4:])), int(binary.LittleEndian.Uint16(s.Header[6:]))
			case "art-image":
				width, height, ok := artDimensions(s)
				if !ok {
					continue
				}
				w, h = int(width), int(height)
			default:
				continue
			}
			if w > maxTextureSize || h > maxTextureSize {
				return fmt.Errorf("%s: texture %dx%d exceeds %dx%d", s.File, w, h, maxTextureSize, maxTextureSize)
			}
		}
	}
	return p.cover(data)
}

func marshalJSON(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	return append(data, '\n'), err
}
