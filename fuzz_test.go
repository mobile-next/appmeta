package appmeta

import (
	"bytes"
	"testing"
)

// Fuzz targets call the parsers directly, not through Parse, so a panic
// fails the fuzzer instead of being recovered into an error.

func FuzzAXML(f *testing.F) {
	f.Add(encodeAXML(acmeShopManifest()))
	f.Add(encodeAXMLStrings(acmeShopManifest(), true))
	f.Add(adaptiveIconXML())
	f.Fuzz(func(t *testing.T, data []byte) {
		_ = parseAXML(data, DefaultLimits().MaxDepth, func(path []string, attrs []xmlAttr) {
			for _, a := range attrs {
				_ = a.text()
			}
		})
		_, _ = parseManifest(data, DefaultLimits().MaxDepth)
	})
}

func FuzzResourceTable(f *testing.F) {
	for _, layout := range []resTableLayout{denseOffsets, sparseOffsets, offset16WithCompactEntries} {
		f.Add(encodeResourceTableLayout(acmeShopResources(), layout), resID("string", 0))
	}
	f.Fuzz(func(t *testing.T, data []byte, id uint32) {
		table, err := parseResourceTable(data)
		if err != nil {
			return
		}
		for _, ref := range []uint32{id, resID("string", 0), resID("mipmap", 0), resID("drawable", 0)} {
			_, _ = table.resolveText(ref)
			_ = table.resolveFiles(ref)
		}
	})
}

func FuzzPlist(f *testing.F) {
	f.Add(binaryPlist(f, acmeInfoPlist(nil)))
	f.Add(xmlPlist(f, acmeInfoPlist(nil)))
	f.Add(developmentProfile(f))
	f.Fuzz(func(t *testing.T, data []byte) {
		if dict, err := decodePlist(data, DefaultLimits().MaxDepth); err == nil {
			_ = deviceFamilies(dict)
			_ = iconBaseNames(dict)
		}
		_, _, _ = parseProvisioningProfile(data, DefaultLimits().MaxDepth)
	})
}

// FuzzIcon covers CgBI and the image decoders appmeta feeds untrusted data.
func FuzzIcon(f *testing.F) {
	f.Add(cgbiPNG(f, solidImage(8, 8, acmeRed)))
	f.Add(solidPNG(f, 8, 8, acmeGreen))
	f.Add(solidWebP(8, 8, acmeBlue))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = encodeIcon(data, 256*256)
	})
}

func FuzzMachO(f *testing.F) {
	f.Add(fatMachO(thinMachO(cpuX86_64, 3, platformIOSSimulator), thinMachO(cpuARM64, 0, platformIOSSimulator)))
	f.Add(thinMachO(cpuARM64, 2, platformIOS).data)
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = sniffMachO(data)
	})
}

func FuzzParse(f *testing.F) {
	f.Add(buildMinimalAPK(f))
	f.Add(buildAdaptiveIconAPK(f))
	f.Add(buildAcmeShopIPA(f))
	f.Add(buildSimulatorIPA(f))
	limits := Limits{MaxEntries: 1000, MaxEntrySize: 1 << 20, MaxTotalSize: 4 << 20, MaxDepth: 32, MaxIconPixels: 256 * 256}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = parse(bytes.NewReader(data), int64(len(data)), limits)
	})
}
