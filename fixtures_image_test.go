package appmeta

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

var (
	acmeRed   = color.NRGBA{R: 220, G: 30, B: 40, A: 255}
	acmeGreen = color.NRGBA{R: 20, G: 200, B: 60, A: 255}
	acmeBlue  = color.NRGBA{R: 10, G: 60, B: 230, A: 255}
)

func solidImage(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func solidPNG(t testing.TB, w, h int, c color.NRGBA) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, solidImage(w, h, c)); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// solidWebP writes a lossless WebP whose five prefix codes each have a
// single symbol, so every pixel is the same colour and takes zero bits.
func solidWebP(w, h int, c color.NRGBA) []byte {
	var bits webpBitWriter
	bits.write(uint32(w-1), 14)
	bits.write(uint32(h-1), 14)
	bits.write(1, 1) // alpha is used
	bits.write(0, 3) // version
	bits.write(0, 1) // no transform
	bits.write(0, 1) // no colour cache
	bits.write(0, 1) // no meta prefix codes
	for _, symbol := range []uint8{c.G, c.R, c.B, c.A, 0} {
		bits.write(1, 1) // simple code
		bits.write(0, 1) // one symbol
		bits.write(1, 1) // 8-bit symbol
		bits.write(uint32(symbol), 8)
	}
	payload := append([]byte{0x2f}, bits.bytes()...)
	if len(payload)%2 == 1 {
		payload = append(payload, 0)
	}

	var out bytes.Buffer
	out.WriteString("RIFF")
	out.Write(u32s(uint32(4 + 8 + len(payload))))
	out.WriteString("WEBPVP8L")
	out.Write(u32s(uint32(len(payload))))
	out.Write(payload)
	return out.Bytes()
}

// webpBitWriter packs bits least-significant first, as VP8L reads them.
type webpBitWriter struct {
	buf   []byte
	acc   uint64
	count uint
}

func (b *webpBitWriter) write(v uint32, n uint) {
	b.acc |= uint64(v) << b.count
	b.count += n
	for b.count >= 8 {
		b.buf = append(b.buf, byte(b.acc))
		b.acc >>= 8
		b.count -= 8
	}
}

func (b *webpBitWriter) bytes() []byte {
	if b.count > 0 {
		return append(b.buf, byte(b.acc))
	}
	return b.buf
}

// iconCenterColor decodes the extracted PNG and samples its middle pixel.
func iconCenterColor(t testing.TB, icon *Icon) color.NRGBA {
	t.Helper()
	if icon == nil {
		t.Fatal("no icon extracted")
	}
	img, err := png.Decode(bytes.NewReader(icon.PNG))
	if err != nil {
		t.Fatalf("icon is not a valid PNG: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != icon.Width || b.Dy() != icon.Height {
		t.Fatalf("icon PNG is %dx%d but reported %dx%d", b.Dx(), b.Dy(), icon.Width, icon.Height)
	}
	return color.NRGBAModel.Convert(img.At(b.Dx()/2, b.Dy()/2)).(color.NRGBA)
}
