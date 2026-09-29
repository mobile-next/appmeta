package appmeta

import (
	"strings"
	"testing"
)

func TestTheLabelResolvesToTheDefaultLocaleString(t *testing.T) {
	info := mustParse(t, buildAcmeShopAPK(t))
	assertEqual(t, "name", info.Name, "Acme Shop")
}

func TestTheLabelFallsBackToEnglishWhenThereIsNoDefaultLocale(t *testing.T) {
	resources := []resEntry{{typeName: "string", entry: 0, values: []resTestValue{
		stringValue("fr", "Bonjour"),
		stringValue("en", "Hello"),
	}}}
	info := mustParse(t, buildAPKWithLabelResources(t, resources, denseOffsets))
	assertEqual(t, "name", info.Name, "Hello")
}

func TestALabelThatReferencesAnotherStringResolves(t *testing.T) {
	resources := []resEntry{
		{typeName: "string", entry: 0, values: []resTestValue{referenceValue(resID("string", 1))}},
		{typeName: "string", entry: 1, values: []resTestValue{stringValue("", "Indirect")}},
	}
	info := mustParse(t, buildAPKWithLabelResources(t, resources, denseOffsets))
	assertEqual(t, "name", info.Name, "Indirect")
}

func TestALabelReferenceLoopEndsWithAWarning(t *testing.T) {
	resources := []resEntry{{typeName: "string", entry: 0, values: []resTestValue{referenceValue(resID("string", 0))}}}
	info := mustParse(t, buildAPKWithLabelResources(t, resources, denseOffsets))
	assertEqual(t, "name", info.Name, "com.acme.label")
	assertWarningMentions(t, info, "android:label")
}

func TestSparseAndCompactResourceTablesResolveLikeDenseOnes(t *testing.T) {
	resources := []resEntry{
		{typeName: "string", entry: 0, values: []resTestValue{stringValue("", "First")}},
		{typeName: "string", entry: 3, values: []resTestValue{stringValue("", "Fourth")}},
	}
	for _, layout := range []resTableLayout{denseOffsets, sparseOffsets, offset16WithCompactEntries} {
		data := buildAPKWithManifest(t,
			manifestElement("com.acme.layout", attrs(androidReference("label", attrLabel, resID("string", 3)))),
			zipEntry{name: androidResourcesPath, data: encodeResourceTableLayout(resources, layout)},
		)
		info := mustParse(t, data)
		assertEqual(t, "name", info.Name, "Fourth")
	}
}

func TestAMissingResourceTableFallsBackToThePackageNameWithAWarning(t *testing.T) {
	data := buildAPKWithManifest(t, acmeShopManifest())
	info := mustParse(t, data)
	assertEqual(t, "name", info.Name, "com.acme.shop")
	assertWarningMentions(t, info, "resources.arsc")
	if info.Icon != nil {
		t.Error("want no icon")
	}
}

func TestTheHighestDensityRasterIconIsChosen(t *testing.T) {
	info := mustParse(t, buildAcmeShopAPK(t))
	assertEqual(t, "icon width", info.Icon.Width, 192)
	assertEqual(t, "icon color", iconCenterColor(t, info.Icon), acmeRed)
	assertEqual(t, "content type", info.Icon.ContentType, "image/png")
	if len(info.Warnings) != 0 {
		t.Errorf("want no warnings, got %q", info.Warnings)
	}
}

func TestAnAdaptiveOnlyIconUsesItsForegroundLayerWithAWarning(t *testing.T) {
	info := mustParse(t, buildAdaptiveIconAPK(t))
	assertEqual(t, "icon color", iconCenterColor(t, info.Icon), acmeBlue)
	assertWarningMentions(t, info, "foreground layer only")
}

func TestAVectorOnlyIconLeavesTheIconEmptyWithAWarning(t *testing.T) {
	vector := encodeAXML(element("vector", nil, element("path", nil)))
	info := mustParse(t, buildAPKWithIconFile(t, "res/drawable/ic_launcher.xml", vector))
	if info.Icon != nil {
		t.Fatal("want no icon")
	}
	assertWarningMentions(t, info, "vector drawables are not rendered")
}

func TestIconsLargerThan512PixelsAreScaledDownKeepingTheirAspectRatio(t *testing.T) {
	info := mustParse(t, buildAPKWithIconFile(t, "res/mipmap/ic.png", solidPNG(t, 1024, 512, acmeGreen)))
	assertEqual(t, "width", info.Icon.Width, 512)
	assertEqual(t, "height", info.Icon.Height, 256)
	assertEqual(t, "icon color", iconCenterColor(t, info.Icon), acmeGreen)
}

func TestAnIconOverThePixelLimitIsSkippedWithAWarning(t *testing.T) {
	data := buildAPKWithIconFile(t, "res/mipmap/ic.png", solidPNG(t, 64, 64, acmeGreen))
	info := mustParse(t, data, WithLimits(Limits{MaxIconPixels: 32 * 32}))
	if info.Icon != nil {
		t.Fatal("want no icon")
	}
	assertWarningMentions(t, info, "limit exceeded")
}

func TestACorruptIconIsSkippedWithAWarning(t *testing.T) {
	info := mustParse(t, buildAPKWithIconFile(t, "res/mipmap/ic.png", []byte("\x89PNG\r\n\x1a\ngarbage")))
	if info.Icon != nil {
		t.Fatal("want no icon")
	}
	assertWarningMentions(t, info, "icon not extracted")
}

func buildAPKWithLabelResources(t testing.TB, resources []resEntry, layout resTableLayout) []byte {
	return buildAPKWithManifest(t,
		manifestElement("com.acme.label", attrs(androidReference("label", attrLabel, resID("string", 0)))),
		zipEntry{name: androidResourcesPath, data: encodeResourceTableLayout(resources, layout)},
	)
}

func assertWarningMentions(t *testing.T, info *Info, text string) {
	t.Helper()
	for _, w := range info.Warnings {
		if strings.Contains(w, text) {
			return
		}
	}
	t.Errorf("no warning mentions %q; warnings: %q", text, info.Warnings)
}
