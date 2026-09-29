package appmeta

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
)

// Apple's CgBI PNG variant (Xcode's "pngcrush -iphone") differs from PNG in
// three ways: a CgBI chunk comes first, IDAT holds raw deflate without a zlib
// header, and pixels are premultiplied BGRA. Go's image/png rejects it.

const (
	pngChunkOverhead = 12
	pngIHDRLen       = 13
	cgbiBytesPerPx   = 4
	pngColorRGBA     = 6
)

var errMalformedCgBI = errors.New("appmeta: malformed CgBI png")

func isCgBI(data []byte) bool {
	rest := data[len(pngSignature):]
	return len(rest) >= 8 && string(rest[4:8]) == "CgBI"
}

type cgbiImage struct {
	width, height int
	idat          []byte
}

func parseCgBI(data []byte) (cgbiImage, error) {
	var img cgbiImage
	var ihdr []byte
	var idat [][]byte
	rest := data[len(pngSignature):]
	for len(rest) >= pngChunkOverhead {
		n := uint64(binary.BigEndian.Uint32(rest))
		if n > uint64(len(rest)-pngChunkOverhead) {
			return img, fmt.Errorf("%w: chunk overflows", errMalformedCgBI)
		}
		typ, body := string(rest[4:8]), rest[8:8+n]
		rest = rest[pngChunkOverhead+n:]
		switch typ {
		case "IHDR":
			ihdr = body
		case "IDAT":
			idat = append(idat, body)
		case "IEND":
			rest = nil
		}
	}
	if len(ihdr) != pngIHDRLen {
		return img, fmt.Errorf("%w: missing IHDR", errMalformedCgBI)
	}
	bitDepth, colorType, interlace := ihdr[8], ihdr[9], ihdr[12]
	if bitDepth != 8 || colorType != pngColorRGBA || interlace != 0 {
		return img, fmt.Errorf("%w: unsupported layout (depth %d, color %d, interlace %d)", errMalformedCgBI, bitDepth, colorType, interlace)
	}
	img.width = int(binary.BigEndian.Uint32(ihdr))
	img.height = int(binary.BigEndian.Uint32(ihdr[4:]))
	img.idat = bytes.Join(idat, nil)
	return img, nil
}

func decodeCgBIConfig(data []byte) (image.Config, error) {
	img, err := parseCgBI(data)
	if err != nil {
		return image.Config{}, err
	}
	return image.Config{ColorModel: color.NRGBAModel, Width: img.width, Height: img.height}, nil
}

// decodeCgBI must only be called after the pixel count was checked against
// the limits, since it allocates width*height*4 bytes.
func decodeCgBI(data []byte) (image.Image, error) {
	src, err := parseCgBI(data)
	if err != nil {
		return nil, err
	}
	stride := src.width * cgbiBytesPerPx
	want := int64(src.height) * int64(stride+1)
	raw, err := io.ReadAll(io.LimitReader(flate.NewReader(bytes.NewReader(src.idat)), want+1))
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, fmt.Errorf("%w: %v", errMalformedCgBI, err)
	}
	if int64(len(raw)) < want {
		return nil, fmt.Errorf("%w: short pixel data", errMalformedCgBI)
	}

	img := image.NewNRGBA(image.Rect(0, 0, src.width, src.height))
	prev := make([]byte, stride)
	for y := range src.height {
		row := raw[y*(stride+1) : (y+1)*(stride+1)]
		if err := unfilterRow(row[0], row[1:], prev); err != nil {
			return nil, err
		}
		out := img.Pix[y*img.Stride:]
		for x := 0; x < stride; x += cgbiBytesPerPx {
			b, g, r, a := row[1+x], row[2+x], row[3+x], row[4+x]
			out[x], out[x+1], out[x+2], out[x+3] = unpremultiply(r, a), unpremultiply(g, a), unpremultiply(b, a), a
		}
		prev = row[1:]
	}
	return img, nil
}

func unpremultiply(c, a uint8) uint8 {
	if a == 0 {
		return 0
	}
	return uint8(min(255, (uint32(c)*255+uint32(a)/2)/uint32(a)))
}

// unfilterRow reverses a PNG row filter in place (PNG spec section 9).
func unfilterRow(filter byte, row, prev []byte) error {
	const bpp = cgbiBytesPerPx
	switch filter {
	case 0:
	case 1:
		for i := bpp; i < len(row); i++ {
			row[i] += row[i-bpp]
		}
	case 2:
		for i := range row {
			row[i] += prev[i]
		}
	case 3:
		for i := range row {
			var left byte
			if i >= bpp {
				left = row[i-bpp]
			}
			row[i] += byte((uint16(left) + uint16(prev[i])) / 2)
		}
	case 4:
		for i := range row {
			var left, upLeft byte
			if i >= bpp {
				left, upLeft = row[i-bpp], prev[i-bpp]
			}
			row[i] += paeth(left, prev[i], upLeft)
		}
	default:
		return fmt.Errorf("%w: unknown filter %d", errMalformedCgBI, filter)
	}
	return nil
}

func paeth(a, b, c byte) byte {
	p := int(a) + int(b) - int(c)
	pa, pb, pc := abs(p-int(a)), abs(p-int(b)), abs(p-int(c))
	switch {
	case pa <= pb && pa <= pc:
		return a
	case pb <= pc:
		return b
	}
	return c
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
