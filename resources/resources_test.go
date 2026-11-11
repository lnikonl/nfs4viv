package resources

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math"
	"strings"
	"testing"

	"github.com/lnikonl/nfs4viv/cartest"
)

func TestSyntheticResourceRoundTrips(t *testing.T) {
	for name, data := range cartest.ResourceFiles() {
		format := strings.TrimPrefix(name[strings.LastIndex(name, "."):], ".")
		if !Supported(format) {
			continue
		}
		t.Run(format, func(t *testing.T) {
			files, err := Extract(format, data)
			if err != nil {
				t.Fatal(err)
			}
			out, err := Pack(format, files)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(out, data) {
				t.Fatal("resource round trip changed bytes")
			}
		})
	}
}

func TestFCEGeometryEdit(t *testing.T) {
	data := cartest.ResourceFiles()["car.fce"]
	files, err := Extract("fce", data)
	if err != nil {
		t.Fatal(err)
	}
	files["model.obj"] = bytes.Replace(files["model.obj"], []byte("v 1 0 0\n"), []byte("v 2.5 0 0\n"), 1)
	out, err := Pack("fce", files)
	if err != nil {
		t.Fatal(err)
	}
	position := fceHeaderSize + int(binary.LittleEndian.Uint32(data[0x14:]))
	want := append([]byte(nil), data...)
	binary.LittleEndian.PutUint32(want[position:], math.Float32bits(2.5))
	// An unedited damage mesh follows the base edit, retaining its deformation.
	damaged := fceTable(data, 0x34)
	binary.LittleEndian.PutUint32(want[damaged:], math.Float32bits(2.5))
	if !bytes.Equal(out, want) {
		t.Fatal("geometry edit changed unrelated FCE data")
	}
	files["model.obj"] = bytes.Replace(files["model.obj"], []byte("f 1/1/1 2/2/2 3/3/3"), []byte("f 99/1/1 2/2/2 3/3/3"), 1)
	if _, err := Pack("fce", files); err == nil {
		t.Fatal("invalid OBJ indices must not silently corrupt the model")
	}
}

func TestFSHResizeAndPaletteEdit(t *testing.T) {
	data := cartest.ResourceFiles()["dash.fsh"]
	files, err := Extract("fsh", data)
	if err != nil {
		t.Fatal(err)
	}
	im, err := decodeTGA(files["000_idx0.tga"])
	if err != nil {
		t.Fatal(err)
	}
	im.Width, im.Height = 512, 8
	im.Pixels = make([]byte, im.Width*im.Height)
	for i := range im.Pixels {
		im.Pixels[i] = byte(i % 4)
	}
	im.Palette[4] = 222
	files["000_idx0.tga"] = encodeTGA(im)
	out, err := Pack("fsh", files)
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate("fsh", out, 0); err != nil {
		t.Fatalf("remastered size rejected by default: %v", err)
	}
	if err := Validate("fsh", out, 256); err == nil {
		t.Fatal("stock texture limit was ignored")
	}
	first := int(binary.LittleEndian.Uint32(out[20:]))
	second := int(binary.LittleEndian.Uint32(out[28:]))
	if first != 32 || second != 96+512*8-16 || int(binary.LittleEndian.Uint32(out[4:])) != len(out) {
		t.Fatalf("FSH offsets/length were not rebuilt: first=%d second=%d size=%d", first, second, len(out))
	}
	oldSecond := int(binary.LittleEndian.Uint32(data[28:]))
	if !bytes.Equal(out[second:], data[oldSecond:]) {
		t.Fatal("resizing the first image changed the second image")
	}
	again, err := Extract("fsh", out)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeTGA(again["000_idx0.tga"])
	if err != nil {
		t.Fatal(err)
	}
	if got.Width != im.Width || got.Height != im.Height || !bytes.Equal(got.Pixels, im.Pixels) || !bytes.Equal(got.Palette, im.Palette) {
		t.Fatal("edited FSH pixels/palette did not survive repacking")
	}
}

