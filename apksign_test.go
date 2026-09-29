package appmeta

import "testing"

func TestAnAPKSignedWithTheDebugKeyIsADebugBuild(t *testing.T) {
	info := mustParse(t, signWithV2(t, buildMinimalAPK(t), androidDebugSubject))
	assertSigningType(t, info, "debug")
}

func TestAnAPKSignedWithAnyOtherKeyIsAReleaseBuild(t *testing.T) {
	info := mustParse(t, buildAcmeShopAPK(t))
	assertSigningType(t, info, "release")
}

func TestAV1OnlySignatureIsRead(t *testing.T) {
	info := mustParse(t, buildAdaptiveIconAPK(t))
	assertSigningType(t, info, "debug")
}

func TestTheV3SignerWinsOverTheV2Signer(t *testing.T) {
	block := signingBlock(
		signingPair{id: signingBlockV2ID, value: v2SignerValue(selfSignedCertificate(t, androidDebugSubject))},
		signingPair{id: signingBlockV3ID, value: v2SignerValue(selfSignedCertificate(t, acmeReleaseSubject))},
	)
	info := mustParse(t, withSigningBlock(t, buildMinimalAPK(t), block))
	assertSigningType(t, info, "release")
}

func TestTheSigningBlockWinsOverTheV1Signature(t *testing.T) {
	info := mustParse(t, signWithV2(t, buildAdaptiveIconAPK(t), acmeReleaseSubject))
	assertSigningType(t, info, "release")
}

func TestAnUnsignedAPKHasNoSigning(t *testing.T) {
	info := mustParse(t, buildMinimalAPK(t))
	if info.Signing != nil {
		t.Fatalf("want no signing, got %+v", info.Signing)
	}
}

func TestACorruptSigningBlockIsAWarningNotAnError(t *testing.T) {
	block := signingBlock(signingPair{id: signingBlockV2ID, value: []byte{0xFF, 0xFF, 0xFF, 0xFF}})
	info := mustParse(t, withSigningBlock(t, buildMinimalAPK(t), block))
	if info.Signing != nil {
		t.Fatal("want no signing")
	}
	assertWarningMentions(t, info, "signing unknown")
}

func TestACorruptV1SignatureIsAWarningNotAnError(t *testing.T) {
	data := buildAPKWithManifest(t, manifestElement("com.acme.v1", nil),
		zipEntry{name: "META-INF/CERT.RSA", data: []byte("\x30\x03\x02\x01")})
	info := mustParse(t, data)
	assertWarningMentions(t, info, "signing unknown")
}

func assertSigningType(t *testing.T, info *Info, want string) {
	t.Helper()
	if info.Signing == nil {
		t.Fatalf("want %s signing, got none (warnings %q)", want, info.Warnings)
	}
	assertEqual(t, "signing type", info.Signing.Type, want)
}
