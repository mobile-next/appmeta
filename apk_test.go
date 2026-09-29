package appmeta

import (
	"errors"
	"slices"
	"testing"
)

func TestAnAPKManifestYieldsPackageVersionAndOSReleases(t *testing.T) {
	info := mustParse(t, buildMinimalAPK(t))
	assertEqual(t, "format", info.Format, "apk")
	assertEqual(t, "platform", info.Platform, "android")
	assertEqual(t, "bundleId", info.BundleID, "com.acme.minimal")
	assertEqual(t, "version", info.Version, "4.2.0")
	assertEqual(t, "buildNumber", info.BuildNumber, "4201")
	assertEqual(t, "minOsVersion", info.MinOSVersion, "8.0")
	assertEqual(t, "targetOsVersion", info.TargetOSVersion, "15")
	assertEqual(t, "name", info.Name, "Minimal")
}

func TestAnAPKWithoutALabelFallsBackToThePackageName(t *testing.T) {
	info := mustParse(t, buildAPKWithManifest(t, manifestElement("com.acme.nolabel", nil)))
	assertEqual(t, "name", info.Name, "com.acme.nolabel")
}

func TestADebuggableAPKIsReportedAsDebuggable(t *testing.T) {
	manifest := manifestElement("com.acme.debug", attrs(androidBool("debuggable", attrDebuggable, true)))
	info := mustParse(t, buildAPKWithManifest(t, manifest))
	if !info.IsDebuggable {
		t.Fatal("want isDebuggable")
	}
}

func TestPermissionsAreListedOnceInManifestOrder(t *testing.T) {
	manifest := manifestElement("com.acme.perms", nil,
		usesPermission("android.permission.INTERNET"),
		usesPermission("android.permission.CAMERA"),
		usesPermission("android.permission.INTERNET"),
	)
	info := mustParse(t, buildAPKWithManifest(t, manifest))
	assertSlice(t, "permissions", info.Permissions, []string{"android.permission.INTERNET", "android.permission.CAMERA"})
}

func TestNativeLibraryDirectoriesBecomeArchitectures(t *testing.T) {
	data := buildAPKWithManifest(t, manifestElement("com.acme.native", nil),
		zipEntry{name: "lib/armeabi-v7a/libacme.so", data: []byte("elf")},
		zipEntry{name: "lib/arm64-v8a/libacme.so", data: []byte("elf")},
		zipEntry{name: "lib/arm64-v8a/libother.so", data: []byte("elf")},
		zipEntry{name: "lib/x86/", data: nil},
	)
	info := mustParse(t, data)
	assertSlice(t, "architectures", info.Architectures, []string{"arm64-v8a", "armeabi-v7a"})
}

func TestAnAPKWithoutNativeLibrariesHasNoArchitectures(t *testing.T) {
	info := mustParse(t, buildMinimalAPK(t))
	assertSlice(t, "architectures", info.Architectures, []string{})
}

func TestAnUnknownFutureAPILevelIsReportedAsAnAPILevel(t *testing.T) {
	manifest := element("manifest", attrs(plainString("package", "com.acme.future")),
		element("uses-sdk", attrs(androidInt("minSdkVersion", attrMinSDKVersion, 99))))
	info := mustParse(t, buildAPKWithManifest(t, manifest))
	assertEqual(t, "minOsVersion", info.MinOSVersion, "API 99")
}

func TestAPreviewCodenameSDKIsPassedThrough(t *testing.T) {
	manifest := element("manifest", attrs(plainString("package", "com.acme.preview")),
		element("uses-sdk", attrs(androidString("targetSdkVersion", attrTargetSDKVersion, "Baklava"))))
	info := mustParse(t, buildAPKWithManifest(t, manifest))
	assertEqual(t, "targetOsVersion", info.TargetOSVersion, "Baklava")
}

func TestObfuscatedAttributeNamesAreFoundByResourceID(t *testing.T) {
	manifest := element("manifest", attrs(
		plainString("package", "com.acme.obfuscated"),
		androidString("", attrVersionName, "9.9"),
		androidInt("x", attrVersionCode, 99),
	))
	info := mustParse(t, buildAPKWithManifest(t, manifest))
	assertEqual(t, "version", info.Version, "9.9")
	assertEqual(t, "buildNumber", info.BuildNumber, "99")
}

func TestUTF8StringPoolsDecodeLikeUTF16Ones(t *testing.T) {
	manifest := manifestElement("com.acme.utf8", attrs(androidString("label", attrLabel, "Café ☕")))
	data := buildZip(t, zipEntry{name: androidManifestPath, data: encodeAXMLStrings(manifest, true)})
	info := mustParse(t, data)
	assertEqual(t, "name", info.Name, "Café ☕")
}

func TestDeviceFamiliesFollowRequiredHardwareFeatures(t *testing.T) {
	cases := map[string]struct {
		features []xmlNode
		want     []string
	}{
		"phone app":       {nil, []string{"phone", "tablet"}},
		"watch app":       {[]xmlNode{usesFeature("android.hardware.type.watch", true)}, []string{"watch"}},
		"tv-only app":     {[]xmlNode{usesFeature("android.software.leanback", true)}, []string{"tv"}},
		"app that has tv": {[]xmlNode{usesFeature("android.software.leanback", false)}, []string{"phone", "tablet", "tv"}},
		"car app":         {[]xmlNode{usesFeature("android.hardware.type.automotive", true)}, []string{"car"}},
		"optional watch":  {[]xmlNode{usesFeature("android.hardware.type.watch", false)}, []string{"phone", "tablet"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			info := mustParse(t, buildAPKWithManifest(t, manifestElement("com.acme.devices", nil, c.features...)))
			assertSlice(t, "deviceFamilies", info.DeviceFamilies, c.want)
		})
	}
}

func TestAManifestWithoutAPackageNameIsAnError(t *testing.T) {
	_, err := parseBytes(buildAPKWithManifest(t, element("manifest", nil)))
	if err == nil {
		t.Fatal("want an error")
	}
}

func TestAManifestNestedDeeperThanTheLimitIsRejected(t *testing.T) {
	deep := element("leaf", nil)
	for range 10 {
		deep = element("wrapper", nil, deep)
	}
	manifest := element("manifest", attrs(plainString("package", "com.acme.deep")), deep)
	_, err := parseBytes(buildAPKWithManifest(t, manifest), WithLimits(Limits{MaxDepth: 5}))
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want ErrLimitExceeded", err)
	}
}

func TestATruncatedManifestIsAnError(t *testing.T) {
	manifest := encodeAXML(manifestElement("com.acme.truncated", nil))
	data := buildZip(t, zipEntry{name: androidManifestPath, data: manifest[:len(manifest)/2]})
	if _, err := parseBytes(data); err == nil {
		t.Fatal("want an error")
	}
}

func assertEqual[T comparable](t *testing.T, field string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", field, got, want)
	}
}

func assertSlice(t *testing.T, field string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s: got %q, want %q", field, got, want)
	}
}
