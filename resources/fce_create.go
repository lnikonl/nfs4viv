package resources

import (
	"bytes"
	"fmt"
	"math"
	"strings"
)

// Metadata positions use native FCE/OBJ coordinates (Y up), not Blender's
// converted Z-up scene coordinates. The manifest is editable alongside OBJ.
type fceMetadata struct {
	Parts        []fcePartMetadata  `json:"parts"`
	Dummies      []fceDummyMetadata `json:"dummies"`
	HalfSize     *fceVec            `json:"half_size"`
	TexturePages int                `json:"texture_pages"`
	Colors       []fceColorMetadata `json:"colors,omitempty"`
}

type fceColorMetadata struct {
	Primary   [4]uint8 `json:"primary"`
	Interior  [4]uint8 `json:"interior"`
	Secondary [4]uint8 `json:"secondary"`
	Driver    [4]uint8 `json:"driver"`
}

type fcePartMetadata struct {
	Object     string `json:"object"`
	Name       string `json:"name"`
	Origin     fceVec `json:"origin"`
	SourcePart *int   `json:"source_part"`
}

type fceDummyMetadata struct {
	Name     string `json:"name"`
	Position fceVec `json:"position"`
}

func fceName(data []byte, offset int) string {
	return string(bytes.SplitN(data[offset:offset+64], []byte{0}, 2)[0])
}

func metadataFromFCE(data []byte) *fceMetadata {
	size := fceVector(data, 0x4c)
	cfg := &fceMetadata{HalfSize: &size, TexturePages: fceU32(data, 0x10),
		Parts: []fcePartMetadata{}, Dummies: []fceDummyMetadata{}}
	for i, p := range fceParts(data) {
		source := i
		cfg.Parts = append(cfg.Parts, fcePartMetadata{p.name, fceName(data, 0xe28+64*i), p.origin, &source})
	}
	for i := 0; i < fceU32(data, 0x58); i++ {
		cfg.Dummies = append(cfg.Dummies, fceDummyMetadata{fceName(data, 0xa28+64*i), fceVector(data, 0x5c+12*i)})
	}
	cfg.Colors = make([]fceColorMetadata, fceU32(data, 0x820))
	for i := range cfg.Colors {
		for table, dest := range []*[4]uint8{&cfg.Colors[i].Primary, &cfg.Colors[i].Interior, &cfg.Colors[i].Secondary, &cfg.Colors[i].Driver} {
			copy(dest[:], data[0x824+64*table+4*i:0x824+64*table+4*i+4])
		}
	}
	return cfg
}

func finiteFCEVec(v fceVec) bool {
	for _, x := range v {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			return false
		}
	}
	return true
}

func validFCEName(name string) bool {
	if len(name) > 63 {
		return false
	}
	for _, b := range []byte(name) {
		if b < 32 || b > 126 {
			return false
		}
	}
	return true
}

func (cfg *fceMetadata) parts(original []byte) ([]fcePart, error) {
	if len(cfg.Parts) > 64 || len(cfg.Dummies) > 16 || len(cfg.Colors) > 16 {
		return nil, fmt.Errorf("FCE supports at most 64 parts, 16 dummies and 16 colors")
	}
	if cfg.TexturePages < 0 || uint64(cfg.TexturePages) > math.MaxUint32 {
		return nil, fmt.Errorf("invalid texture_pages")
	}
	if cfg.HalfSize != nil && !finiteFCEVec(*cfg.HalfSize) {
		return nil, fmt.Errorf("half_size must be finite")
	}
	old := fceParts(original)
	parts := make([]fcePart, len(cfg.Parts))
	objects := map[string]bool{}
	for i, spec := range cfg.Parts {
		if !validFCEName(spec.Name) {
			return nil, fmt.Errorf("part %d name must be at most 63 printable ASCII bytes", i)
		}
		if spec.Object == "" || len(strings.Fields(spec.Object)) != 1 || strings.ContainsAny(spec.Object, "#\x00\r\n") || strings.TrimSpace(spec.Object) != spec.Object || objects[spec.Object] {
			return nil, fmt.Errorf("part %d needs a unique, single-token object name", i)
		}
		objects[spec.Object] = true
		if !finiteFCEVec(spec.Origin) {
			return nil, fmt.Errorf("part %d origin must be finite", i)
		}
		if spec.SourcePart != nil {
			index := *spec.SourcePart
			if index < 0 || index >= len(old) {
				return nil, fmt.Errorf("part %d source_part %d outside original.fce", i, index)
			}
			parts[i] = old[index]
		}
		parts[i].name, parts[i].origin = spec.Object, spec.Origin
	}
	for i, dummy := range cfg.Dummies {
		if !validFCEName(dummy.Name) || !finiteFCEVec(dummy.Position) {
			return nil, fmt.Errorf("invalid dummy %d name/position", i)
		}
	}
	return parts, nil
}

func writeFCEName(data []byte, offset int, name string) {
	if fceName(data, offset) == name {
		return
	} // Preserve original name padding.
	clear(data[offset : offset+64])
	copy(data[offset:offset+64], name)
}

