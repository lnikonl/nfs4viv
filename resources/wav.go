package resources

import (
	"encoding/binary"
	"fmt"
)

type pcm struct {
	Channels int `json:"channels"`
	Bits     int `json:"bits"`
	Rate     int `json:"sample_rate"`
}

func encodeWAV(info pcm, samples []byte) []byte {
	out := make([]byte, 44, 44+len(samples)+1)
	copy(out, "RIFF")
	copy(out[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(out[16:], 16)
	binary.LittleEndian.PutUint16(out[20:], 1)
	binary.LittleEndian.PutUint16(out[22:], uint16(info.Channels))
	binary.LittleEndian.PutUint32(out[24:], uint32(info.Rate))
	stride := info.Channels * info.Bits / 8
	binary.LittleEndian.PutUint32(out[28:], uint32(stride*info.Rate))
	binary.LittleEndian.PutUint16(out[32:], uint16(stride))
	binary.LittleEndian.PutUint16(out[34:], uint16(info.Bits))
	copy(out[36:], "data")
	binary.LittleEndian.PutUint32(out[40:], uint32(len(samples)))
	out = append(out, samples...)
	if len(samples)&1 != 0 {
		out = append(out, 0)
	}
	binary.LittleEndian.PutUint32(out[4:], uint32(len(out)-8))
	return out
}

func decodeWAV(data []byte) (pcm, []byte, error) {
	var info pcm
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return info, nil, fmt.Errorf("expected PCM RIFF/WAVE")
	}
	end := uint64(binary.LittleEndian.Uint32(data[4:])) + 8
	if end > uint64(len(data)) || end < 12 {
		return info, nil, fmt.Errorf("truncated RIFF/WAVE")
	}
	var samples []byte
	foundFormat := false
	for position := 12; position <= int(end)-8; {
		length := int(binary.LittleEndian.Uint32(data[position+4:]))
		body := position + 8
		if length > int(end)-body {
			return info, nil, fmt.Errorf("truncated WAV chunk")
		}
		switch string(data[position : position+4]) {
		case "fmt ":
			if length < 16 || binary.LittleEndian.Uint16(data[body:]) != 1 {
				return info, nil, fmt.Errorf("WAV must use integer PCM")
			}
			info = pcm{Channels: int(binary.LittleEndian.Uint16(data[body+2:])), Bits: int(binary.LittleEndian.Uint16(data[body+14:])), Rate: int(binary.LittleEndian.Uint32(data[body+4:]))}
			if info.Channels < 1 || info.Channels > 2 || info.Bits != 8 && info.Bits != 16 || info.Rate < 1 || info.Rate > 384000 {
				return info, nil, fmt.Errorf("unsupported PCM WAV format")
			}
			stride := info.Channels * info.Bits / 8
			if int(binary.LittleEndian.Uint16(data[body+12:])) != stride || int(binary.LittleEndian.Uint32(data[body+8:])) != stride*info.Rate {
				return info, nil, fmt.Errorf("invalid PCM WAV byte rate/block alignment")
			}
			foundFormat = true
		case "data":
			if samples != nil {
				return info, nil, fmt.Errorf("multiple WAV data chunks are unsupported")
			}
			samples = data[body : body+length]
		}
		position = body + length + length%2
	}
	if !foundFormat || samples == nil || len(samples)%(info.Channels*info.Bits/8) != 0 {
		return info, nil, fmt.Errorf("missing WAV format/data or incomplete PCM frame")
	}
	return info, samples, nil
}
