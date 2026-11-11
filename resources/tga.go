package resources

import (
	"encoding/binary"
	"fmt"
)

type tgaImage struct {
	Width, Height int
	Pixels        []byte // BGRA, top-left origin; or 8-bit indices when indexed.
	Palette       []byte // BGRA entries.
}

func validateCarTGA(data []byte, limit int) error {
	if len(data) < 18 {
		return fmt.Errorf("truncated TGA header")
	}
	if data[1] != 0 || data[2] != 2 || data[16] != 32 {
		return fmt.Errorf("CAR texture must be uncompressed 32-bit true-color TGA (type 2, no color map); got type %d, %d-bit, color map %d", data[2], data[16], data[1])
	}
	w, h := int(binary.LittleEndian.Uint16(data[12:])), int(binary.LittleEndian.Uint16(data[14:]))
	if w == 0 || h == 0 {
		return fmt.Errorf("TGA dimensions must be nonzero")
	}
	if data[17]&0xc0 != 0 || data[17]&15 != 0 && data[17]&15 != 8 {
		return fmt.Errorf("unsupported TGA interleave/attribute descriptor 0x%02x", data[17])
	}
	start := 18 + int(data[0])
	if start > len(data) || uint64(w)*uint64(h)*4 > uint64(len(data)-start) {
		return fmt.Errorf("truncated TGA pixel data for %dx%d", w, h)
	}
	if limit > 0 && (w > limit || h > limit) {
		return fmt.Errorf("texture %dx%d exceeds %dx%d", w, h, limit, limit)
	}
	return nil
}

func encodeTGA(im tgaImage) []byte {
	header := make([]byte, 18)
	header[2], header[16], header[17] = 2, 32, 0x28
	if im.Palette != nil {
		header[1], header[2], header[7], header[16], header[17] = 1, 1, 32, 8, 0x20
		binary.LittleEndian.PutUint16(header[5:], uint16(len(im.Palette)/4))
	}
	binary.LittleEndian.PutUint16(header[12:], uint16(im.Width))
	binary.LittleEndian.PutUint16(header[14:], uint16(im.Height))
	out := append(header, im.Palette...)
	return append(out, im.Pixels...)
}

func decodeTGA(data []byte) (tgaImage, error) {
	var im tgaImage
	if len(data) < 18 {
		return im, fmt.Errorf("truncated TGA header")
	}
	im.Width = int(binary.LittleEndian.Uint16(data[12:]))
	im.Height = int(binary.LittleEndian.Uint16(data[14:]))
	if im.Width == 0 || im.Height == 0 || data[17]&0xc0 != 0 {
		return im, fmt.Errorf("invalid TGA dimensions/interleave")
	}
	typeID, bits := data[2], int(data[16])
	indexed := typeID == 1 || typeID == 9
	if !indexed && typeID != 2 && typeID != 10 || indexed && (bits != 8 || data[1] != 1) || !indexed && (data[1] != 0 || bits != 24 && bits != 32) {
		return im, fmt.Errorf("expected 8-bit indexed or 24/32-bit true-color TGA")
	}
	position := 18 + int(data[0])
	if position > len(data) {
		return im, fmt.Errorf("truncated TGA image ID")
	}
	if indexed {
		first, count := int(binary.LittleEndian.Uint16(data[3:])), int(binary.LittleEndian.Uint16(data[5:]))
		stride := (int(data[7]) + 7) / 8
		if first != 0 || count < 1 || count > 256 || stride != 3 && stride != 4 || count*stride > len(data)-position {
			return im, fmt.Errorf("indexed TGA needs a zero-based 24/32-bit palette of at most 256 colors")
		}
		im.Palette = make([]byte, count*4)
		for i := 0; i < count; i++ {
			copy(im.Palette[4*i:4*i+3], data[position+stride*i:position+stride*i+3])
			im.Palette[4*i+3] = 255
			if stride == 4 {
				im.Palette[4*i+3] = data[position+stride*i+3]
			}
		}
		position += count * stride
	}
	stride, outStride := bits/8, 4
	if indexed {
		outStride = 1
	}
	count := uint64(im.Width) * uint64(im.Height)
	// Bound allocation by both the encoded input's expansion potential and a
	// working-buffer limit, rather than imposing stock-game texture dimensions.
	if count*uint64(outStride) > 512<<20 || typeID < 8 && count*uint64(stride) > uint64(len(data)-position) {
		return im, fmt.Errorf("TGA pixels are truncated or exceed the 512 MiB working-buffer limit")
	}
	im.Pixels = make([]byte, int(count)*outStride)
	index := 0
	for index < int(count) {
		n, repeat := 1, false
		if typeID >= 8 {
			if position >= len(data) {
				return im, fmt.Errorf("truncated TGA RLE packet")
			}
			n, repeat = int(data[position]&127)+1, data[position]&128 != 0
			position++
		}
		if n > int(count)-index {
			return im, fmt.Errorf("TGA RLE packet overruns image")
		}
		var pixel []byte
		for i := 0; i < n; i++ {
			if i == 0 || !repeat {
				if stride > len(data)-position {
					return im, fmt.Errorf("truncated TGA pixels")
				}
				pixel = data[position : position+stride]
				position += stride
			}
			x, y := index%im.Width, index/im.Width
			if data[17]&0x10 != 0 {
				x = im.Width - 1 - x
			}
			if data[17]&0x20 == 0 {
				y = im.Height - 1 - y
			}
			dest := (y*im.Width + x) * outStride
			if indexed {
				if int(pixel[0]) >= len(im.Palette)/4 {
					return im, fmt.Errorf("TGA index outside palette")
				}
				im.Pixels[dest] = pixel[0]
			} else {
				copy(im.Pixels[dest:dest+3], pixel[:3])
				im.Pixels[dest+3] = 255
				if stride == 4 {
					im.Pixels[dest+3] = pixel[3]
				}
			}
			index++
		}
	}
	return im, nil
}
