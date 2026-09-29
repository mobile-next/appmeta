package appmeta

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"

	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

var (
	pngSignature  = []byte("\x89PNG\r\n\x1a\n")
	jpegSignature = []byte{0xFF, 0xD8, 0xFF}
	riffSignature = []byte("RIFF")
	webpSignature = []byte("WEBP")
)

var errUnknownImageFormat = errors.New("appmeta: unknown image format")

type imageCodec struct {
	decode       func([]byte) (image.Image, error)
	decodeConfig func([]byte) (image.Config, error)
}

// codecFor sniffs the format. Decoders are called directly rather than
// registered with package image, so the host's image registry is untouched.
func codecFor(data []byte) (imageCodec, error) {
	switch {
	case bytes.HasPrefix(data, pngSignature):
		return imageCodec{
			decode:       func(b []byte) (image.Image, error) { return png.Decode(bytes.NewReader(b)) },
			decodeConfig: func(b []byte) (image.Config, error) { return png.DecodeConfig(bytes.NewReader(b)) },
		}, nil
	case bytes.HasPrefix(data, jpegSignature):
		return imageCodec{
			decode:       func(b []byte) (image.Image, error) { return jpeg.Decode(bytes.NewReader(b)) },
			decodeConfig: func(b []byte) (image.Config, error) { return jpeg.DecodeConfig(bytes.NewReader(b)) },
		}, nil
	case len(data) >= 12 && bytes.HasPrefix(data, riffSignature) && bytes.Equal(data[8:12], webpSignature):
		return imageCodec{
			decode:       func(b []byte) (image.Image, error) { return webp.Decode(bytes.NewReader(b)) },
			decodeConfig: func(b []byte) (image.Config, error) { return webp.DecodeConfig(bytes.NewReader(b)) },
		}, nil
	}
	return imageCodec{}, errUnknownImageFormat
}

// imageSize reads only the image header.
func imageSize(data []byte) (int, int, error) {
	codec, err := codecFor(data)
	if err != nil {
		return 0, 0, err
	}
	cfg, err := codec.decodeConfig(data)
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}

// encodeIcon decodes a PNG, JPEG or WebP and re-encodes
// it as a PNG no larger than MaxIconSize, so the host only ever serves
// images appmeta produced.
func encodeIcon(data []byte, maxPixels int) (*Icon, error) {
	codec, err := codecFor(data)
	if err != nil {
		return nil, err
	}
	cfg, err := codec.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxPixels/cfg.Height {
		return nil, fmt.Errorf("%w: icon is %dx%d", ErrLimitExceeded, cfg.Width, cfg.Height)
	}
	img, err := codec.decode(data)
	if err != nil {
		return nil, err
	}
	img = fitWithin(img, MaxIconSize)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(buf.Bytes())
	return &Icon{
		ContentType: "image/png",
		Width:       img.Bounds().Dx(),
		Height:      img.Bounds().Dy(),
		SHA256:      hex.EncodeToString(sum[:]),
		PNG:         buf.Bytes(),
	}, nil
}

func fitWithin(img image.Image, side int) image.Image {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if w <= side && h <= side {
		return img
	}
	if w >= h {
		h = max(1, h*side/w)
		w = side
	} else {
		w = max(1, w*side/h)
		h = side
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Src, nil)
	return dst
}
