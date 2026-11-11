package resources

// Wavefront is the editable surface of an FCE project. The original FCE is a
// template for names, pivots, dummies, colors, animation and unknown fields;
// OBJ object/material names carry part identity and triangle flags/page.
import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

type fceVec [3]float32

type fcePart struct {
	name                       string
	origin                     fceVec
	sourceOrigin               fceVec
	allowMissing               bool
	firstV, numV, firstT, numT int
}

func fceU32(data []byte, off int) int    { return int(binary.LittleEndian.Uint32(data[off:])) }
func fcePut(data []byte, off, value int) { binary.LittleEndian.PutUint32(data[off:], uint32(value)) }
func fceVector(data []byte, off int) (v fceVec) {
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[off+4*i:]))
	}
	return
}
func fceWriteVector(data []byte, off int, v fceVec) {
	for i, value := range v {
		binary.LittleEndian.PutUint32(data[off+4*i:], math.Float32bits(value))
	}
}
func fceTable(data []byte, field int) int { return fceHeaderSize + fceU32(data, field) }

func fceParts(data []byte) []fcePart {
	parts := make([]fcePart, fceU32(data, 0x11c))
	for i := range parts {
		name := strings.TrimRight(string(bytes.SplitN(data[0xe28+64*i:0xe28+64*(i+1)], []byte{0}, 2)[0]), " ")
		name = strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
				return r
			}
			return '_'
		}, name)
		// Blender 4.x limits object names to 63 bytes, including this prefix.
		if len(name) > 48 {
			name = name[:48]
		}
		origin := fceVector(data, 0x120+12*i)
		parts[i] = fcePart{name: fmt.Sprintf("part_%03d_%s", i, name), origin: origin, sourceOrigin: origin,
			firstV: fceU32(data, 0x420+4*i), numV: fceU32(data, 0x520+4*i), firstT: fceU32(data, 0x620+4*i), numT: fceU32(data, 0x720+4*i)}
		parts[i].allowMissing = parts[i].numV == 0 && parts[i].numT == 0
	}
	return parts
}

func fceMaterial(texture, flags uint32) string { return fmt.Sprintf("fce_t%08x_f%08x", texture, flags) }

func exportFCEOBJ(data []byte, damaged bool) []byte {
	var b bytes.Buffer
	b.WriteString("# NFS4 FCE4: keep object and material names. Y up, -Z forward.\nmtllib model.mtl\n")
	vField, nField := 0x14, 0x18
	if damaged {
		vField, nField = 0x34, 0x38
	}
	vBase, nBase, tBase := fceTable(data, vField), fceTable(data, nField), fceTable(data, 0x1c)
	vertex, uv := 1, 1
	for _, part := range fceParts(data) {
		fmt.Fprintf(&b, "\no %s\n", part.name)
		for i := 0; i < part.numV; i++ {
			v := fceVector(data, vBase+12*(part.firstV+i))
			fmt.Fprintf(&b, "v %.17g %.17g %.17g\n", float64(v[0])+float64(part.origin[0]), float64(v[1])+float64(part.origin[1]), float64(v[2])+float64(part.origin[2]))
		}
		for i := 0; i < part.numV; i++ {
			n := fceVector(data, nBase+12*(part.firstV+i))
			fmt.Fprintf(&b, "vn %s %s %s\n", objFloat(n[0]), objFloat(n[1]), objFloat(n[2]))
		}
		for i := 0; i < part.numT; i++ {
			t := data[tBase+56*(part.firstT+i):]
			for c := 0; c < 3; c++ {
				u := math.Float32frombits(binary.LittleEndian.Uint32(t[32+4*c:]))
				v := math.Float32frombits(binary.LittleEndian.Uint32(t[44+4*c:]))
				fmt.Fprintf(&b, "vt %s %.17g\n", objFloat(u), 1-float64(v))
			}
		}
		material := ""
		for i := 0; i < part.numT; i++ {
			t := data[tBase+56*(part.firstT+i):]
			m := fceMaterial(binary.LittleEndian.Uint32(t), binary.LittleEndian.Uint32(t[28:]))
			if m != material {
				fmt.Fprintf(&b, "usemtl %s\n", m)
				material = m
			}
			b.WriteString("f")
			for c := 0; c < 3; c++ {
				v := vertex + fceU32(t, 4+4*c)
				fmt.Fprintf(&b, " %d/%d/%d", v, uv, v)
				uv++
			}
			b.WriteByte('\n')
		}
		vertex += part.numV
	}
	return b.Bytes()
}

