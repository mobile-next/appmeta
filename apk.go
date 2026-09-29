package appmeta

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

const (
	androidManifestPath  = "AndroidManifest.xml"
	androidResourcesPath = "resources.arsc"
)

// Android framework attribute ids (android.R.attr).
const (
	attrLabel            = 0x01010001
	attrIcon             = 0x01010002
	attrName             = 0x01010003
	attrDebuggable       = 0x0101000f
	attrMinSDKVersion    = 0x0101020c
	attrVersionCode      = 0x0101021b
	attrVersionName      = 0x0101021c
	attrTargetSDKVersion = 0x01010270
	attrRequired         = 0x0101028e
	attrDrawable         = 0x01010199
)

// androidReleases maps API levels to the release users know them by.
var androidReleases = map[int]string{
	1: "1.0", 2: "1.1", 3: "1.5", 4: "1.6", 5: "2.0", 6: "2.0.1", 7: "2.1", 8: "2.2",
	9: "2.3", 10: "2.3.3", 11: "3.0", 12: "3.1", 13: "3.2", 14: "4.0", 15: "4.0.3",
	16: "4.1", 17: "4.2", 18: "4.3", 19: "4.4", 20: "4.4W", 21: "5.0", 22: "5.1",
	23: "6.0", 24: "7.0", 25: "7.1", 26: "8.0", 27: "8.1", 28: "9", 29: "10", 30: "11",
	31: "12", 32: "12L", 33: "13", 34: "14", 35: "15", 36: "16",
}

type apkFeature struct {
	name     string
	required bool
}

type apkManifest struct {
	packageName string
	versionCode string
	versionName xmlAttr
	label       xmlAttr
	icon        xmlAttr
	minSDK      xmlAttr
	targetSDK   xmlAttr
	debuggable  bool
	permissions []string
	features    []apkFeature
}

func parseAPK(a *archive) (*Info, error) {
	data, err := a.read(androidManifestPath)
	if err != nil {
		return nil, err
	}
	m, err := parseManifest(data, a.limits.MaxDepth)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", androidManifestPath, err)
	}
	if m.packageName == "" {
		return nil, fmt.Errorf("%s: %w: no package name", androidManifestPath, errMalformedResource)
	}

	info := &Info{
		Format:          "apk",
		Platform:        "android",
		BundleID:        m.packageName,
		BuildNumber:     m.versionCode,
		MinOSVersion:    androidRelease(m.minSDK),
		TargetOSVersion: androidRelease(m.targetSDK),
		IsDebuggable:    m.debuggable,
		DeviceFamilies:  androidDeviceFamilies(m.features),
		Architectures:   androidABIs(a),
		Permissions:     m.permissions,
	}
	res := &apkResources{archive: a}
	info.Version = res.text(m.versionName, "android:versionName", info)
	info.Name = res.text(m.label, "android:label", info)
	if info.Name == "" {
		info.Name = m.packageName
	}
	extractAPKIcon(res, m.icon, info)
	return info, nil
}

// apkResources loads resources.arsc on first use; most manifests of simple
// apps need no lookups.
type apkResources struct {
	archive *archive
	table   *resourceTable
	err     error
	loaded  bool
}

func (r *apkResources) get() (*resourceTable, error) {
	if !r.loaded {
		r.loaded = true
		data, err := r.archive.read(androidResourcesPath)
		if err == nil {
			r.table, err = parseResourceTable(data)
		}
		if err != nil {
			r.err = fmt.Errorf("%s: %w", androidResourcesPath, err)
		}
	}
	return r.table, r.err
}

// text returns an attribute's text, resolving references. Failures become
// warnings: the rest of the metadata is still useful.
func (r *apkResources) text(attr xmlAttr, field string, info *Info) string {
	if !attr.isReference() {
		return attr.text()
	}
	table, err := r.get()
	if err != nil {
		info.Warnings = append(info.Warnings, fmt.Sprintf("%s %s not resolved: %v", field, formatResID(attr.data), err))
		return ""
	}
	s, ok := table.resolveText(attr.data)
	if !ok {
		info.Warnings = append(info.Warnings, fmt.Sprintf("%s %s not found in %s", field, formatResID(attr.data), androidResourcesPath))
	}
	return s
}

func parseManifest(data []byte, maxDepth int) (*apkManifest, error) {
	m := &apkManifest{}
	err := parseAXML(data, maxDepth, func(path []string, attrs []xmlAttr) {
		switch {
		case pathIs(path, "manifest"):
			if a, ok := findAttr(attrs, 0, "package"); ok {
				m.packageName = a.text()
			}
			if a, ok := findAttr(attrs, attrVersionCode, "versionCode"); ok {
				m.versionCode = a.text()
			}
			m.versionName, _ = findAttr(attrs, attrVersionName, "versionName")
		case pathIs(path, "manifest", "uses-sdk"):
			m.minSDK, _ = findAttr(attrs, attrMinSDKVersion, "minSdkVersion")
			m.targetSDK, _ = findAttr(attrs, attrTargetSDKVersion, "targetSdkVersion")
		case pathIs(path, "manifest", "application"):
			m.label, _ = findAttr(attrs, attrLabel, "label")
			m.icon, _ = findAttr(attrs, attrIcon, "icon")
			if a, ok := findAttr(attrs, attrDebuggable, "debuggable"); ok {
				m.debuggable = a.valueType == resValueTypeBool && a.data != 0
			}
		case pathIs(path, "manifest", "uses-permission"),
			pathIs(path, "manifest", "uses-permission-sdk-23"),
			pathIs(path, "manifest", "uses-permission-sdk-m"):
			if a, ok := findAttr(attrs, attrName, "name"); ok && a.text() != "" && !slices.Contains(m.permissions, a.text()) {
				m.permissions = append(m.permissions, a.text())
			}
		case pathIs(path, "manifest", "uses-feature"):
			feature := apkFeature{required: true}
			if a, ok := findAttr(attrs, attrName, "name"); ok {
				feature.name = a.text()
			}
			if a, ok := findAttr(attrs, attrRequired, "required"); ok && a.valueType == resValueTypeBool {
				feature.required = a.data != 0
			}
			m.features = append(m.features, feature)
		}
	})
	if err != nil {
		return nil, err
	}
	return m, nil
}

