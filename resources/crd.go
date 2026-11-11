package resources

import (
	"encoding/binary"
	"fmt"
)

func (p *project) parseCRD(data []byte) error {
	if len(data) < 0x168 || string(data[:4]) != "CRDl" {
		return fmt.Errorf("expected CRDl engine curve table with a 0x168-byte header")
	}
	if binary.LittleEndian.Uint16(data[4:6]) != 2 {
		return fmt.Errorf("unsupported CRDl version (expected 2)")
	}
	seen := map[int]bool{}
	for index := 0; index < 16; index++ {
		position := 0x128 + 4*index
		relative := int(binary.LittleEndian.Uint32(data[position:]))
		if relative == 0 {
			continue
		}
		offset := position + relative
		if offset < 0x168 || offset > len(data)-512 {
			return fmt.Errorf("curve %d points outside file", index)
		}
		if !seen[offset] {
			p.Sections = append(p.Sections, section{File: fmt.Sprintf("curve_%02d.json", index), Offset: offset, Size: 512, Encoding: "curve"})
			seen[offset] = true
		}
	}
	if len(seen) == 0 {
		return fmt.Errorf("CRDl has no engine curves")
	}
	return nil
}
