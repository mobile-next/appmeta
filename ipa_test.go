package appmeta

import (
	"errors"
	"image"
	"image/color"
	"testing"
)

func TestAnIPAInfoPlistYieldsBundleVersionAndOS(t *testing.T) {
	info := mustParse(t, buildAcmeShopIPA(t))
	assertEqual(t, "format", info.Format, "ipa")
	assertEqual(t, "platform", info.Platform, "ios")
	assertEqual(t, "bundleId", info.BundleID, "com.acme.shop")
	assertEqual(t, "name", info.Name, "Acme Shop")
	assertEqual(t, "version", info.Version, "4.2.0")
	assertEqual(t, "buildNumber", info.BuildNumber, "4201")
	assertEqual(t, "minOsVersion", info.MinOSVersion, "15.0")
	assertEqual(t, "targetOsVersion", info.TargetOSVersion, "17.2")
	assertSlice(t, "deviceFamilies", info.DeviceFamilies, []string{"phone", "tablet"})
}

func TestXMLInfoPlistsParseLikeBinaryOnes(t *testing.T) {
	info := mustParse(t, buildIPA(t, xmlPlist(t, acmeInfoPlist(nil))))
	assertEqual(t, "bundleId", info.BundleID, "com.acme.shop")
	assertEqual(t, "version", info.Version, "4.2.0")
}

func TestTheNameFallsBackFromDisplayNameToBundleNameToBundleID(t *testing.T) {
	info := mustParse(t, buildIPA(t, binaryPlist(t, acmeInfoPlist(map[string]any{"CFBundleDisplayName": nil}))))
	assertEqual(t, "name", info.Name, "AcmeShop")

	info = mustParse(t, buildIPA(t, binaryPlist(t, acmeInfoPlist(map[string]any{"CFBundleDisplayName": nil, "CFBundleName": nil}))))
	assertEqual(t, "name", info.Name, "com.acme.shop")
}

func TestANumericBundleVersionIsReportedAsText(t *testing.T) {
	info := mustParse(t, buildIPA(t, binaryPlist(t, acmeInfoPlist(map[string]any{"CFBundleVersion": 77}))))
	assertEqual(t, "buildNumber", info.BuildNumber, "77")
}

func TestDeviceFamiliesComeFromUIDeviceFamilyAndDefaultToPhone(t *testing.T) {
	info := mustParse(t, buildIPA(t, binaryPlist(t, acmeInfoPlist(map[string]any{"UIDeviceFamily": []int{3}}))))
	assertSlice(t, "deviceFamilies", info.DeviceFamilies, []string{"tv"})

	info = mustParse(t, buildIPA(t, binaryPlist(t, acmeInfoPlist(map[string]any{"UIDeviceFamily": nil}))))
	assertSlice(t, "deviceFamilies", info.DeviceFamilies, []string{"phone"})
}

func TestTheLargestDeclaredIconIsChosenAndItsCgBIEncodingIsUndone(t *testing.T) {
	info := mustParse(t, buildAcmeShopIPA(t))
	assertEqual(t, "icon width", info.Icon.Width, 180)
	assertEqual(t, "icon color", iconCenterColor(t, info.Icon), acmeRed)
	if len(info.Warnings) != 0 {
		t.Errorf("want no warnings, got %q", info.Warnings)
	}
}

func TestCgBIDecodingReproducesEveryPixelOfAGradient(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 37, 23))
	for y := range 23 {
		for x := range 37 {
			src.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 7), G: uint8(y * 11), B: uint8(x*y + 3), A: 255})
		}
	}
	decoded, err := decodeCgBI(cgbiPNG(t, src))
	if err != nil {
		t.Fatal(err)
	}
	for y := range 23 {
		for x := range 37 {
			if x == 0 && y == 0 {
				continue // made transparent by cgbiPNG
			}
			got := color.NRGBAModel.Convert(decoded.At(x, y))
			if got != src.NRGBAAt(x, y) {
				t.Fatalf("pixel %d,%d: got %v, want %v", x, y, got, src.NRGBAAt(x, y))
			}
		}
	}
}

