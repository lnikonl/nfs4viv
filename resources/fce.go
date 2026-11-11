package resources

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
)

const fceHeaderSize = 0x2038

type triangle struct {
	Texture  uint32     `json:"texture"`
	Vertices [3]uint32  `json:"vertices"`
	Reserved [3]uint32  `json:"reserved"`
	Flags    uint32     `json:"flags"`
	U        [3]float32 `json:"u"`
	V        [3]float32 `json:"v"`
}

func (p *project) parseFCE(data []byte) error {
	if len(data) < fceHeaderSize {
		return fmt.Errorf("truncated FCE4 header (need 0x2038 bytes)")
	}
	u32 := func(off int) int { return int(binary.LittleEndian.Uint32(data[off:])) }
	if u32(0) != 0x101014 {
		return fmt.Errorf("unsupported FCE version 0x%x (expected NFS4 FCE4 0x101014)", u32(0))
	}
	vertices, triangles := u32(0xc), u32(8)
	if u32(0x58) > 16 || u32(0x11c) > 64 || u32(0x820) > 16 {
		return fmt.Errorf("FCE dummy, part or color count exceeds header capacity")
	}
	tables := []struct {
		position, count, stride int
		name, encoding          string
	}{
		{0x14, vertices, 12, "vertices.json", "vec3"},
		{0x18, vertices, 12, "normals.json", "vec3"},
		{0x1c, triangles, 56, "triangles.json", "triangles"},
		{0x20, vertices, 32, "reserved1.bin", "raw"},
		{0x24, vertices, 12, "reserved2.bin", "raw"},
		{0x28, vertices, 12, "reserved3.bin", "raw"},
		{0x34, vertices, 12, "damaged_vertices.json", "vec3"},
		{0x38, vertices, 12, "damaged_normals.json", "vec3"},
		{0x3c, vertices, 4, "reserved4.bin", "raw"},
		{0x40, vertices, 4, "animation.bin", "raw"},
		{0x44, vertices, 4, "reserved5.bin", "raw"},
		{0x48, triangles, 12, "reserved6.bin", "raw"},
	}
	for _, table := range tables {
		offset := uint64(fceHeaderSize) + uint64(u32(table.position))
		length := uint64(table.count) * uint64(table.stride)
		if offset > uint64(len(data)) || length > uint64(len(data))-offset {
			return fmt.Errorf("%s outside file", table.name)
		}
		if length == 0 {
			continue
		}
		if table.encoding == "vec3" && !finiteFloats(data[int(offset):int(offset+length)]) {
			return fmt.Errorf("%s contains non-finite coordinates", table.name)
		}
		p.Sections = append(p.Sections, section{File: table.name, Offset: int(offset), Size: int(length), Encoding: table.encoding})
	}
	// These header fields may alias other tables, so check them without
	// exporting potentially overlapping legacy sections a second time.
	for _, position := range []int{0x2c, 0x30} {
		offset := uint64(fceHeaderSize) + uint64(u32(position))
		length := uint64(vertices) * 12
		if offset > uint64(len(data)) || length > uint64(len(data))-offset || !finiteFloats(data[int(offset):int(offset+length)]) {
			return fmt.Errorf("invalid undamaged vector table at header offset 0x%x", position)
		}
	}
	if !finiteFloats(data[0x4c:0x58]) || !finiteFloats(data[0x5c:0x5c+12*u32(0x58)]) || !finiteFloats(data[0x120:0x120+12*u32(0x11c)]) {
		return fmt.Errorf("non-finite model, dummy or part coordinates")
	}
	base := fceHeaderSize + u32(0x1c)
	for index := 0; index < triangles; index++ {
		if !finiteFloats(data[base+56*index+32 : base+56*index+56]) {
			return fmt.Errorf("triangle %d has non-finite texture coordinates", index)
		}
	}
	for part := 0; part < u32(0x11c); part++ {
		firstVertex, countVertex := u32(0x420+4*part), u32(0x520+4*part)
		firstTriangle, countTriangle := u32(0x620+4*part), u32(0x720+4*part)
		if firstVertex > vertices || countVertex > vertices-firstVertex || firstTriangle > triangles || countTriangle > triangles-firstTriangle {
			return fmt.Errorf("part %d outside geometry tables", part)
		}
		for index := firstTriangle; index < firstTriangle+countTriangle; index++ {
			for corner := 0; corner < 3; corner++ {
				if u32(base+56*index+4+4*corner) >= countVertex {
					return fmt.Errorf("part %d triangle %d has an invalid vertex index", part, index)
				}
			}
		}
	}
	return nil
}