func extractAPKIcon(res *apkResources, attr xmlAttr, info *Info) {
	if !attr.isReference() {
		return
	}
	table, err := res.get()
	if err != nil {
		info.Warnings = append(info.Warnings, fmt.Sprintf("icon not extracted: %v", err))
		return
	}
	files := table.resolveFiles(attr.data)
	icon, err := firstDecodableRaster(res.archive, files)
	if icon != nil {
		info.Icon = icon
		return
	}
	for _, f := range files {
		if !strings.HasSuffix(f.path, ".xml") {
			continue
		}
		foreground, ok := adaptiveIconForeground(res.archive, f.path)
		if !ok {
			continue
		}
		if icon, _ := firstDecodableRaster(res.archive, table.resolveFiles(foreground)); icon != nil {
			info.Icon = icon
			info.Warnings = append(info.Warnings, "adaptive icon rendered from foreground layer only")
			return
		}
	}
	if err != nil {
		info.Warnings = append(info.Warnings, fmt.Sprintf("icon not extracted: %v", err))
		return
	}
	info.Warnings = append(info.Warnings, "icon not extracted: no raster image found (vector drawables are not rendered)")
}

// firstDecodableRaster tries raster files from the highest density down.
func firstDecodableRaster(a *archive, files []resourceFile) (*Icon, error) {
	var rasters []resourceFile
	for _, f := range files {
		if isRasterPath(f.path) {
			rasters = append(rasters, f)
		}
	}
	slices.SortStableFunc(rasters, func(x, y resourceFile) int {
		return densityRank(y.density) - densityRank(x.density)
	})
	var lastErr error
	for _, f := range rasters {
		data, err := a.read(f.path)
		if err != nil {
			lastErr = err
			continue
		}
		icon, err := encodeIcon(data, a.limits.MaxIconPixels)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", f.path, err)
			continue
		}
		return icon, nil
	}
	return nil, lastErr
}

func isRasterPath(path string) bool {
	for _, ext := range []string{".png", ".webp", ".jpg", ".jpeg"} {
		if strings.HasSuffix(strings.ToLower(path), ext) {
			return true
		}
	}
	return false
}

// densityRank orders densities for icon choice: the default configuration
// counts as mdpi, and nodpi as the lowest.
func densityRank(density uint16) int {
	switch density {
	case 0:
		return densityMedium
	case densityNone, densityAny:
		return 0
	}
	return int(density)
}

// adaptiveIconForeground returns the drawable of an adaptive icon's
// foreground layer, which may sit on the element or on a nested one such as
// <inset>.
func adaptiveIconForeground(a *archive, path string) (uint32, bool) {
	data, err := a.read(path)
	if err != nil {
		return 0, false
	}
	var found uint32
	err = parseAXML(data, a.limits.MaxDepth, func(p []string, attrs []xmlAttr) {
		if found != 0 || len(p) < 2 || p[0] != "adaptive-icon" || p[1] != "foreground" {
			return
		}
		if d, ok := findAttr(attrs, attrDrawable, "drawable"); ok && d.isReference() {
			found = d.data
		}
	})
	return found, err == nil && found != 0
}

// androidRelease maps an SDK attribute to a release name. Preview builds use
// a codename string, which is passed through.
func androidRelease(sdk xmlAttr) string {
	text := sdk.text()
	level, err := strconv.Atoi(text)
	if err != nil {
		return text
	}
	if release, ok := androidReleases[level]; ok {
		return release
	}
	return "API " + text
}

func androidDeviceFamilies(features []apkFeature) []string {
	requires := func(name string) bool {
		return slices.Contains(features, apkFeature{name: name, required: true})
	}
	switch {
	case requires("android.hardware.type.watch"):
		return []string{"watch"}
	case requires("android.hardware.type.automotive"):
		return []string{"car"}
	case requires("android.software.leanback"):
		return []string{"tv"}
	case slices.Contains(features, apkFeature{name: "android.software.leanback"}):
		return []string{"phone", "tablet", "tv"}
	}
	return []string{"phone", "tablet"}
}

// androidABIs lists the lib/<abi>/ directories that contain files.
func androidABIs(a *archive) []string {
	var abis []string
	for name := range a.files {
		rest, ok := strings.CutPrefix(name, "lib/")
		if !ok {
			continue
		}
		abi, file, ok := strings.Cut(rest, "/")
		if ok && abi != "" && file != "" && !strings.HasSuffix(file, "/") && !slices.Contains(abis, abi) {
			abis = append(abis, abi)
		}
	}
	slices.Sort(abis)
	return abis
}
