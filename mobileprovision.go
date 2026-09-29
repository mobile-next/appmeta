package appmeta

import (
	"bytes"
	"fmt"
	"time"
)

var (
	xmlDeclaration = []byte("<?xml")
	plistEndTag    = []byte("</plist>")
)

// parseProvisioningProfile reads embedded.mobileprovision, a CMS SignedData
// blob whose content is an XML plist. The signature is not verified (the
// device does that at install time), so the plist is located directly
// instead of decoding the CMS structure.
func parseProvisioningProfile(data []byte, maxDepth int) (*Signing, bool, error) {
	start := bytes.Index(data, xmlDeclaration)
	if start < 0 {
		return nil, false, fmt.Errorf("%w: no plist in provisioning profile", errMalformedPlist)
	}
	end := bytes.Index(data[start:], plistEndTag)
	if end < 0 {
		return nil, false, fmt.Errorf("%w: unterminated plist in provisioning profile", errMalformedPlist)
	}
	profile, err := decodePlist(data[start:start+end+len(plistEndTag)], maxDepth)
	if err != nil {
		return nil, false, err
	}

	entitlements := plistDict(profile, "Entitlements")
	getTaskAllow := plistBool(entitlements, "get-task-allow")
	signing := &Signing{Type: profileType(profile, getTaskAllow)}
	if teams := plistStrings(profile, "TeamIdentifier"); len(teams) > 0 {
		signing.TeamID = teams[0]
	} else {
		signing.TeamID = plistString(entitlements, "com.apple.developer.team-identifier")
	}
	if expires, ok := profile["ExpirationDate"].(time.Time); ok {
		utc := expires.UTC()
		signing.ExpiresAt = &utc
	}
	return signing, getTaskAllow, nil
}

// profileType follows how Xcode distinguishes profiles: enterprise profiles
// provision all devices, development and ad-hoc ones list devices, and
// App Store ones list none.
func profileType(profile map[string]any, getTaskAllow bool) string {
	switch {
	case plistBool(profile, "ProvisionsAllDevices"):
		return SigningEnterprise
	case len(plistStrings(profile, "ProvisionedDevices")) > 0 && getTaskAllow:
		return SigningDevelopment
	case len(plistStrings(profile, "ProvisionedDevices")) > 0:
		return SigningAdHoc
	}
	return SigningAppStore
}
