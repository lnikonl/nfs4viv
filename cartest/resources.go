package cartest

import (
	"encoding/binary"
	"math"

	"github.com/lnikonl/nfs4viv/fedata"
	"github.com/lnikonl/nfs4viv/viv"
)

// ResourceFiles builds small, structurally valid NFS4 resources for codec and
// validator tests. No data is copied from original game assets.
func ResourceFiles() map[string][]byte {
	return map[string][]byte{
		"car.fce":    resourceFCE(),
		"car00.tga":  resourceTGA(),
		"cop.art":    resourceART(),
		"carp.txt":   []byte("Serial Number(0)\r\n7\r\nCar Classification(1)\r\n0\r\n"),
		"dash.fsh":   resourceFSH(),
		"careng.bnk": resourceBNK(),
		"careng.ctb": resourceCRD(),
		"careng.ltb": resourceCRD(),
		"fedata.eng": fedata.New().Bytes(),
	}
}

// ResourceArchive collects the valid resource fixtures in a CAR archive.
func ResourceArchive() *viv.File {
	files := ResourceFiles()
	archive := &viv.File{}
	for _, name := range []string{"car.fce", "car00.tga", "cop.art", "carp.txt", "dash.fsh", "careng.bnk", "careng.ctb", "careng.ltb", "fedata.eng"} {
		archive.Entries = append(archive.Entries, viv.Entry{Name: name, Data: files[name]})
	}
	return archive
}

func resourceFCE() []byte {
	header := make([]byte, 0x2038)
	u32 := func(off int, value uint32) { binary.LittleEndian.PutUint32(header[off:], value) }
	u32(0, 0x101014)
	u32(8, 1)
	u32(0xc, 3)
	u32(0x10, 1)
	u32(0x11c, 1)
	u32(0x520, 3)
	u32(0x720, 1)
	u32(0x820, 1)
	for _, off := range []int{0x4c, 0x50, 0x54} {
		u32(off, math.Float32bits(1))
	}
	copy(header[0xe28:], ":HB")
	var pool []byte
	for _, table := range []struct{ off, count, stride int }{
		{0x14, 3, 12}, {0x18, 3, 12}, {0x1c, 1, 56}, {0x20, 3, 32},
		{0x24, 3, 12}, {0x28, 3, 12}, {0x34, 3, 12}, {0x38, 3, 12},
		{0x3c, 3, 4}, {0x40, 3, 4}, {0x44, 3, 4}, {0x48, 1, 12},
	} {
		u32(table.off, uint32(len(pool)))
		b := make([]byte, table.count*table.stride)
		if table.off == 0x14 || table.off == 0x34 {
			for i := 0; i < 3; i++ {
				binary.LittleEndian.PutUint32(b[12*i+4*i:], math.Float32bits(1))
			}
		}
		if table.off == 0x1c {
			binary.LittleEndian.PutUint32(b[8:], 1)
			binary.LittleEndian.PutUint32(b[12:], 2)
			binary.LittleEndian.PutUint32(b[28:], 1)
			binary.LittleEndian.PutUint32(b[36:], math.Float32bits(1))
			binary.LittleEndian.PutUint32(b[52:], math.Float32bits(1))
		}
		pool = append(pool, b...)
	}
	u32(0x2c, 0)
	u32(0x30, binary.LittleEndian.Uint32(header[0x18:]))
	return append(header, pool...)
}

func resourceTGA() []byte {
	data := make([]byte, 18+8*8*4)
	data[2], data[16], data[17] = 2, 32, 8
	binary.LittleEndian.PutUint16(data[12:], 8)
	binary.LittleEndian.PutUint16(data[14:], 8)
	for i := 18; i < len(data); i++ {
		data[i] = byte(i * 17)
	}
	return data
}