func objFloat(v float32) string { return strconv.FormatFloat(float64(v), 'g', -1, 32) }

func extractFCEOBJ(data []byte) (map[string][]byte, error) {
	if err := Validate("fce", data, 0); err != nil {
		return nil, fmt.Errorf("fce: %w", err)
	}
	var mtl bytes.Buffer
	mtl.WriteString("# Material names encode FCE texture page and flags; retain them on export.\n")
	seen := map[string]bool{}
	base := fceTable(data, 0x1c)
	for i := 0; i < fceU32(data, 8); i++ {
		t := data[base+56*i:]
		name := fceMaterial(binary.LittleEndian.Uint32(t), binary.LittleEndian.Uint32(t[28:]))
		if !seen[name] {
			fmt.Fprintf(&mtl, "\nnewmtl %s\nKd 0.8 0.8 0.8\n", name)
			seen[name] = true
		}
	}
	manifest, err := marshalJSON(&project{Version: 2, Format: "fce", FCE: metadataFromFCE(data)})
	if err != nil {
		return nil, err
	}
	return map[string][]byte{
		ManifestName: manifest,
		"model.obj":  exportFCEOBJ(data, false), "damaged.obj": exportFCEOBJ(data, true),
		"model.mtl": mtl.Bytes(), "original.fce": append([]byte(nil), data...),
	}, nil
}

type objCorner struct{ v, vt, vn int }
type objFace struct {
	corners        [3]objCorner
	texture, flags uint32
}
type objObject struct {
	name     string
	declared []int
	faces    []objFace
}
type objDocument struct {
	vertices   [][3]float64
	uvs        [][2]float64
	normals    []fceVec
	objects    []*objObject
	referenced map[int]bool
}

// OBJ indices are independent, may be negative, and are global across objects.
func objIndex(s string, count int) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n == 0 {
		return 0, fmt.Errorf("invalid OBJ index %q", s)
	}
	if n < 0 {
		n += count
	} else {
		n--
	}
	if n < 0 || n >= count {
		return 0, fmt.Errorf("OBJ index %q outside table of %d entries", s, count)
	}
	return n, nil
}

func parseOBJ(data []byte) (*objDocument, error) {
	return parseOBJMaterials(data, false)
}