func TestCgBIPremultipliedAlphaIsUndone(t *testing.T) {
	translucent := color.NRGBA{R: 200, G: 100, B: 50, A: 128}
	decoded, err := decodeCgBI(cgbiPNG(t, solidImage(4, 4, translucent)))
	if err != nil {
		t.Fatal(err)
	}
	got := color.NRGBAModel.Convert(decoded.At(2, 2)).(color.NRGBA)
	if absDiff(got.R, translucent.R) > 1 || absDiff(got.G, translucent.G) > 1 || absDiff(got.B, translucent.B) > 1 || got.A != translucent.A {
		t.Fatalf("got %v, want about %v", got, translucent)
	}
}

func TestAnIconOnlyInAssetsCarLeavesTheIconEmptyWithAWarning(t *testing.T) {
	info := mustParse(t, buildIPA(t, binaryPlist(t, acmeInfoPlist(nil)), zipEntry{name: "Assets.car", data: []byte("car")}))
	if info.Icon != nil {
		t.Fatal("want no icon")
	}
	assertWarningMentions(t, info, "Assets.car")
}

func TestLegacyIconFilesAreFound(t *testing.T) {
	plist := acmeInfoPlist(map[string]any{"CFBundleIcons": nil, "CFBundleIconFiles": []string{"Icon.png"}})
	info := mustParse(t, buildIPA(t, binaryPlist(t, plist),
		zipEntry{name: "Icon.png", data: solidPNG(t, 57, 57, acmeGreen)},
		zipEntry{name: "Icon@2x.png", data: solidPNG(t, 114, 114, acmeBlue)},
		zipEntry{name: "IconUnrelated.png", data: solidPNG(t, 300, 300, acmeRed)},
	))
	assertEqual(t, "icon width", info.Icon.Width, 114)
	assertEqual(t, "icon color", iconCenterColor(t, info.Icon), acmeBlue)
}

func TestAnIPAWithoutABundleIdentifierIsAnError(t *testing.T) {
	_, err := parseBytes(buildIPA(t, binaryPlist(t, acmeInfoPlist(map[string]any{"CFBundleIdentifier": nil}))))
	if err == nil {
		t.Fatal("want an error")
	}
}

func TestTheMainAppIsChosenOverNestedAppBundles(t *testing.T) {
	watch := binaryPlist(t, map[string]any{"CFBundleIdentifier": "com.acme.shop.watch"})
	info := mustParse(t, buildIPA(t, binaryPlist(t, acmeInfoPlist(nil)),
		zipEntry{name: "Watch/AcmeWatch.app/Info.plist", data: watch}))
	assertEqual(t, "bundleId", info.BundleID, "com.acme.shop")
}

func TestPlistsNestedDeeperThanTheLimitAreRejected(t *testing.T) {
	var deep any = "leaf"
	for range 10 {
		deep = []any{deep}
	}
	plist := acmeInfoPlist(map[string]any{"Deep": deep})
	for name, encoded := range map[string][]byte{"binary": binaryPlist(t, plist), "xml": xmlPlist(t, plist)} {
		t.Run(name, func(t *testing.T) {
			_, err := parseBytes(buildIPA(t, encoded), WithLimits(Limits{MaxDepth: 6}))
			if !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("got %v, want ErrLimitExceeded", err)
			}
		})
	}
}

func TestATextPlistWithHostileNestingIsRejectedBeforeDecoding(t *testing.T) {
	hostile := []byte("<a/> = " + string(repeatByte('(', 100000)) + ";")
	_, err := parseBytes(buildIPA(t, hostile))
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want ErrLimitExceeded", err)
	}
}

func repeatByte(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

func absDiff(a, b uint8) uint8 {
	if a > b {
		return a - b
	}
	return b - a
}
