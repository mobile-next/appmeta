package appmeta

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// maxIconCandidates bounds how many icon names are matched and how many icon
// files are opened to compare sizes; real apps declare a handful.
const maxIconCandidates = 32

var iosDeviceFamilies = map[int64]string{1: "phone", 2: "tablet", 3: "tv", 4: "watch", 7: "vision"}

// iconVariantSuffix matches what Xcode appends to CFBundleIconFiles names.
var iconVariantSuffix = regexp.MustCompile(`^(@[23]x)?(~ipad|~iphone)?\.png$`)

// findAppBundle returns the "Payload/<name>.app/" prefix of the main app.
func findAppBundle(a *archive) string {
	best := ""
	for name := range a.files {
		rest, ok := strings.CutPrefix(name, "Payload/")
		if !ok {
			continue
		}
		app, file, ok := strings.Cut(rest, "/")
		bundle := "Payload/" + app + "/"
		// Pick the smallest name so the result does not depend on map order.
		if ok && strings.HasSuffix(app, ".app") && file == "Info.plist" && (best == "" || bundle < best) {
			best = bundle
		}
	}
	return best
}

func parseIPA(a *archive) (*Info, error) {
	bundle := findAppBundle(a)
	data, err := a.read(bundle + "Info.plist")
	if err != nil {
		return nil, err
	}
	plist, err := decodePlist(data, a.limits.MaxDepth)
	if err != nil {
		return nil, fmt.Errorf("%sInfo.plist: %w", bundle, err)
	}
	bundleID := plistString(plist, "CFBundleIdentifier")
	if bundleID == "" {
		return nil, fmt.Errorf("%sInfo.plist: %w: no CFBundleIdentifier", bundle, errMalformedPlist)
	}

	info := &Info{
		Format:          "ipa",
		Platform:        "ios",
		BundleID:        bundleID,
		Name:            firstNonEmpty(plistString(plist, "CFBundleDisplayName"), plistString(plist, "CFBundleName"), bundleID),
		Version:         plistString(plist, "CFBundleShortVersionString"),
		BuildNumber:     plistString(plist, "CFBundleVersion"),
		MinOSVersion:    plistString(plist, "MinimumOSVersion"),
		TargetOSVersion: plistString(plist, "DTPlatformVersion"),
		IsSimulator:     strings.Contains(strings.ToLower(plistString(plist, "DTPlatformName")), "simulator"),
		DeviceFamilies:  deviceFamilies(plist),
	}
	extractIPAIcon(a, bundle, plist, info)
	return info, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// deviceFamilies defaults to phone, as iOS does when UIDeviceFamily is absent.
func deviceFamilies(plist map[string]any) []string {
	var families []string
	for _, id := range plistInts(plist, "UIDeviceFamily") {
		if name, ok := iosDeviceFamilies[id]; ok && !slices.Contains(families, name) {
			families = append(families, name)
		}
	}
	if len(families) == 0 {
		return []string{"phone"}
	}
	return families
}

func extractIPAIcon(a *archive, bundle string, plist map[string]any, info *Info) {
	var best string
	bestArea := 0
	for _, path := range iconCandidates(a, bundle, iconBaseNames(plist)) {
		data, err := a.read(path)
		if err != nil {
			continue
		}
		w, h, err := imageSize(data)
		if err == nil && w*h > bestArea {
			best, bestArea = path, w*h
		}
	}
	if best == "" {
		primary := plistDict(plistDict(plist, "CFBundleIcons"), "CFBundlePrimaryIcon")
		if a.has(bundle+"Assets.car") || plistString(primary, "CFBundleIconName") != "" {
			info.Warnings = append(info.Warnings, "icon not extracted: it is only in Assets.car")
			return
		}
		info.Warnings = append(info.Warnings, "icon not extracted: no icon file found")
		return
	}
	data, err := a.read(best)
	if err == nil {
		info.Icon, err = encodeIcon(data, a.limits.MaxIconPixels)
	}
	if err != nil {
		info.Warnings = append(info.Warnings, fmt.Sprintf("icon not extracted: %s: %v", strings.TrimPrefix(best, bundle), err))
	}
}

// iconBaseNames lists icon names from every place Info.plist declares them.
func iconBaseNames(plist map[string]any) []string {
	var names []string
	for _, key := range []string{"CFBundleIcons", "CFBundleIcons~ipad"} {
		primary := plistDict(plistDict(plist, key), "CFBundlePrimaryIcon")
		names = append(names, plistStrings(primary, "CFBundleIconFiles")...)
	}
	names = append(names, plistStrings(plist, "CFBundleIconFiles")...)
	names = append(names, plistStrings(plist, "CFBundleIconFile")...)

	var unique []string
	for _, n := range names {
		n = strings.TrimSuffix(n, ".png")
		if n != "" && !slices.Contains(unique, n) && len(unique) < maxIconCandidates {
			unique = append(unique, n)
		}
	}
	return unique
}

// iconCandidates finds the files in the bundle root that are variants of the
// declared icon names, e.g. AppIcon60x60 -> AppIcon60x60@3x.png.
func iconCandidates(a *archive, bundle string, baseNames []string) []string {
	var out []string
	for name := range a.files {
		file, ok := strings.CutPrefix(name, bundle)
		if !ok || strings.Contains(file, "/") {
			continue
		}
		for _, base := range baseNames {
			suffix, ok := strings.CutPrefix(file, base)
			if ok && iconVariantSuffix.MatchString(suffix) {
				out = append(out, name)
				break
			}
		}
	}
	slices.Sort(out)
	if len(out) > maxIconCandidates {
		out = out[:maxIconCandidates]
	}
	return out
}
