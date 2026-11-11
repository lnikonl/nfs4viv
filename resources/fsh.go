package resources

import (
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
)

func (p *project) parseFSH(data []byte) error {
	if len(data) < 16 || string(data[:4]) != "SHPI" {
		return fmt.Errorf("expected SHPI texture container")
	}
	declared, count := uint64(binary.LittleEndian.Uint32(data[4:])), uint64(binary.LittleEndian.Uint32(data[8:]))
	if declared < 16 || declared > uint64(len(data)) || count > (declared-16)/8 {
		return fmt.Errorf("invalid SHPI size/directory")
	}
	directoryEnd := 16 + 8*int(count)
	offsets := []int{len(data)}
	for index := 0; index < int(count); index++ {
		offset := int(binary.LittleEndian.Uint32(data[20+8*index:]))
		if offset < directoryEnd || offset > int(declared)-16 {
			return fmt.Errorf("SHPI image %d points outside data pool", index)
		}
		offsets = append(offsets, offset)
		p.Fixups = append(p.Fixups, fixup{Position: 20 + 8*index, Width: 4, Endian: "little", Target: offset})
	}
	sort.Ints(offsets)
	seen := map[int]bool{}
	for index := 0; index < int(count); index++ {
		offset := int(binary.LittleEndian.Uint32(data[20+8*index:]))
		if seen[offset] {
			continue
		}
		seen[offset] = true
		end := offsets[sort.SearchInts(offsets, offset+1)]
		if end < offset+16 {
			return fmt.Errorf("SHPI image %d overlaps the next image header", index)
		}
		h := data[offset : offset+16]
		w, height := int(binary.LittleEndian.Uint16(h[4:])), int(binary.LittleEndian.Uint16(h[6:]))
		stride := fshStride(h[0])
		if stride == 0 || w == 0 || height == 0 || uint64(w)*uint64(height)*uint64(stride) > uint64(end-offset-16) {
			return fmt.Errorf("SHPI image %d: unsupported format 0x%02x, invalid dimensions or truncated pixels", index, h[0])
		}
		pixelSize := w * height * stride
		footerOffset := int(h[1]) | int(h[2])<<8 | int(h[3])<<16
		if footerOffset != 0 && (footerOffset < 16+pixelSize || footerOffset > end-offset-16) {
			return fmt.Errorf("SHPI image %d: footer outside image", index)
		}
		name := strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
				return r
			}
			return '_'
		}, string(data[16+8*index:20+8*index]))
		s := section{
			File: fmt.Sprintf("%03d_%s.tga", index, name), Offset: offset, Size: end - offset, Encoding: "fsh-image",
			Header: append([]byte(nil), h...), Footer: append([]byte(nil), data[offset+16+pixelSize:end]...),
		}
		if h[0] == 0x7b {
			palette, _, stride, err := fshPalette(s)
			if err != nil {
				return fmt.Errorf("SHPI image %d: %w", index, err)
			}
			for _, value := range data[offset+16 : offset+16+pixelSize] {
				if int(value) >= len(palette)/stride {
					return fmt.Errorf("SHPI image %d: pixel index outside palette", index)
				}
			}
		}
		p.Sections = append(p.Sections, s)
	}
	return nil
}

func fshStride(kind byte) int {
	switch kind {
	case 0x7b:
		return 1
	case 0x6d, 0x78, 0x7e:
		return 2
	case 0x7f:
		return 3
	case 0x7d:
		return 4
	}
	return 0
}

func fshPalette(s section) ([]byte, int, int, error) {
	if len(s.Header) != 16 {
		return nil, 0, 0, fmt.Errorf("invalid FSH image header")
	}
	w, h := int(binary.LittleEndian.Uint16(s.Header[4:])), int(binary.LittleEndian.Uint16(s.Header[6:]))
	start := (int(s.Header[1]) | int(s.Header[2])<<8 | int(s.Header[3])<<16) - 16 - w*h
	if start < 0 || start > len(s.Footer)-16 {
		return nil, 0, 0, fmt.Errorf("indexed FSH needs a local palette")
	}
	stride := 0
	switch s.Footer[start] {
	case 0x2a:
		stride = 4
	case 0x22, 0x24:
		stride = 3
	case 0x2d:
		stride = 2
	}
	count := int(binary.LittleEndian.Uint16(s.Footer[start+4:]))
	if stride == 0 || count < 1 || count > 256 || count*stride > len(s.Footer)-start-16 {
		return nil, 0, 0, fmt.Errorf("invalid or unsupported FSH palette")
	}
	return s.Footer[start+16 : start+16+count*stride], start + 16, stride, nil
}