func parseOBJMaterials(data []byte, genericMaterials bool) (*objDocument, error) {
	d := &objDocument{referenced: map[int]bool{}}
	var current *objObject
	var texture uint32
	flags := uint32(1)
	hasMaterial := genericMaterials
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 4096), 4*1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		fields := strings.Fields(strings.SplitN(scanner.Text(), "#", 2)[0])
		if len(fields) == 0 {
			continue
		}
		fail := func(err error) (*objDocument, error) { return nil, fmt.Errorf("line %d: %w", line, err) }
		switch fields[0] {
		case "v", "vn", "vt":
			n := 3
			if fields[0] == "vt" {
				n = 2
			}
			if len(fields) != n+1 {
				return fail(fmt.Errorf("%s needs %d coordinates", fields[0], n))
			}
			var v [3]float64
			for i := 0; i < n; i++ {
				f, err := strconv.ParseFloat(fields[i+1], 64)
				if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || math.Abs(f) > math.MaxFloat32 {
					return fail(fmt.Errorf("invalid coordinate %q", fields[i+1]))
				}
				v[i] = f
			}
			switch fields[0] {
			case "v":
				d.vertices = append(d.vertices, v)
				if current != nil {
					current.declared = append(current.declared, len(d.vertices)-1)
				}
			case "vt":
				d.uvs = append(d.uvs, [2]float64{v[0], v[1]})
			case "vn":
				d.normals = append(d.normals, fceVec{float32(v[0]), float32(v[1]), float32(v[2])})
			}
		case "o":
			if len(fields) != 2 {
				return fail(fmt.Errorf("expected a single FCE object name"))
			}
			current = &objObject{name: fields[1]}
			d.objects = append(d.objects, current)
		case "usemtl":
			if len(fields) != 2 {
				return fail(fmt.Errorf("missing FCE material name"))
			}
			var t, f uint32
			if _, err := fmt.Sscanf(fields[1], "fce_t%08x_f%08x", &t, &f); err != nil || fceMaterial(t, f) != fields[1] {
				if !genericMaterials || strings.HasPrefix(fields[1], "fce_") {
					return fail(fmt.Errorf("unknown material %q; retain fce_tXXXXXXXX_fXXXXXXXX names", fields[1]))
				}
				t, f = 0, 1
			}
			texture, flags, hasMaterial = t, f, true
		case "f":
			if current == nil {
				return fail(fmt.Errorf("face outside a named FCE object"))
			}
			if !hasMaterial {
				return fail(fmt.Errorf("face has no FCE material; export materials from Blender"))
			}
			if len(fields) != 4 {
				return fail(fmt.Errorf("FCE requires triangles; triangulate faces before exporting"))
			}
			face := objFace{texture: texture, flags: flags}
			for i, field := range fields[1:] {
				indices := strings.Split(field, "/")
				if len(indices) > 3 {
					return fail(fmt.Errorf("invalid face corner %q", field))
				}
				corner := objCorner{vt: -1, vn: -1}
				var err error
				corner.v, err = objIndex(indices[0], len(d.vertices))
				if err != nil {
					return fail(err)
				}
				if len(indices) < 2 || indices[1] == "" {
					return fail(fmt.Errorf("missing UV coordinates; export UVs from Blender"))
				}
				corner.vt, err = objIndex(indices[1], len(d.uvs))
				if err != nil {
					return fail(err)
				}
				if len(indices) == 3 && indices[2] != "" {
					corner.vn, err = objIndex(indices[2], len(d.normals))
					if err != nil {
						return fail(err)
					}
				}
				face.corners[i] = corner
				d.referenced[corner.v] = true
			}
			current.faces = append(current.faces, face)
		case "mtllib", "g", "s": // Object identity comes from 'o', not material/smoothing groups.
		default:
			return fail(fmt.Errorf("unsupported OBJ statement %q", fields[0]))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return d, nil
}

type fceMesh struct {
	vertices, normals []fceVec
	normalKnown       []bool
	triangles         []triangle
}

func normalized(v fceVec) fceVec {
	l := math.Sqrt(float64(v[0])*float64(v[0]) + float64(v[1])*float64(v[1]) + float64(v[2])*float64(v[2]))
	if l != 0 {
		for i := range v {
			v[i] = float32(float64(v[i]) / l)
		}
	}
	return v
}

func (d *objDocument) mesh(o *objObject, origin fceVec) fceMesh {
	// Sort by source vertex index, retaining loose vertices and original ordering.
	used := map[int]bool{}
	for _, f := range o.faces {
		for _, c := range f.corners {
			used[c.v] = true
		}
	}
	for _, v := range o.declared {
		// Vertex declarations are global in OBJ. Some writers put the entire
		// pool under the first object; only genuinely loose vertices belong
		// to their declaration's object rather than their referencing faces.
		if !d.referenced[v] {
			used[v] = true
		}
	}
	indices := make([]int, 0, len(used))
	for v := range used {
		indices = append(indices, v)
	}
	sort.Ints(indices)
	m := fceMesh{}
	base := map[int]int{}
	for _, index := range indices {
		base[index] = len(m.vertices)
		v := d.vertices[index]
		m.vertices = append(m.vertices, fceVec{float32(v[0] - float64(origin[0])), float32(v[1] - float64(origin[1])), float32(v[2] - float64(origin[2]))})
		m.normals = append(m.normals, fceVec{})
		m.normalKnown = append(m.normalKnown, false)
	}
	// If normals were omitted, generate area-weighted vertex normals.
	generated := make([]fceVec, len(m.vertices))
	for _, f := range o.faces {
		a, b, c := m.vertices[base[f.corners[0].v]], m.vertices[base[f.corners[1].v]], m.vertices[base[f.corners[2].v]]
		u, v := fceVec{b[0] - a[0], b[1] - a[1], b[2] - a[2]}, fceVec{c[0] - a[0], c[1] - a[1], c[2] - a[2]}
		n := fceVec{u[1]*v[2] - u[2]*v[1], u[2]*v[0] - u[0]*v[2], u[0]*v[1] - u[1]*v[0]}
		for _, c := range f.corners {
			for axis := 0; axis < 3; axis++ {
				generated[base[c.v]][axis] += n[axis]
			}
		}
	}
	type key struct {
		vertex int
		normal fceVec
	}
	assigned := map[int]bool{}
	lookup := map[key]int{}
	for _, f := range o.faces {
		t := triangle{Texture: f.texture, Flags: f.flags}
		for i, c := range f.corners {
			n := normalized(generated[base[c.v]])
			if c.vn >= 0 {
				n = d.normals[c.vn]
			}
			k := key{c.v, n}
			index, ok := lookup[k]
			if !ok {
				index = base[c.v]
				if assigned[c.v] {
					index = len(m.vertices)
					m.vertices = append(m.vertices, m.vertices[base[c.v]])
					m.normals = append(m.normals, n)
					m.normalKnown = append(m.normalKnown, true)
				} else {
					m.normals[index] = n
					m.normalKnown[index] = true
					assigned[c.v] = true
				}
				lookup[k] = index
			}
			t.Vertices[i] = uint32(index)
			t.U[i], t.V[i] = float32(d.uvs[c.vt][0]), float32(1-d.uvs[c.vt][1])
		}
		m.triangles = append(m.triangles, t)
	}
	return m
}

