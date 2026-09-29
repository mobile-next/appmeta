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
