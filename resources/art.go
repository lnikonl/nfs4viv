package resources

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/draw"
	"image/png"
)

const artWorkingBufferLimit = 512 << 20

func (p *project) parseART(data []byte) error {
	if len(data) < 8 {
		return fmt.Errorf("truncated ART texture header")
	}
	offset := 0
	for index := 0; offset < len(data); index++ {
		if len(data)-offset < 8 {
			return fmt.Errorf("ART image %d: truncated header", index)
		}
		header := data[offset : offset+8]
		width := binary.LittleEndian.Uint32(header)
		height := binary.LittleEndian.Uint32(header[4:])
		if width == 0 || height == 0 {
			return fmt.Errorf("ART image %d: dimensions must be nonzero", index)
		}
		count := uint64(width) * uint64(height)
		if count > uint64(len(data)-offset-8)/4 {
			return fmt.Errorf("ART image %d: %dx%d pixels are truncated", index, width, height)
		}
		size := 8 + int(count)*4
		p.Sections = append(p.Sections, section{
			File: fmt.Sprintf("image_%03d.png", index), Offset: offset, Size: size,
			Encoding: "art-image", Header: append([]byte(nil), header...),
		})
		offset += size
	}
	return nil
}

func exportARTImage(s section, data []byte) ([]byte, error) {
	width, height, ok := artDimensions(s)
	if !ok {
		return nil, fmt.Errorf("invalid ART image metadata")
	}
	if len(data) < 8 {
		return nil, fmt.Errorf("truncated ART image")
	}
	count := uint64(width) * uint64(height)
	if count > uint64(len(data)-8)/4 {
		return nil, fmt.Errorf("truncated ART pixels")
	}
	im := image.NewNRGBA(image.Rect(0, 0, int(width), int(height)))
	for y := 0; y < int(height); y++ {
		sourceRow := int(height) - 1 - y
		for x := 0; x < int(width); x++ {
			source := data[8+4*(sourceRow*int(width)+x):]
			target := im.Pix[4*(y*int(width)+x):]
			target[0], target[1], target[2], target[3] = source[2], source[1], source[0], source[3]
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, im); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func importARTImage(data []byte) ([]byte, error) {
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("expected a PNG image: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 {
		return nil, fmt.Errorf("PNG dimensions must be nonzero")
	}
	count := uint64(config.Width) * uint64(config.Height)
	if count > artWorkingBufferLimit/4 {
		return nil, fmt.Errorf("PNG pixels exceed the %d MiB working-buffer limit", artWorkingBufferLimit>>20)
	}
	source, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	im := image.NewNRGBA(image.Rect(0, 0, config.Width, config.Height))
	draw.Draw(im, im.Bounds(), source, image.Point{}, draw.Src)
	out := make([]byte, 8+int(count)*4)
	binary.LittleEndian.PutUint32(out, uint32(config.Width))
	binary.LittleEndian.PutUint32(out[4:], uint32(config.Height))
	for y := 0; y < config.Height; y++ {
		targetRow := config.Height - 1 - y
		for x := 0; x < config.Width; x++ {
			source := im.Pix[4*(y*config.Width+x):]
			target := out[8+4*(targetRow*config.Width+x):]
			target[0], target[1], target[2], target[3] = source[2], source[1], source[0], source[3]
		}
	}
	return out, nil
}

func artDimensions(s section) (uint32, uint32, bool) {
	if len(s.Header) != 8 {
		return 0, 0, false
	}
	return binary.LittleEndian.Uint32(s.Header), binary.LittleEndian.Uint32(s.Header[4:]), true
}