func TestARTImageEdit(t *testing.T) {
	original := cartest.ResourceFiles()["cop.art"]
	files, err := Extract("art", original)
	if err != nil {
		t.Fatal(err)
	}
	source, err := png.Decode(bytes.NewReader(files["image_000.png"]))
	if err != nil {
		t.Fatal(err)
	}
	if source.Bounds().Dx() != 4 || source.Bounds().Dy() != 2 {
		t.Fatalf("first ART image decoded as %v", source.Bounds())
	}
	if got := source.At(0, 0); color.NRGBAModel.Convert(got) != (color.NRGBA{68, 36, 4, 132}) {
		t.Fatalf("ART pixel channel/origin conversion: %v", got)
	}
	im := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	im.SetNRGBA(0, 0, color.NRGBA{10, 20, 30, 40})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, im); err != nil {
		t.Fatal(err)
	}
	files["image_000.png"] = encoded.Bytes()
	out, err := Pack("art", files)
	if err != nil {
		t.Fatal(err)
	}
	pixel := 8 + 4*(1*4+0)
	want := []byte{30, 20, 10, 40}
	if !bytes.Equal(out[pixel:pixel+4], want) {
		t.Fatalf("edited ART pixel = %v, want %v", out[pixel:pixel+4], want)
	}
	if err := Validate("art", out, 3); err == nil {
		t.Fatal("ART texture limit was ignored")
	}
	if err := Validate("art", out, 4); err != nil {
		t.Fatal(err)
	}
}

func TestARTImageResize(t *testing.T) {
	original := cartest.ResourceFiles()["cop.art"]
	files, err := Extract("art", original)
	if err != nil {
		t.Fatal(err)
	}
	im := image.NewNRGBA(image.Rect(0, 0, 3, 1))
	im.SetNRGBA(1, 0, color.NRGBA{1, 2, 3, 4})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, im); err != nil {
		t.Fatal(err)
	}
	files["image_001.png"] = encoded.Bytes()
	out, err := Pack("art", files)
	if err != nil {
		t.Fatal(err)
	}
	offset := 8 + 4*4*2
	if binary.LittleEndian.Uint32(out[offset:]) != 3 || binary.LittleEndian.Uint32(out[offset+4:]) != 1 {
		t.Fatal("resized ART header was not rebuilt")
	}
	if !bytes.Equal(out[offset+8+4:offset+8+8], []byte{3, 2, 1, 4}) {
		t.Fatalf("resized ART pixel = %v", out[offset+8+4:offset+8+8])
	}
	if _, err := Extract("art", out); err != nil {
		t.Fatal(err)
	}
}

func TestBNKSampleResize(t *testing.T) {
	data := cartest.ResourceFiles()["careng.bnk"]
	files, err := Extract("bnk", data)
	if err != nil {
		t.Fatal(err)
	}
	info, samples, err := decodeWAV(files["sample_000.wav"])
	if err != nil {
		t.Fatal(err)
	}
	samples = append(append([]byte(nil), samples...), []byte{1, 2, 3, 4, 5, 6, 7, 8}...)
	files["sample_000.wav"] = encodeWAV(info, samples)
	out, err := Pack("bnk", files)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(data)+8 || int(binary.LittleEndian.Uint32(out[12:])) != len(out)-256 {
		t.Fatal("BNK pool size was not rebuilt")
	}
	again, err := Extract("bnk", out)
	if err != nil {
		t.Fatal(err)
	}
	_, got, err := decodeWAV(again["sample_000.wav"])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(samples, got) || !bytes.Equal(files["sample_002.wav"], again["sample_002.wav"]) {
		t.Fatal("BNK audio data or the other sample was corrupted")
	}
	info.Rate = 44100
	files["sample_000.wav"] = encodeWAV(info, samples)
	if _, err := Pack("bnk", files); err == nil {
		t.Fatal("unsupported sample-rate edit was ignored")
	}
}

func TestBNKMonoSampleAlignment(t *testing.T) {
	data := cartest.ResourceFiles()["careng.bnk"]
	data[47] = 1 // First PT header: channels property, mono instead of stereo.
	files, err := Extract("bnk", data)
	if err != nil {
		t.Fatal(err)
	}
	info, samples, err := decodeWAV(files["sample_000.wav"])
	if err != nil {
		t.Fatal(err)
	}
	samples = append(append([]byte(nil), samples...), 1, 2)
	files["sample_000.wav"] = encodeWAV(info, samples)
	out, err := Pack("bnk", files)
	if err != nil {
		t.Fatal(err)
	}
	p := &project{Format: "bnk"}
	if err := p.parseBNK(out); err != nil {
		t.Fatal(err)
	}
	for _, s := range p.Sections {
		if s.Offset%4 != 0 {
			t.Errorf("sample %s lost its 4-byte alignment", s.File)
		}
	}
	again, err := Extract("bnk", out)
	if err != nil || !bytes.Equal(again["sample_002.wav"], files["sample_002.wav"]) {
		t.Fatalf("alignment padding corrupted the following sample: %v", err)
	}
}