func (cfg *fceMetadata) writeHeader(header []byte, meshes, damage []fceMesh) {
	oldParts, oldDummies := fceU32(header, 0x11c), fceU32(header, 0x58)
	fcePut(header, 0x11c, len(cfg.Parts))
	fcePut(header, 0x58, len(cfg.Dummies))
	fcePut(header, 0x10, cfg.TexturePages)
	if cfg.Colors != nil {
		oldColors := fceU32(header, 0x820)
		fcePut(header, 0x820, len(cfg.Colors))
		for i, color := range cfg.Colors {
			for table, value := range [][4]uint8{color.Primary, color.Interior, color.Secondary, color.Driver} {
				copy(header[0x824+64*table+4*i:], value[:])
			}
		}
		for i := len(cfg.Colors); i < oldColors; i++ {
			for table := 0; table < 4; table++ {
				clear(header[0x824+64*table+4*i : 0x824+64*table+4*i+4])
			}
		}
	}
	for i, p := range cfg.Parts {
		writeFCEName(header, 0xe28+64*i, p.Name)
		fceWriteVector(header, 0x120+12*i, p.Origin)
	}
	for i := len(cfg.Parts); i < oldParts; i++ {
		clear(header[0xe28+64*i : 0xe28+64*(i+1)])
		clear(header[0x120+12*i : 0x120+12*(i+1)])
		for _, off := range []int{0x420, 0x520, 0x620, 0x720} {
			fcePut(header, off+4*i, 0)
		}
	}
	for i, dummy := range cfg.Dummies {
		writeFCEName(header, 0xa28+64*i, dummy.Name)
		fceWriteVector(header, 0x5c+12*i, dummy.Position)
	}
	for i := len(cfg.Dummies); i < oldDummies; i++ {
		clear(header[0xa28+64*i : 0xa28+64*(i+1)])
		clear(header[0x5c+12*i : 0x5c+12*(i+1)])
	}
	size := fceVec{}
	if cfg.HalfSize != nil {
		size = *cfg.HalfSize
	} else {
		for i, m := range meshes {
			sets := [][]fceVec{m.vertices}
			if damage != nil {
				sets = append(sets, damage[i].vertices)
			}
			for _, vertices := range sets {
				for _, v := range vertices {
					for a := range size {
						x := float32(math.Abs(float64(v[a]) + float64(cfg.Parts[i].Origin[a])))
						if x > size[a] {
							size[a] = x
						}
					}
				}
			}
		}
	}
	fceWriteVector(header, 0x4c, size)
}

func matchingFCEDamage(parts []fcePart, meshes, damage []fceMesh) error {
	for i := range meshes {
		if len(meshes[i].vertices) != len(damage[i].vertices) || len(meshes[i].triangles) != len(damage[i].triangles) {
			return fmt.Errorf("damaged.obj: %s topology must match model.obj", parts[i].name)
		}
		for j, t := range meshes[i].triangles {
			if t.Vertices != damage[i].triangles[j].Vertices {
				return fmt.Errorf("damaged.obj: %s vertex order/topology must match model.obj", parts[i].name)
			}
		}
	}
	return nil
}

// CreateFCE builds an NFS4 FCE4 from a triangulated, UV-mapped Wavefront model
// without a donor. Generic/no materials use page 0 and flags 1. Encoded
// fce_tXXXXXXXX_fXXXXXXXX materials specify explicit game settings. Optional
// damaged geometry must have the same object names and indexed topology.
func CreateFCE(modelOBJ, damagedOBJ []byte) ([]byte, error) {
	d, err := parseOBJMaterials(modelOBJ, true)
	if err != nil {
		return nil, fmt.Errorf("model OBJ: %w", err)
	}
	if len(d.objects) == 0 || len(d.objects) > 64 {
		return nil, fmt.Errorf("model needs 1..64 named objects")
	}
	// This empty valid header provides zero-initialized tables, not donor data.
	original := make([]byte, fceHeaderSize)
	fcePut(original, 0, 0x101014)
	fcePut(original, 0x820, 1)
	for _, off := range []int{0x824, 0x864, 0x8a4, 0x8e4} {
		original[off+2] = 255
		original[off+3] = 255
	}
	cfg := &fceMetadata{TexturePages: 1, Dummies: []fceDummyMetadata{}}
	names := map[string]bool{}
	for _, obj := range d.objects {
		name := obj.name
		if !strings.HasPrefix(name, ":") {
			name = ":" + name
		}
		if names[name] {
			return nil, fmt.Errorf("duplicate FCE part name %q", name)
		}
		names[name] = true
		m := d.mesh(obj, fceVec{})
		if len(m.triangles) == 0 {
			return nil, fmt.Errorf("object %q has no triangles", obj.name)
		}
		min, max := m.vertices[0], m.vertices[0]
		for _, v := range m.vertices {
			for a, x := range v {
				if x < min[a] {
					min[a] = x
				}
				if x > max[a] {
					max[a] = x
				}
			}
		}
		var origin fceVec
		for a := range origin {
			origin[a] = float32((float64(min[a]) + float64(max[a])) / 2)
		}
		cfg.Parts = append(cfg.Parts, fcePartMetadata{Object: obj.name, Name: name, Origin: origin})
		for _, f := range obj.faces {
			if uint64(f.texture)+1 > math.MaxUint32 {
				return nil, fmt.Errorf("texture page cannot be represented")
			}
			if int(f.texture)+1 > cfg.TexturePages {
				cfg.TexturePages = int(f.texture) + 1
			}
		}
	}
	parts, err := cfg.parts(original)
	if err != nil {
		return nil, err
	}
	meshes, err := d.meshes(parts)
	if err != nil {
		return nil, err
	}
	var damage []fceMesh
	if damagedOBJ != nil {
		dd, err := parseOBJMaterials(damagedOBJ, true)
		if err != nil {
			return nil, fmt.Errorf("damaged OBJ: %w", err)
		}
		damage, err = dd.meshes(parts)
		if err != nil {
			return nil, fmt.Errorf("damaged OBJ: %w", err)
		}
		if err := matchingFCEDamage(parts, meshes, damage); err != nil {
			return nil, err
		}
	}
	out, err := rebuildFCE(original, cfg, parts, meshes, damage)
	if err != nil {
		return nil, err
	}
	if err := Validate("fce", out, 0); err != nil {
		return nil, fmt.Errorf("created fce: %w", err)
	}
	return out, nil
}
