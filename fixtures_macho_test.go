package appmeta

import (
	"bytes"
	"encoding/asn1"
	"encoding/binary"
	"testing"
	"time"
)

const (
	platformIOS          = 2
	platformIOSSimulator = 7
	cpuARM64             = cpuTypeARM | cpuArchABI64
	cpuX86_64            = cpuTypeX86 | cpuArchABI64
	fatSliceAlignment    = 0x4000
)

type machOSlice struct {
	cpuType, cpuSubtype uint32
	data                []byte
}

// thinMachO is a 64-bit executable header with one LC_BUILD_VERSION.
func thinMachO(cpuType, cpuSubtype, platform uint32) machOSlice {
	var b bytes.Buffer
	le32 := func(v ...uint32) {
		for _, x := range v {
			_ = binary.Write(&b, binary.LittleEndian, x)
		}
	}
	const buildVersionLen = 24
	le32(machOMagic64, cpuType, cpuSubtype, 2, 1, buildVersionLen, 0, 0)
	le32(lcBuildVersion, buildVersionLen, platform, 15<<16, 17<<16|2<<8, 0)
	return machOSlice{cpuType: cpuType, cpuSubtype: cpuSubtype, data: b.Bytes()}
}

// fatMachO places each slice at a 16 KiB boundary, as lipo does for arm64.
func fatMachO(slices ...machOSlice) []byte {
	var b bytes.Buffer
	be32 := func(v ...uint32) {
		for _, x := range v {
			_ = binary.Write(&b, binary.BigEndian, x)
		}
	}
	be32(fatMagic, uint32(len(slices)))
	offset := uint32(fatSliceAlignment)
	for _, s := range slices {
		be32(s.cpuType, s.cpuSubtype, offset, uint32(len(s.data)), 14)
		offset += fatSliceAlignment
	}
	for i, s := range slices {
		b.Write(make([]byte, fatSliceAlignment*(i+1)-b.Len()))
		b.Write(s.data)
	}
	return b.Bytes()
}

type profileOptions struct {
	getTaskAllow         bool
	provisionedDevices   []string
	provisionsAllDevices bool
}

// provisioningProfile wraps a profile plist in CMS SignedData with no
// signers, which is the layout of embedded.mobileprovision minus the
// signature appmeta does not verify.
func provisioningProfile(t testing.TB, opts profileOptions) []byte {
	t.Helper()
	profile := map[string]any{
		"Name":           "Acme Shop Profile",
		"TeamIdentifier": []string{"ACME123456"},
		"ExpirationDate": time.Date(2027, 3, 1, 12, 0, 0, 0, time.UTC),
		"Entitlements": map[string]any{
			"get-task-allow":                      opts.getTaskAllow,
			"com.apple.developer.team-identifier": "ACME123456",
		},
	}
	if opts.provisionedDevices != nil {
		profile["ProvisionedDevices"] = opts.provisionedDevices
	}
	if opts.provisionsAllDevices {
		profile["ProvisionsAllDevices"] = true
	}

	type encapsulatedContent struct {
		Type    asn1.ObjectIdentifier
		Content []byte `asn1:"explicit,tag:0"`
	}
	type signedData struct {
		Version          int
		DigestAlgorithms []asn1.RawValue `asn1:"set"`
		Content          encapsulatedContent
		SignerInfos      []asn1.RawValue `asn1:"set"`
	}
	type contentInfo struct {
		Type    asn1.ObjectIdentifier
		Content signedData `asn1:"explicit,tag:0"`
	}
	der, err := asn1.Marshal(contentInfo{
		Type: asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2},
		Content: signedData{
			Version: 1,
			Content: encapsulatedContent{Type: asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}, Content: xmlPlist(t, profile)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return der
}

func developmentProfile(t testing.TB) []byte {
	return provisioningProfile(t, profileOptions{getTaskAllow: true, provisionedDevices: []string{"00008110-000A1B2C3D4E5F60"}})
}