func TestTGAImportRLEAndOrigin(t *testing.T) {
	im := tgaImage{Width: 2, Height: 2, Pixels: make([]byte, 16)}
	header := append([]byte(nil), encodeTGA(im)[:18]...)
	header[2], header[17] = 10, 8 // RLE, bottom-left origin.
	data := append(header, 0x81, 1, 2, 3, 4, 0x01, 5, 6, 7, 8, 9, 10, 11, 12)
	got, err := decodeTGA(data)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{5, 6, 7, 8, 9, 10, 11, 12, 1, 2, 3, 4, 1, 2, 3, 4}
	if !bytes.Equal(got.Pixels, want) {
		t.Fatalf("RLE/origin conversion: got %v, want %v", got.Pixels, want)
	}
}

func TestCurveEdit(t *testing.T) {
	for _, format := range []string{"ctb", "ltb"} {
		t.Run(format, func(t *testing.T) {
			original := cartest.ResourceFiles()["careng."+format]
			files, err := Extract(format, original)
			if err != nil {
				t.Fatal(err)
			}
			var values []int
			if err := json.Unmarshal(files["curve_00.json"], &values); err != nil {
				t.Fatal(err)
			}
			values[17] = 201
			files["curve_00.json"], _ = json.Marshal(values)
			out, err := Pack(format, files)
			if err != nil {
				t.Fatal(err)
			}
			want := append([]byte(nil), original...)
			want[0x168+17] = 201
			if !bytes.Equal(want, out) {
				t.Fatal("curve edit changed unrelated bytes")
			}
		})
	}
}

func TestCompressedFSH(t *testing.T) {
	original := cartest.ResourceFiles()["dash.fsh"]
	compressed, err := encodeRefPack(original)
	if err != nil {
		t.Fatal(err)
	}
	files, err := Extract("fsh", compressed)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Pack("fsh", files)
	if err != nil || !bytes.Equal(out, compressed) {
		t.Fatalf("compressed round trip: %v", err)
	}
	im, err := decodeTGA(files["001_rgb1.tga"])
	if err != nil {
		t.Fatal(err)
	}
	im.Pixels[0] = 255
	files["001_rgb1.tga"] = encodeTGA(im)
	out, err = Pack("fsh", files)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeRefPack(out)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(decoded, original) {
		t.Fatal("edited compressed image was ignored")
	}
	if err := Validate("fsh", out, 0); err != nil {
		t.Fatal(err)
	}
}

func TestRefPackBackReferences(t *testing.T) {
	for _, test := range []struct {
		command []byte
		want    string
	}{
		{[]byte{0x04, 0x03}, "ABCDABCD"},
		{[]byte{0x80, 0x00, 0x03}, "ABCDABCD"},
		{[]byte{0xc0, 0x00, 0x03, 0x00}, "ABCDABCDA"},
	} {
		data := []byte{0x10, 0xfb, 0, 0, byte(len(test.want)), 0xe0, 'A', 'B', 'C', 'D'}
		data = append(append(data, test.command...), 0xfc)
		got, err := decodeRefPack(data)
		if err != nil || string(got) != test.want {
			t.Fatalf("RefPack %x: got %q, want %q, err %v", test.command, got, test.want, err)
		}
	}
}

func TestResourceRejectsTruncation(t *testing.T) {
	for name, data := range cartest.ResourceFiles() {
		format := strings.TrimPrefix(name[strings.LastIndex(name, "."):], ".")
		if !Supported(format) && format != "tga" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			for _, size := range []int{0, 3, len(data) / 2} {
				if err := Validate(format, data[:size], 0); err == nil {
					t.Errorf("accepted truncated %s of %d bytes", name, size)
				}
			}
		})
	}
}

func TestResourceProjectMissingAndExtraFiles(t *testing.T) {
	files, err := Extract("fce", cartest.ResourceFiles()["car.fce"])
	if err != nil {
		t.Fatal(err)
	}
	files["extra.bin"] = []byte{1}
	if _, err := Pack("fce", files); err == nil {
		t.Fatal("unreferenced file was ignored")
	}
	delete(files, "extra.bin")
	delete(files, "model.obj")
	if _, err := Pack("fce", files); err == nil {
		t.Fatal("missing geometry was ignored")
	}
}

func FuzzValidateResources(f *testing.F) {
	for name, data := range cartest.ResourceFiles() {
		format := strings.TrimPrefix(name[strings.LastIndex(name, "."):], ".")
		if Supported(format) || format == "tga" {
			f.Add(format, data)
		}
	}
	f.Fuzz(func(t *testing.T, format string, data []byte) {
		_ = Validate(format, data, 0)
	})
}