func resourceART() []byte {
	var data []byte
	for index, image := range []struct{ width, height int }{{4, 2}, {2, 3}, {1, 1}} {
		header := make([]byte, 8)
		binary.LittleEndian.PutUint32(header, uint32(image.width))
		binary.LittleEndian.PutUint32(header[4:], uint32(image.height))
		data = append(data, header...)
		for y := 0; y < image.height; y++ {
			for x := 0; x < image.width; x++ {
				data = append(data, byte(16*index+4*y+x), byte(32+4*y+x), byte(64+4*y+x), byte(128+4*y+x))
			}
		}
	}
	return data
}

func resourceFSH() []byte {
	header := make([]byte, 32)
	copy(header, "SHPI")
	binary.LittleEndian.PutUint32(header[8:], 2)
	copy(header[12:], "GIMX")
	copy(header[16:], "idx0")
	binary.LittleEndian.PutUint32(header[20:], 32)
	copy(header[24:], "rgb1")
	first := make([]byte, 16+16+16+16)
	first[0], first[1] = 0x7b, 32
	binary.LittleEndian.PutUint16(first[4:], 4)
	binary.LittleEndian.PutUint16(first[6:], 4)
	for i := 16; i < 32; i++ {
		first[i] = byte(i % 4)
	}
	first[32] = 0x2a
	binary.LittleEndian.PutUint16(first[36:], 4)
	binary.LittleEndian.PutUint16(first[38:], 1)
	copy(first[48:], []byte{0, 0, 0, 0, 10, 20, 30, 127, 60, 70, 80, 255, 200, 210, 220, 255})
	second := make([]byte, 16+32)
	second[0] = 0x7e
	binary.LittleEndian.PutUint16(second[4:], 4)
	binary.LittleEndian.PutUint16(second[6:], 4)
	for i := 16; i < len(second); i += 2 {
		binary.LittleEndian.PutUint16(second[i:], uint16(i*997))
	}
	binary.LittleEndian.PutUint32(header[28:], uint32(len(header)+len(first)))
	data := append(append(header, first...), second...)
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)))
	return data
}

func resourceBNK() []byte {
	data := make([]byte, 296)
	copy(data, "BNKl")
	binary.LittleEndian.PutUint16(data[4:], 4)
	binary.LittleEndian.PutUint16(data[6:], 3)
	binary.LittleEndian.PutUint32(data[8:], 256)
	binary.LittleEndian.PutUint32(data[12:], 40)
	binary.LittleEndian.PutUint32(data[16:], 0xffffffff)
	for _, stream := range []struct{ index, header, offset, channels int }{{0, 40, 256, 2}, {2, 104, 288, 1}} {
		binary.LittleEndian.PutUint32(data[20+4*stream.index:], uint32(stream.header-20-4*stream.index))
		pt := []byte{'P', 'T', 0, 0, 0xfd}
		for _, property := range []struct{ key, width, value int }{
			{0x82, 1, stream.channels}, {0x84, 2, 22050}, {0x85, 4, 4},
			{0x86, 4, 0}, {0x87, 4, 4}, {0x88, 4, stream.offset}, {0x92, 1, 2}, {0x8a, 4, 0},
		} {
			pt = append(pt, byte(property.key), byte(property.width))
			for i := property.width - 1; i >= 0; i-- {
				pt = append(pt, byte(property.value>>(8*i)))
			}
		}
		copy(data[stream.header:], append(pt, 0xff))
	}
	for i := 256; i < len(data); i++ {
		data[i] = byte(i*23 + 7)
	}
	return data
}

func resourceCRD() []byte {
	data := make([]byte, 0x168+1024)
	copy(data, "CRDl")
	binary.LittleEndian.PutUint16(data[4:], 2)
	data[7] = 100
	binary.LittleEndian.PutUint32(data[0x128:], 0x168-0x128)
	binary.LittleEndian.PutUint32(data[0x148:], 0x368-0x148)
	for i := 0x168; i < len(data); i++ {
		data[i] = byte(i * 13)
	}
	return data
}