func exportFSHImage(s section, data []byte) ([]byte, error) {
	w, h := int(binary.LittleEndian.Uint16(s.Header[4:])), int(binary.LittleEndian.Uint16(s.Header[6:]))
	kind := s.Header[0]
	stride := fshStride(kind)
	im := tgaImage{Width: w, Height: h}
	if kind == 0x7b {
		palette, _, paletteStride, err := fshPalette(s)
		if err != nil {
			return nil, err
		}
		im.Pixels = data[16 : 16+w*h]
		im.Palette = make([]byte, len(palette)/paletteStride*4)
		for i := 0; i < len(palette)/paletteStride; i++ {
			decodePackedPixel(im.Palette[4*i:], palette[paletteStride*i:], paletteStride, 0x7e)
		}
	} else {
		im.Pixels = make([]byte, w*h*4)
		for i := 0; i < w*h; i++ {
			decodePackedPixel(im.Pixels[4*i:], data[16+stride*i:], stride, kind)
		}
	}
	return encodeTGA(im), nil
}

func importFSHImage(s section, data []byte) ([]byte, error) {
	if len(s.Header) != 16 || fshStride(s.Header[0]) == 0 {
		return nil, fmt.Errorf("invalid FSH image metadata")
	}
	im, err := decodeTGA(data)
	if err != nil {
		return nil, err
	}
	header := append([]byte(nil), s.Header...)
	footer := append([]byte(nil), s.Footer...)
	oldW, oldH := int(binary.LittleEndian.Uint16(header[4:])), int(binary.LittleEndian.Uint16(header[6:]))
	stride, kind := fshStride(header[0]), header[0]
	oldFooterOffset := int(header[1]) | int(header[2])<<8 | int(header[3])<<16
	if (im.Width != oldW || im.Height != oldH) && oldFooterOffset > 16+oldW*oldH*stride {
		return nil, fmt.Errorf("resizing an image with additional mip data is not supported")
	}
	pixels := make([]byte, im.Width*im.Height*stride)
	if kind == 0x7b {
		palette, start, paletteStride, err := fshPalette(s)
		if err != nil {
			return nil, err
		}
		if im.Palette == nil || len(im.Palette)/4 != len(palette)/paletteStride {
			return nil, fmt.Errorf("preserve indexed TGA format and its %d palette entries", len(palette)/paletteStride)
		}
		copy(pixels, im.Pixels)
		for i := 0; i < len(palette)/paletteStride; i++ {
			encodePackedPixel(footer[start+paletteStride*i:], im.Palette[4*i:], paletteStride, 0x7e)
		}
	} else {
		for i := 0; i < im.Width*im.Height; i++ {
			var pixel []byte
			if im.Palette != nil {
				pixel = im.Palette[int(im.Pixels[i])*4:]
			} else {
				pixel = im.Pixels[4*i:]
			}
			encodePackedPixel(pixels[stride*i:], pixel, stride, kind)
		}
	}
	binary.LittleEndian.PutUint16(header[4:], uint16(im.Width))
	binary.LittleEndian.PutUint16(header[6:], uint16(im.Height))
	if oldFooterOffset != 0 {
		offset := oldFooterOffset + len(pixels) - oldW*oldH*stride
		if offset >= 1<<24 {
			return nil, fmt.Errorf("FSH footer offset exceeds 24-bit format capacity")
		}
		header[1], header[2], header[3] = byte(offset), byte(offset>>8), byte(offset>>16)
	}
	out := append(header, pixels...)
	return append(out, footer...), nil
}

func decodePackedPixel(out, in []byte, stride int, kind byte) {
	if stride >= 3 {
		copy(out[:3], in[:3])
		out[3] = 255
		if stride == 4 {
			out[3] = in[3]
		}
		return
	}
	v := binary.LittleEndian.Uint16(in)
	switch kind {
	case 0x6d:
		out[0], out[1], out[2], out[3] = byte(v&15)*17, byte(v>>4&15)*17, byte(v>>8&15)*17, byte(v>>12)*17
	case 0x78:
		out[0], out[1], out[2], out[3] = expand5(v), byte(v>>5&63)<<2|byte(v>>5&63)>>4, expand5(v>>11), 255
	default:
		out[0], out[1], out[2], out[3] = expand5(v), expand5(v>>5), expand5(v>>10), byte(v>>15)*255
	}
}

func encodePackedPixel(out, in []byte, stride int, kind byte) {
	if stride >= 3 {
		copy(out[:stride], in[:stride])
		return
	}
	var v uint16
	switch kind {
	case 0x6d:
		v = uint16(in[0]>>4) | uint16(in[1]>>4)<<4 | uint16(in[2]>>4)<<8 | uint16(in[3]>>4)<<12
	case 0x78:
		v = uint16(in[0]>>3) | uint16(in[1]>>2)<<5 | uint16(in[2]>>3)<<11
	default:
		v = uint16(in[0]>>3) | uint16(in[1]>>3)<<5 | uint16(in[2]>>3)<<10 | uint16(in[3]>>7)<<15
	}
	binary.LittleEndian.PutUint16(out, v)
}

func expand5(v uint16) byte {
	v &= 31
	return byte(v<<3 | v>>2)
}
