package resources

import "fmt"

func decodeRefPack(data []byte) ([]byte, error) {
	if len(data) < 5 || data[0]&0xfe != 0x10 || data[1] != 0xfb {
		return nil, fmt.Errorf("expected SHPI or EA RefPack stream")
	}
	position := 2
	if data[0]&1 != 0 {
		position += 3 // Optional compressed size.
	}
	if len(data)-position < 3 {
		return nil, fmt.Errorf("truncated RefPack header")
	}
	expected := int(data[position])<<16 | int(data[position+1])<<8 | int(data[position+2])
	position += 3
	var out []byte
	literal := func(length int) error {
		if length > len(data)-position || length > expected-len(out) {
			return fmt.Errorf("RefPack literal outside stream/output")
		}
		out = append(out, data[position:position+length]...)
		position += length
		return nil
	}
	backref := func(distance, length int) error {
		if distance < 1 || distance > len(out) || length > expected-len(out) {
			return fmt.Errorf("invalid RefPack back-reference")
		}
		for i := 0; i < length; i++ {
			out = append(out, out[len(out)-distance])
		}
		return nil
	}
	for position < len(data) {
		control := int(data[position])
		position++
		if control >= 0xfc {
			if err := literal(control & 3); err != nil {
				return nil, err
			}
			if len(out) != expected {
				return nil, fmt.Errorf("RefPack length %d does not match header %d", len(out), expected)
			}
			return out, nil
		}
		if control >= 0xe0 {
			if err := literal((control&31)*4 + 4); err != nil {
				return nil, err
			}
			continue
		}
		extra := 1
		if control >= 0x80 {
			extra = 2
		}
		if control >= 0xc0 {
			extra = 3
		}
		if extra > len(data)-position {
			return nil, fmt.Errorf("truncated RefPack command")
		}
		a := int(data[position])
		position += extra
		lit, distance, length := control&3, ((control>>5)<<8|a)+1, ((control&0x1c)>>2)+3
		if extra == 2 {
			b := int(data[position-1])
			lit, distance, length = a>>6, ((a&63)<<8|b)+1, (control&63)+4
		}
		if extra == 3 {
			b, c := int(data[position-2]), int(data[position-1])
			distance, length = ((control&0x10)<<12|a<<8|b)+1, ((control>>2&3)<<8|c)+5
		}
		if err := literal(lit); err != nil {
			return nil, err
		}
		if err := backref(distance, length); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("missing RefPack terminator")
}

// A literal-only RefPack encoder is deterministic and accepted by the game;
// unchanged compressed projects reuse their original stream byte for byte.
func encodeRefPack(data []byte) ([]byte, error) {
	if len(data) >= 1<<24 {
		return nil, fmt.Errorf("RefPack output exceeds its 24-bit length capacity")
	}
	out := []byte{0x10, 0xfb, byte(len(data) >> 16), byte(len(data) >> 8), byte(len(data))}
	for len(data) >= 4 {
		n := min(112, len(data)/4*4)
		out = append(out, byte(0xe0+n/4-1))
		out = append(out, data[:n]...)
		data = data[n:]
	}
	out = append(out, 0xfc+byte(len(data)))
	return append(out, data...), nil
}
