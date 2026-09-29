package appmeta

import "testing"

func buildAPKWithManifest(t testing.TB, manifest xmlNode, extra ...zipEntry) []byte {
	t.Helper()
	entries := append([]zipEntry{{name: androidManifestPath, data: encodeAXML(manifest)}}, extra...)
	return buildZip(t, entries...)
}

func manifestElement(packageName string, applicationAttrs []xmlTestAttr, children ...xmlNode) xmlNode {
	all := append([]xmlNode{
		element("uses-sdk", attrs(
			androidInt("minSdkVersion", attrMinSDKVersion, 26),
			androidInt("targetSdkVersion", attrTargetSDKVersion, 35),
		)),
	}, children...)
	all = append(all, element("application", applicationAttrs))
	return element("manifest", attrs(
		androidInt("versionCode", attrVersionCode, 4201),
		androidString("versionName", attrVersionName, "4.2.0"),
		plainString("package", packageName),
	), all...)
}

func usesPermission(name string) xmlNode {
	return element("uses-permission", attrs(androidString("name", attrName, name)))
}

func usesFeature(name string, required bool) xmlNode {
	return element("uses-feature", attrs(
		androidString("name", attrName, name),
		androidBool("required", attrRequired, required),
	))
}

func buildMinimalAPK(t testing.TB) []byte {
	return buildAPKWithManifest(t, manifestElement("com.acme.minimal", attrs(
		androidString("label", attrLabel, "Minimal"),
	)))
}

const (
	densityMdpi    = 160
	densityXxxhdpi = 640
)

func acmeShopResources() []resEntry {
	return []resEntry{
		{typeName: "string", entry: 0, values: []resTestValue{
			stringValue("", "Acme Shop"),
			stringValue("fr", "Boutique Acme"),
		}},
		{typeName: "mipmap", entry: 0, values: []resTestValue{
			fileAtDensity(densityMdpi, "res/mipmap-mdpi/ic_launcher.png"),
			fileAtDensity(densityXxxhdpi, "res/mipmap-xxxhdpi/ic_launcher.webp"),
			fileAtDensity(densityAny, "res/mipmap-anydpi-v26/ic_launcher.xml"),
		}},
		{typeName: "drawable", entry: 0, values: []resTestValue{
			fileAtDensity(0, "res/drawable/ic_launcher_foreground.png"),
		}},
	}
}

func adaptiveIconXML() []byte {
	return encodeAXML(element("adaptive-icon", nil,
		element("background", attrs(androidReference("drawable", attrDrawable, resID("drawable", 1)))),
		element("foreground", attrs(androidReference("drawable", attrDrawable, resID("drawable", 0)))),
	))
}

func acmeShopManifest() xmlNode {
	return manifestElement("com.acme.shop", attrs(
		androidReference("label", attrLabel, resID("string", 0)),
		androidReference("icon", attrIcon, resID("mipmap", 0)),
	),
		usesPermission("android.permission.CAMERA"),
		usesPermission("android.permission.INTERNET"),
	)
}

// buildAcmeShopAPK is a typical release APK: resource label, icons at two
// densities plus an adaptive icon, permissions and two ABIs.
func buildAcmeShopAPK(t testing.TB) []byte {
	return buildAPKWithManifest(t, acmeShopManifest(),
		zipEntry{name: androidResourcesPath, data: encodeResourceTable(acmeShopResources())},
		zipEntry{name: "res/mipmap-mdpi/ic_launcher.png", data: solidPNG(t, 48, 48, acmeGreen)},
		zipEntry{name: "res/mipmap-xxxhdpi/ic_launcher.webp", data: solidWebP(192, 192, acmeRed)},
		zipEntry{name: "res/mipmap-anydpi-v26/ic_launcher.xml", data: adaptiveIconXML()},
		zipEntry{name: "res/drawable/ic_launcher_foreground.png", data: solidPNG(t, 108, 108, acmeBlue)},
		zipEntry{name: "lib/arm64-v8a/libacme.so", data: []byte("elf")},
		zipEntry{name: "lib/armeabi-v7a/libacme.so", data: []byte("elf")},
	)
}

// buildAdaptiveIconAPK has only an adaptive icon, as apps with minSdk 26
// often do.
func buildAdaptiveIconAPK(t testing.TB) []byte {
	resources := []resEntry{
		{typeName: "mipmap", entry: 0, values: []resTestValue{
			fileAtDensity(densityAny, "res/mipmap-anydpi-v26/ic_launcher.xml"),
		}},
		{typeName: "drawable", entry: 0, values: []resTestValue{
			fileAtDensity(0, "res/drawable/ic_launcher_foreground.png"),
		}},
	}
	manifest := manifestElement("com.acme.adaptive", attrs(
		androidString("label", attrLabel, "Adaptive"),
		androidReference("icon", attrIcon, resID("mipmap", 0)),
	))
	return buildAPKWithManifest(t, manifest,
		zipEntry{name: androidResourcesPath, data: encodeResourceTable(resources)},
		zipEntry{name: "res/mipmap-anydpi-v26/ic_launcher.xml", data: adaptiveIconXML()},
		zipEntry{name: "res/drawable/ic_launcher_foreground.png", data: solidPNG(t, 108, 108, acmeBlue)},
	)
}

func buildAPKWithIconFile(t testing.TB, path string, data []byte) []byte {
	resources := []resEntry{{typeName: "mipmap", entry: 0, values: []resTestValue{fileAtDensity(0, path)}}}
	manifest := manifestElement("com.acme.icon", attrs(androidReference("icon", attrIcon, resID("mipmap", 0))))
	return buildAPKWithManifest(t, manifest,
		zipEntry{name: androidResourcesPath, data: encodeResourceTable(resources)},
		zipEntry{name: path, data: data},
	)
}