func finiteFloats(data []byte) bool {
	for i := 0; i < len(data); i += 4 {
		value := math.Float32frombits(binary.LittleEndian.Uint32(data[i:]))
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return false
		}
	}
	return true
}

func exportSection(s section, data []byte) ([]byte, error) {
	switch s.Encoding {
	case "raw":
		return data, nil
	case "vec3":
		vectors := make([][3]float32, len(data)/12)
		for index := range vectors {
			for axis := range vectors[index] {
				vectors[index][axis] = math.Float32frombits(binary.LittleEndian.Uint32(data[12*index+4*axis:]))
			}
		}
		return marshalJSON(vectors)
	case "triangles":
		triangles := make([]triangle, len(data)/56)
		for index := range triangles {
			b := data[56*index:]
			t := &triangles[index]
			t.Texture = binary.LittleEndian.Uint32(b)
			t.Flags = binary.LittleEndian.Uint32(b[28:])
			for corner := 0; corner < 3; corner++ {
				t.Vertices[corner] = binary.LittleEndian.Uint32(b[4+4*corner:])
				t.Reserved[corner] = binary.LittleEndian.Uint32(b[16+4*corner:])
				t.U[corner] = math.Float32frombits(binary.LittleEndian.Uint32(b[32+4*corner:]))
				t.V[corner] = math.Float32frombits(binary.LittleEndian.Uint32(b[44+4*corner:]))
			}
		}
		return marshalJSON(triangles)
	case "curve":
		values := make([]int, len(data))
		for i, value := range data {
			values[i] = int(value)
		}
		return marshalJSON(values)
	case "fsh-image":
		return exportFSHImage(s, data)
	case "art-image":
		return exportARTImage(s, data)
	case "wav":
		return encodeWAV(*s.Audio, data), nil
	default:
		return nil, fmt.Errorf("unknown section encoding %q", s.Encoding)
	}
}

func importSection(s section, data []byte) ([]byte, error) {
	switch s.Encoding {
	case "raw":
		return data, nil
	case "vec3":
		var vectors [][3]float32
		if err := json.Unmarshal(data, &vectors); err != nil {
			return nil, err
		}
		out := make([]byte, 12*len(vectors))
		for index, vector := range vectors {
			for axis, value := range vector {
				binary.LittleEndian.PutUint32(out[12*index+4*axis:], math.Float32bits(value))
			}
		}
		return out, nil
	case "triangles":
		var triangles []triangle
		if err := json.Unmarshal(data, &triangles); err != nil {
			return nil, err
		}
		return encodeFCETriangles(triangles), nil
	case "curve":
		var values []int
		if err := json.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		out := make([]byte, len(values))
		for index, value := range values {
			if value < 0 || value > 255 {
				return nil, fmt.Errorf("curve value %d at %d is outside 0..255", value, index)
			}
			out[index] = byte(value)
		}
		return out, nil
	case "fsh-image":
		return importFSHImage(s, data)
	case "art-image":
		return importARTImage(data)
	case "wav":
		if s.Audio == nil {
			return nil, fmt.Errorf("missing PCM metadata")
		}
		info, samples, err := decodeWAV(data)
		if err != nil {
			return nil, err
		}
		if info != *s.Audio {
			return nil, fmt.Errorf("WAV format must remain %d Hz, %d channel(s), %d-bit PCM", s.Audio.Rate, s.Audio.Channels, s.Audio.Bits)
		}
		return samples, nil
	default:
		return nil, fmt.Errorf("unknown section encoding %q", s.Encoding)
	}
}

func encodeFCETriangles(triangles []triangle) []byte {
	out := make([]byte, 56*len(triangles))
	for index, t := range triangles {
		b := out[56*index:]
		binary.LittleEndian.PutUint32(b, t.Texture)
		binary.LittleEndian.PutUint32(b[28:], t.Flags)
		for corner := 0; corner < 3; corner++ {
			binary.LittleEndian.PutUint32(b[4+4*corner:], t.Vertices[corner])
			binary.LittleEndian.PutUint32(b[16+4*corner:], t.Reserved[corner])
			binary.LittleEndian.PutUint32(b[32+4*corner:], math.Float32bits(t.U[corner]))
			binary.LittleEndian.PutUint32(b[44+4*corner:], math.Float32bits(t.V[corner]))
		}
	}
	return out
}
