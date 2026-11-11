package resources

import (
	"encoding/binary"
	"fmt"
	"sort"
)

type ptValue struct {
	value, position, width int
}

type audioStream struct {
	name   string
	fields map[byte]ptValue
}

func defaultPT() map[byte]ptValue {
	fields := map[byte]ptValue{}
	for key, value := range map[byte]int{0x82: 1, 0x83: 0, 0x84: 22050, 0x85: 0, 0x86: 0, 0x87: 0, 0x88: 0, 0x92: 2} {
		fields[key] = ptValue{value: value, position: -1}
	}
	return fields
}

func parsePT(data []byte, start, end int, name string) ([]audioStream, error) {
	if start < 0 || start > end-4 || string(data[start:start+4]) != "PT\x00\x00" {
		return nil, fmt.Errorf("%s: expected PT audio header", name)
	}
	streams := []audioStream{{name: name, fields: defaultPT()}}
	audio := false
	position := start + 4
	for position < end {
		key := data[position]
		position++
		switch key {
		case 0xff:
			return streams, nil
		case 0xfd:
			audio = true
			continue
		case 0xfe:
			if len(streams) >= 16 {
				return nil, fmt.Errorf("too many alternate audio streams")
			}
			streams = append(streams, audioStream{name: fmt.Sprintf("%s_alt%d", name, len(streams)), fields: defaultPT()})
			audio = false
			continue
		case 0xfc:
			return nil, fmt.Errorf("unsupported PT command 0xfc")
		}
		if position >= end {
			return nil, fmt.Errorf("truncated PT property")
		}
		width := int(data[position])
		position++
		if width > 4 || width > end-position {
			return nil, fmt.Errorf("invalid PT property length %d", width)
		}
		value := 0
		for _, b := range data[position : position+width] {
			value = value<<8 | int(b)
		}
		if audio {
			streams[len(streams)-1].fields[key] = ptValue{value: value, position: position, width: width}
			if key == 0x8a {
				audio = false
			}
		}
		position += width
	}
	return nil, fmt.Errorf("unterminated PT header")
}

func (p *project) parseBNK(data []byte) error {
	if len(data) < 20 || string(data[:4]) != "BNKl" || binary.LittleEndian.Uint16(data[4:]) != 4 {
		return fmt.Errorf("expected little-endian BNKl version 4")
	}
	count := int(binary.LittleEndian.Uint16(data[6:]))
	pool := int(binary.LittleEndian.Uint32(data[8:]))
	poolSize := int(binary.LittleEndian.Uint32(data[12:]))
	if count > (len(data)-20)/4 || pool < 20+4*count || pool > len(data) || poolSize > len(data)-pool {
		return fmt.Errorf("BNK directory or data pool outside file")
	}
	var streams []audioStream
	for index := 0; index < count; index++ {
		position := 20 + 4*index
		relative := int(binary.LittleEndian.Uint32(data[position:]))
		if relative == 0 {
			continue
		}
		start := position + relative
		if start < 20+4*count || start >= pool {
			return fmt.Errorf("BNK stream %d header points outside header pool", index)
		}
		parsed, err := parsePT(data, start, pool, fmt.Sprintf("sample_%03d", index))
		if err != nil {
			return err
		}
		streams = append(streams, parsed...)
	}
	offsets := []int{len(data)}
	for _, stream := range streams {
		off := stream.fields[0x88].value
		if off == 0 && stream.fields[0x85].value == 0 {
			continue
		}
		if off < pool || off > len(data) {
			return fmt.Errorf("%s: audio data points outside pool", stream.name)
		}
		offsets = append(offsets, off)
	}
	sort.Ints(offsets)
	seen := map[int]section{}
	for _, stream := range streams {
		fields := stream.fields
		offset, samples := fields[0x88].value, fields[0x85].value
		if offset == 0 && samples == 0 {
			continue
		}
		channels, bytesPerSample, rate, codec := fields[0x82].value, fields[0x92].value, fields[0x84].value, fields[0x83].value
		if channels < 1 || channels > 2 || bytesPerSample < 1 || bytesPerSample > 2 || rate < 1 || rate > 384000 {
			return fmt.Errorf("%s: unsupported channel count, sample depth or rate", stream.name)
		}
		if fields[0x86].value > samples || fields[0x87].value > samples {
			return fmt.Errorf("%s: loop lies outside audio samples", stream.name)
		}
		s := section{File: stream.name + ".bin", Offset: offset, Encoding: "raw"}
		next := sort.SearchInts(offsets, offset+1)
		if next == len(offsets) {
			s.Size = 0
		} else {
			s.Size = offsets[next] - offset
		}
		switch codec {
		case 0:
			length := uint64(samples) * uint64(channels) * uint64(bytesPerSample)
			if length > uint64(len(data)-offset) || int(length) > s.Size {
				return fmt.Errorf("%s: truncated PCM samples", stream.name)
			}
			s.Size, s.Encoding, s.File = int(length), "wav", stream.name+".wav"
			s.Audio = &pcm{Channels: channels, Bits: bytesPerSample * 8, Rate: rate}
		case 7:
			if samples > 0 && s.Size == 0 {
				return fmt.Errorf("%s: empty EA ADPCM payload", stream.name)
			}
		default:
			return fmt.Errorf("%s: unsupported BNK audio codec 0x%x", stream.name, codec)
		}
		if previous, ok := seen[offset]; ok {
			if previous.Size != s.Size || previous.Encoding != s.Encoding || previous.Audio != nil && *previous.Audio != *s.Audio {
				return fmt.Errorf("aliased BNK streams have incompatible formats")
			}
		} else if s.Size > 0 {
			seen[offset] = s
			p.Sections = append(p.Sections, s)
		}
		off := fields[0x88]
		p.Fixups = append(p.Fixups, fixup{Position: off.position, Width: off.width, Endian: "big", Target: offset})
		if codec == 0 && samples > 0 {
			n := fields[0x85]
			p.Fixups = append(p.Fixups, fixup{Position: n.position, Width: n.width, Endian: "big", Target: offset, Divisor: channels * bytesPerSample})
			if end := fields[0x87]; end.value == samples && end.width > 0 {
				p.Fixups = append(p.Fixups, fixup{Position: end.position, Width: end.width, Endian: "big", Target: offset, Divisor: channels * bytesPerSample})
			}
		}
	}
	return nil
}
