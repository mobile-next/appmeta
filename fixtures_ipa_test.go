package appmeta

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"io"
	"maps"
	"testing"

	"howett.net/plist"
)

const acmeBundle = "Payload/AcmeShop.app/"

func acmeInfoPlist(overrides map[string]any) map[string]any {
	dict := map[string]any{
		"CFBundleIdentifier":         "com.acme.shop",
		"CFBundleDisplayName":        "Acme Shop",
		"CFBundleName":               "AcmeShop",
		"CFBundleShortVersionString": "4.2.0",
		"CFBundleVersion":            "4201",
		"CFBundleExecutable":         "AcmeShop",
		"MinimumOSVersion":           "15.0",
		"DTPlatformName":             "iphoneos",
		"DTPlatformVersion":          "17.2",
		"UIDeviceFamily":             []int{1, 2},
		"CFBundleIcons": map[string]any{
			"CFBundlePrimaryIcon": map[string]any{
				"CFBundleIconFiles": []string{"AppIcon60x60"},
				"CFBundleIconName":  "AppIcon",
			},
		},
	}
	maps.Copy(dict, overrides)
	for k, v := range overrides {
		if v == nil {
			delete(dict, k)
		}
	}
	return dict
}

func binaryPlist(t testing.TB, v any) []byte {
	t.Helper()
	data, err := plist.Marshal(v, plist.BinaryFormat)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func xmlPlist(t testing.TB, v any) []byte {
	t.Helper()
	data, err := plist.MarshalIndent(v, plist.XMLFormat, "\t")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func buildIPA(t testing.TB, infoPlist []byte, extra ...zipEntry) []byte {
	t.Helper()
	entries := []zipEntry{{name: acmeBundle + "Info.plist", data: infoPlist}}
	for _, e := range extra {
		entries = append(entries, zipEntry{name: acmeBundle + e.name, data: e.data})
	}
	return buildZip(t, entries...)
}

// cgbiPNG encodes img the way Xcode does for device builds: premultiplied
// BGRA pixels, raw deflate, and a leading CgBI chunk. Go's PNG encoder does
// the row filtering; filters work per byte within a pixel, so swapping
// channels before filtering is equivalent to swapping after.
func cgbiPNG(t testing.TB, img *image.NRGBA) []byte {
	t.Helper()
	crushed := image.NewNRGBA(img.Bounds())
	for i := 0; i < len(img.Pix); i += 4 {
		r, g, b, a := img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]
		premultiply := func(c uint8) uint8 { return uint8((uint32(c)*uint32(a) + 127) / 255) }
		copy(crushed.Pix[i:], []byte{premultiply(b), premultiply(g), premultiply(r), a})
	}
	// Go writes RGB for opaque images; CgBI is always RGBA.
	crushed.Pix[3] = 0
	crushed.Pix[0], crushed.Pix[1], crushed.Pix[2] = 0, 0, 0

	var standard bytes.Buffer
	if err := png.Encode(&standard, crushed); err != nil {
		t.Fatal(err)
	}
	ihdr, idat := pngChunks(t, standard.Bytes())
	zr, err := zlib.NewReader(bytes.NewReader(idat))
	if err != nil {
		t.Fatal(err)
	}
	filtered, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	var raw bytes.Buffer
	fw, _ := flate.NewWriter(&raw, flate.BestCompression)
	_, _ = fw.Write(filtered)
	_ = fw.Close()

	var out bytes.Buffer
	out.Write(pngSignature)
	writePNGChunk(&out, "CgBI", []byte{0x50, 0x00, 0x20, 0x06})
	writePNGChunk(&out, "IHDR", ihdr)
	writePNGChunk(&out, "IDAT", raw.Bytes())
	writePNGChunk(&out, "IEND", nil)
	return out.Bytes()
}

func pngChunks(t testing.TB, data []byte) (ihdr, idat []byte) {
	t.Helper()
	rest := data[len(pngSignature):]
	for len(rest) >= 12 {
		n := binary.BigEndian.Uint32(rest)
		typ, body := string(rest[4:8]), rest[8:8+n]
		switch typ {
		case "IHDR":
			ihdr = body
		case "IDAT":
			idat = append(idat, body...)
		}
		rest = rest[12+n:]
	}
	return ihdr, idat
}

func writePNGChunk(w *bytes.Buffer, typ string, body []byte) {
	_ = binary.Write(w, binary.BigEndian, uint32(len(body)))
	w.WriteString(typ)
	w.Write(body)
	crc := crc32.NewIEEE()
	crc.Write([]byte(typ))
	crc.Write(body)
	_ = binary.Write(w, binary.BigEndian, crc.Sum32())
}

func acmeDeviceExecutable() zipEntry {
	return zipEntry{name: "AcmeShop", data: fatMachO(thinMachO(cpuARM64, 0, platformIOS))}
}

// buildAcmeShopIPA is a typical development build: arm64 executable,
// development profile and CgBI icons at two scales.
func buildAcmeShopIPA(t testing.TB) []byte {
	return buildIPA(t, binaryPlist(t, acmeInfoPlist(nil)),
		acmeDeviceExecutable(),
		zipEntry{name: "embedded.mobileprovision", data: developmentProfile(t)},
		zipEntry{name: "AppIcon60x60@2x.png", data: cgbiPNG(t, solidImage(120, 120, acmeGreen))},
		zipEntry{name: "AppIcon60x60@3x.png", data: cgbiPNG(t, solidImage(180, 180, acmeRed))},
		zipEntry{name: "Assets.car", data: []byte("car")},
	)
}

// buildSimulatorIPA is a zipped simulator build: XML plist, universal
// simulator executable, no profile, icon only in Assets.car.
func buildSimulatorIPA(t testing.TB) []byte {
	plist := acmeInfoPlist(map[string]any{"DTPlatformName": "iphonesimulator", "UIDeviceFamily": []int{1}})
	return buildIPA(t, xmlPlist(t, plist),
		zipEntry{name: "AcmeShop", data: fatMachO(
			thinMachO(cpuX86_64, 3, platformIOSSimulator),
			thinMachO(cpuARM64, 0, platformIOSSimulator),
		)},
		zipEntry{name: "Assets.car", data: []byte("car")},
	)
}
