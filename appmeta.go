// Package appmeta extracts metadata from Android .apk and iOS .ipa files.
//
// Parse reads through an io.ReaderAt and touches only the zip central
// directory and the few entries it needs, so the input can live behind HTTP
// range requests or S3 ranged GETs. Input is treated as hostile: every read is
// bounded by Limits and Parse never lets a panic escape.
//
// Parse takes no context. Callers that need a deadline should pass a ReaderAt
// that fails once their context is done; the limits bound the CPU work done
// between reads.
package appmeta

import (
	"errors"
	"fmt"
	"io"
	"time"
)

// SchemaVersion is the version of the JSON shape of Info. It changes only on
// backwards-incompatible changes to schema/appmeta.schema.json.
const SchemaVersion = 1

// Info is the metadata of one app binary. String fields are empty when the
// binary does not declare them.
type Info struct {
	SchemaVersion   int      `json:"schemaVersion"`
	Format          string   `json:"format"`   // "apk" or "ipa"
	Platform        string   `json:"platform"` // "android" or "ios"
	BundleID        string   `json:"bundleId"`
	Name            string   `json:"name"`
	Version         string   `json:"version"`
	BuildNumber     string   `json:"buildNumber"`
	MinOSVersion    string   `json:"minOsVersion"`
	TargetOSVersion string   `json:"targetOsVersion"`
	IsSimulator     bool     `json:"isSimulator"`
	IsDebuggable    bool     `json:"isDebuggable"`
	DeviceFamilies  []string `json:"deviceFamilies"`
	Architectures   []string `json:"architectures"`
	Permissions     []string `json:"permissions"`
	Signing         *Signing `json:"signing"`
	Icon            *Icon    `json:"icon"`
	Warnings        []string `json:"warnings"`
}

// Signing describes the provisioning profile an iOS app was signed with.
type Signing struct {
	Type      string     `json:"type"` // "development", "ad-hoc", "enterprise" or "app-store"
	TeamID    string     `json:"teamId"`
	ExpiresAt *time.Time `json:"expiresAt"`
}

// Icon is the app icon, re-encoded as PNG and at most MaxIconSize pixels on
// each side.
type Icon struct {
	ContentType string `json:"contentType"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	SHA256      string `json:"sha256"`
	// PNG holds the encoded image; it is base64 in JSON.
	PNG []byte `json:"base64,omitempty"`
}

var (
	// ErrUnsupportedFormat is returned for input that is not an APK or IPA.
	ErrUnsupportedFormat = errors.New("appmeta: not an apk or ipa")
	// ErrLimitExceeded is returned when the input exceeds one of the Limits.
	ErrLimitExceeded = errors.New("appmeta: limit exceeded")
)

// Parse extracts metadata from the APK or IPA of the given size read through r.
func Parse(r io.ReaderAt, size int64, opts ...Option) (info *Info, err error) {
	// The host must survive any input, so a parser bug becomes an error.
	defer func() {
		if p := recover(); p != nil {
			info = nil
			err = fmt.Errorf("appmeta: internal error: %v", p)
		}
	}()

	cfg := config{limits: DefaultLimits()}
	for _, opt := range opts {
		opt(&cfg)
	}
	return parse(r, size, cfg.limits)
}

// parse is Parse without the panic recovery, so fuzzing sees panics.
func parse(r io.ReaderAt, size int64, limits Limits) (*Info, error) {
	a, err := openArchive(r, size, limits)
	if err != nil {
		return nil, err
	}

	var info *Info
	switch {
	case a.has(androidManifestPath):
		info, err = parseAPK(a)
	case findAppBundle(a) != "":
		info, err = parseIPA(a)
	default:
		return nil, ErrUnsupportedFormat
	}
	if err != nil {
		return nil, err
	}
	info.SchemaVersion = SchemaVersion
	fillEmptyLists(info)
	return info, nil
}

// fillEmptyLists makes lists encode as [] rather than null, so consumers do
// not need two checks for "none".
func fillEmptyLists(info *Info) {
	if info.DeviceFamilies == nil {
		info.DeviceFamilies = []string{}
	}
	if info.Architectures == nil {
		info.Architectures = []string{}
	}
	if info.Permissions == nil {
		info.Permissions = []string{}
	}
	if info.Warnings == nil {
		info.Warnings = []string{}
	}
}