func importFCEOBJ(data []byte, parts []fcePart) ([]fceMesh, error) {
	d, err := parseOBJ(data)
	if err != nil {
		return nil, err
	}
	return d.meshes(parts)
}

func (d *objDocument) meshes(parts []fcePart) ([]fceMesh, error) {
	byName := map[string]*objObject{}
	valid := map[string]bool{}
	for _, p := range parts {
		valid[p.name] = true
	}
	for _, o := range d.objects {
		if !valid[o.name] {
			return nil, fmt.Errorf("unknown object %q; retain the original part names", o.name)
		}
		if byName[o.name] != nil {
			return nil, fmt.Errorf("duplicate object %q", o.name)
		}
		byName[o.name] = o
	}
	meshes := make([]fceMesh, len(parts))
	for i, p := range parts {
		o := byName[p.name]
		if o == nil {
			if p.allowMissing {
				continue
			} // Blender may omit empty objects.
			return nil, fmt.Errorf("missing object %q", p.name)
		}
		meshes[i] = d.mesh(o, p.origin)
	}
	return meshes, nil
}

func packFCEOBJ(files map[string][]byte) ([]byte, error) {
	for _, name := range []string{"original.fce", "model.obj", "damaged.obj", "model.mtl"} {
		if _, ok := files[name]; !ok {
			return nil, fmt.Errorf("missing project file %s", name)
		}
	}
	for name := range files {
		switch name {
		case ManifestName, "original.fce", "model.obj", "damaged.obj", "model.mtl", "damaged.mtl":
		default:
			return nil, fmt.Errorf("unreferenced project file %s", name)
		}
	}
	original := files["original.fce"]
	if err := Validate("fce", original, 0); err != nil {
		return nil, fmt.Errorf("original.fce: %w", err)
	}
	baseChanged := !bytes.Equal(files["model.obj"], exportFCEOBJ(original, false))
	damageChanged := !bytes.Equal(files["damaged.obj"], exportFCEOBJ(original, true))
	var p project
	decoder := json.NewDecoder(bytes.NewReader(files[ManifestName]))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&p); err != nil {
		return nil, fmt.Errorf("%s: %w", ManifestName, err)
	}
	cfg := p.FCE
	if cfg == nil {
		cfg = metadataFromFCE(original)
	}
	if cfg.Colors == nil {
		cfg.Colors = metadataFromFCE(original).Colors
	}
	parts, err := cfg.parts(original)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ManifestName, err)
	}
	if !baseChanged && !damageChanged && reflect.DeepEqual(cfg, metadataFromFCE(original)) {
		return append([]byte(nil), original...), nil
	}
	meshes, err := importFCEOBJ(files["model.obj"], parts)
	if err != nil {
		return nil, fmt.Errorf("model.obj: %w", err)
	}
	var damage []fceMesh
	if damageChanged {
		damage, err = importFCEOBJ(files["damaged.obj"], parts)
		if err != nil {
			return nil, fmt.Errorf("damaged.obj: %w", err)
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
		return nil, fmt.Errorf("rebuilt fce: %w", err)
	}
	return out, nil
}

