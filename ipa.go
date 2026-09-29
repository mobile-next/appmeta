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
		Format:          FormatIPA,
		Platform:        PlatformIOS,
		BundleID:        bundleID,
		Name:            firstNonEmpty(plistString(plist, "CFBundleDisplayName"), plistString(plist, "CFBundleName"), bundleID),
		Version:         plistString(plist, "CFBundleShortVersionString"),
		BuildNumber:     plistString(plist, "CFBundleVersion"),
		MinOSVersion:    plistString(plist, "MinimumOSVersion"),
		TargetOSVersion: plistString(plist, "DTPlatformVersion"),
		IsSimulator:     strings.Contains(strings.ToLower(plistString(plist, "DTPlatformName")), "simulator"),
		DeviceFamilies:  deviceFamilies(plist),
	}
	executable, warning := sniffExecutable(a, bundle, plistString(plist, "CFBundleExecutable"))
	info.Warnings = appendWarning(info.Warnings, warning)
	info.Architectures = executable.architectures
	// The Mach-O platform wins over DTPlatformName, which is only what Xcode
	// wrote into Info.plist.
	if executable.hasPlatform {
		info.IsSimulator = executable.isSimulator
	}
	info.Signing, info.IsDebuggable, warning = readProvisioningProfile(a, bundle)
	info.Warnings = appendWarning(info.Warnings, warning)
	info.Icon, warning = extractIPAIcon(a, bundle, plist)
	info.Warnings = appendWarning(info.Warnings, warning)
	return info, nil
}

func sniffExecutable(a *archive, bundle, executable string) (machOSummary, string) {
	if executable == "" {
		return machOSummary{}, "architectures unknown: Info.plist has no CFBundleExecutable"
	}
	data, err := a.readPrefix(bundle+executable, machOPrefixLen)
	if err != nil {
		return machOSummary{}, fmt.Sprintf("architectures unknown: %v", err)
	}
	summary, err := sniffMachO(data)
	if err != nil {
		return machOSummary{}, fmt.Sprintf("architectures unknown: %s: %v", executable, err)
	}
	return summary, ""
}

// readProvisioningProfile returns no signing when there is no profile, as
// for simulator builds and App Store downloads.
func readProvisioningProfile(a *archive, bundle string) (signing *Signing, isDebuggable bool, warning string) {
	path := bundle + "embedded.mobileprovision"
	if !a.has(path) {
		return nil, false, ""
	}
	data, err := a.read(path)
	if err == nil {
		signing, isDebuggable, err = parseProvisioningProfile(data, a.limits.MaxDepth)
	}
	if err != nil {
		return nil, false, fmt.Sprintf("signing unknown: embedded.mobileprovision: %v", err)
	}
	return signing, isDebuggable, ""
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

func extractIPAIcon(a *archive, bundle string, plist map[string]any) (*Icon, string) {
	path, data := largestIconFile(a, iconCandidates(a, bundle, iconBaseNames(plist)))
	if path == "" {
		primary := plistDict(plistDict(plist, "CFBundleIcons"), "CFBundlePrimaryIcon")
		if a.has(bundle+"Assets.car") || plistString(primary, "CFBundleIconName") != "" {
			return nil, "icon not extracted: it is only in Assets.car"
		}
		return nil, "icon not extracted: no icon file found"
	}
	icon, err := encodeIcon(data, a.limits.MaxIconPixels)
	if err != nil {
		return nil, fmt.Sprintf("icon not extracted: %s: %v", strings.TrimPrefix(path, bundle), err)
	}
	return icon, ""
}

// largestIconFile returns the path and content of the candidate with the
// most pixels, or an empty path when none is a readable image.
func largestIconFile(a *archive, candidates []string) (string, []byte) {
	var bestPath string
	var bestData []byte
	bestArea := 0
	for _, path := range candidates {
		data, err := a.read(path)
		if err != nil {
			continue
		}
		w, h, err := imageSize(data)
		if err == nil && w*h > bestArea {
			bestPath, bestData, bestArea = path, data, w*h
		}
	}
	return bestPath, bestData
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
