package appmeta

import (
	"testing"
	"time"
)

func TestADeviceBuildReportsItsArchitecturesAndIsNotASimulator(t *testing.T) {
	info := mustParse(t, buildAcmeShopIPA(t))
	assertSlice(t, "architectures", info.Architectures, []string{"arm64"})
	if info.IsSimulator {
		t.Error("want a device build")
	}
}

func TestASimulatorBuildIsDetectedFromTheExecutable(t *testing.T) {
	info := mustParse(t, buildSimulatorIPA(t))
	assertSlice(t, "architectures", info.Architectures, []string{"x86_64", "arm64"})
	if !info.IsSimulator {
		t.Error("want a simulator build")
	}
}

func TestTheExecutableOverridesAMisleadingPlatformName(t *testing.T) {
	plist := acmeInfoPlist(map[string]any{"DTPlatformName": "iphonesimulator"})
	info := mustParse(t, buildIPA(t, binaryPlist(t, plist), acmeDeviceExecutable()))
	if info.IsSimulator {
		t.Error("want a device build")
	}
}

func TestAThinExecutableIsRecognised(t *testing.T) {
	exe := thinMachO(cpuARM64, 2, platformIOS).data
	info := mustParse(t, buildIPA(t, binaryPlist(t, acmeInfoPlist(nil)), zipEntry{name: "AcmeShop", data: exe}))
	assertSlice(t, "architectures", info.Architectures, []string{"arm64e"})
}

func TestAMissingExecutableIsAWarningNotAnError(t *testing.T) {
	info := mustParse(t, buildIPA(t, binaryPlist(t, acmeInfoPlist(nil))))
	assertSlice(t, "architectures", info.Architectures, []string{})
	assertWarningMentions(t, info, "architectures unknown")
}

func TestAnExecutableThatIsNotMachOIsAWarning(t *testing.T) {
	info := mustParse(t, buildIPA(t, binaryPlist(t, acmeInfoPlist(nil)), zipEntry{name: "AcmeShop", data: []byte("#!/bin/sh\necho this is a shell script, not an app\n")}))
	assertWarningMentions(t, info, "not a mach-o")
}

func TestADevelopmentProfileReportsTeamExpiryAndDebuggable(t *testing.T) {
	info := mustParse(t, buildAcmeShopIPA(t))
	if info.Signing == nil {
		t.Fatal("want signing")
	}
	assertEqual(t, "type", info.Signing.Type, "development")
	assertEqual(t, "teamId", info.Signing.TeamID, "ACME123456")
	assertEqual(t, "expiresAt", *info.Signing.ExpiresAt, time.Date(2027, 3, 1, 12, 0, 0, 0, time.UTC))
	if !info.IsDebuggable {
		t.Error("want debuggable (get-task-allow)")
	}
}

func TestProfileTypesAreTellApartByDevicesAndEntitlements(t *testing.T) {
	cases := map[string]profileOptions{
		"development": {getTaskAllow: true, provisionedDevices: []string{"device"}},
		"ad-hoc":      {provisionedDevices: []string{"device"}},
		"enterprise":  {provisionsAllDevices: true},
		"app-store":   {},
	}
	for want, opts := range cases {
		t.Run(want, func(t *testing.T) {
			ipa := buildIPA(t, binaryPlist(t, acmeInfoPlist(nil)), acmeDeviceExecutable(),
				zipEntry{name: "embedded.mobileprovision", data: provisioningProfile(t, opts)})
			info := mustParse(t, ipa)
			assertEqual(t, "type", info.Signing.Type, want)
		})
	}
}

func TestAnIPAWithoutAProfileHasNoSigning(t *testing.T) {
	info := mustParse(t, buildSimulatorIPA(t))
	if info.Signing != nil {
		t.Fatalf("want no signing, got %+v", info.Signing)
	}
}

func TestACorruptProfileIsAWarning(t *testing.T) {
	ipa := buildIPA(t, binaryPlist(t, acmeInfoPlist(nil)), acmeDeviceExecutable(),
		zipEntry{name: "embedded.mobileprovision", data: []byte("\x30\x82garbage")})
	info := mustParse(t, ipa)
	if info.Signing != nil {
		t.Fatal("want no signing")
	}
	assertWarningMentions(t, info, "signing unknown")
}