var fceGeometryTables = []struct {
	field, stride int
	triangles     bool
}{
	{0x14, 12, false}, {0x18, 12, false}, {0x1c, 56, true},
	{0x20, 32, false}, {0x24, 12, false}, {0x28, 12, false},
	{0x34, 12, false}, {0x38, 12, false}, {0x3c, 4, false},
	{0x40, 4, false}, {0x44, 4, false}, {0x48, 12, true},
}

func rebuildFCE(original []byte, cfg *fceMetadata, parts []fcePart, meshes, damage []fceMesh) ([]byte, error) {
	header := append([]byte(nil), original[:fceHeaderSize]...)
	cfg.writeHeader(header, meshes, damage)
	var vertexSource, triangleSource []int
	var positions, normals, damagedPositions, damagedNormals []fceVec
	var triangles []triangle
	for i, p := range parts {
		m := meshes[i]
		fcePut(header, 0x420+4*i, len(positions))
		fcePut(header, 0x520+4*i, len(m.vertices))
		fcePut(header, 0x620+4*i, len(triangles))
		fcePut(header, 0x720+4*i, len(m.triangles))
		for j, v := range m.vertices {
			// Unchanged vertex counts retain index-based game data. With changed
			// topology transfer animation/reserved data and damage from the nearest
			// original local vertex; never borrow it from another part.
			source := -1
			if len(m.vertices) == p.numV {
				source = p.firstV + j
			} else {
				best := math.Inf(1)
				for k := 0; k < p.numV; k++ {
					old := fceVector(original, fceTable(original, 0x14)+12*(p.firstV+k))
					d := float64(0)
					for a := range v {
						delta := float64(v[a]) + float64(p.origin[a]) - float64(old[a]) - float64(p.sourceOrigin[a])
						d += delta * delta
					}
					if d < best {
						best, source = d, p.firstV+k
					}
				}
			}
			vertexSource = append(vertexSource, source)
			positions = append(positions, v)
			n := m.normals[j]
			if !m.normalKnown[j] && source >= 0 {
				n = fceVector(original, fceTable(original, 0x18)+12*source)
			}
			normals = append(normals, n)
			dv, dn := v, n
			if damage != nil {
				dv, dn = damage[i].vertices[j], damage[i].normals[j]
				if !damage[i].normalKnown[j] && source >= 0 {
					dn = fceVector(original, fceTable(original, 0x38)+12*source)
				}
			} else if source >= 0 {
				ov := fceVector(original, fceTable(original, 0x14)+12*source)
				od := fceVector(original, fceTable(original, 0x34)+12*source)
				on := fceVector(original, fceTable(original, 0x18)+12*source)
				nd := fceVector(original, fceTable(original, 0x38)+12*source)
				for a := range dv {
					// Subtract before adding so an unchanged float keeps its bits.
					dv[a] = od[a] + (v[a] - ov[a])
					dn[a] = nd[a] + (n[a] - on[a])
				}
			}
			damagedPositions = append(damagedPositions, dv)
			damagedNormals = append(damagedNormals, dn)
		}
		for j, t := range m.triangles {
			source := -1
			if j < p.numT {
				source = p.firstT + j
				old := original[fceTable(original, 0x1c)+56*source:]
				for c := range t.Reserved {
					t.Reserved[c] = binary.LittleEndian.Uint32(old[16+4*c:])
				}
			}
			triangleSource = append(triangleSource, source)
			triangles = append(triangles, t)
		}
	}
	if cfg.HalfSize == nil {
		// Also cover damage inherited from a donor, not just an explicitly
		// supplied damaged OBJ (which writeHeader has already included).
		size := fceVector(header, 0x4c)
		for i, p := range parts {
			first := fceU32(header, 0x420+4*i)
			for j := range meshes[i].vertices {
				for a, value := range damagedPositions[first+j] {
					x := float32(math.Abs(float64(value) + float64(p.origin[a])))
					if x > size[a] {
						size[a] = x
					}
				}
			}
		}
		fceWriteVector(header, 0x4c, size)
	}
	fcePut(header, 8, len(triangles))
	fcePut(header, 0xc, len(positions))
	tables := map[int][]byte{}
	for field, vectors := range map[int][]fceVec{0x14: positions, 0x18: normals, 0x34: damagedPositions, 0x38: damagedNormals} {
		b := make([]byte, 12*len(vectors))
		for i, v := range vectors {
			fceWriteVector(b, 12*i, v)
		}
		tables[field] = b
	}
	// Original game files also contain independent undamaged tables, whereas
	// some editors alias them to the active tables. Preserve their own values
	// and apply the same edit delta instead of leaving stale geometry there.
	for _, pair := range [][2]int{{0x2c, 0x14}, {0x30, 0x18}} {
		vectors := positions
		if pair[1] == 0x18 {
			vectors = normals
		}
		b := make([]byte, 12*len(vectors))
		for i, v := range vectors {
			if source := vertexSource[i]; source >= 0 {
				old := fceVector(original, fceTable(original, pair[1])+12*source)
				undamaged := fceVector(original, fceTable(original, pair[0])+12*source)
				for a := range v {
					v[a] = undamaged[a] + (v[a] - old[a])
				}
			}
			fceWriteVector(b, 12*i, v)
		}
		tables[pair[0]] = b
	}
	tables[0x1c] = encodeFCETriangles(triangles)
	for _, table := range fceGeometryTables {
		if tables[table.field] != nil {
			continue
		}
		sources := vertexSource
		if table.triangles {
			sources = triangleSource
		}
		b := make([]byte, table.stride*len(sources))
		for i, source := range sources {
			if source >= 0 {
				off := fceTable(original, table.field) + table.stride*source
				copy(b[i*table.stride:], original[off:off+table.stride])
			}
		}
		tables[table.field] = b
	}
	// Preserve physical layout (including gaps and trailing bytes) when sizes
	// did not change. For resized models replace each table in physical order.
	type span struct{ start, size, field int }
	var spans []span
	for _, table := range fceGeometryTables {
		count := fceU32(original, 0xc)
		if table.triangles {
			count = fceU32(original, 8)
		}
		spans = append(spans, span{fceTable(original, table.field), count * table.stride, table.field})
	}
	aliases := map[int]int{}
	var appended []int
	for _, pair := range [][2]int{{0x2c, 0x14}, {0x30, 0x18}} {
		if fceU32(original, pair[0]) == fceU32(original, pair[1]) {
			aliases[pair[0]] = pair[1]
			continue
		}
		s := span{fceTable(original, pair[0]), fceU32(original, 0xc) * 12, pair[0]}
		overlap := false
		for _, other := range spans {
			if s.size > 0 && other.size > 0 && s.start < other.start+other.size && other.start < s.start+s.size {
				overlap = true
				break
			}
		}
		if overlap {
			// Exotic partial aliases cannot be overwritten without also editing
			// their neighbor. Give the undamaged table its own storage instead.
			appended = append(appended, pair[0])
		} else {
			spans = append(spans, s)
		}
	}
	sort.SliceStable(spans, func(i, j int) bool {
		if spans[i].start == spans[j].start {
			return spans[i].size < spans[j].size
		}
		return spans[i].start < spans[j].start
	})
	out := header
	last := fceHeaderSize
	for _, s := range spans {
		if s.size == 0 {
			continue
		}
		out = append(out, original[last:s.start]...)
		fcePut(out, s.field, len(out)-fceHeaderSize)
		out = append(out, tables[s.field]...)
		last = s.start + s.size
	}
	out = append(out, original[last:]...)
	for _, s := range spans {
		if s.size == 0 {
			fcePut(out, s.field, len(out)-fceHeaderSize)
			out = append(out, tables[s.field]...)
		}
	}
	for field, target := range aliases {
		fcePut(out, field, fceU32(out, target))
	}
	for _, field := range appended {
		fcePut(out, field, len(out)-fceHeaderSize)
		out = append(out, tables[field]...)
	}
	return out, nil
}
