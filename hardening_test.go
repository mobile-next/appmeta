package appmeta

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

var errInjectedReadFailure = errors.New("injected network failure")

func TestAReadFailureIsAnErrorAndNeverAWarning(t *testing.T) {
	data := buildAcmeShopAPK(t)
	for healthyReads := 1; ; healthyReads++ {
		reader := &failingAfterReads{r: bytes.NewReader(data), remaining: healthyReads}
		info, err := Parse(reader, int64(len(data)))
		if !reader.failed {
			return // every read the parser needs succeeded
		}
		if !errors.Is(err, errInjectedReadFailure) {
			t.Fatalf("after %d healthy reads: got %+v, %v; want the read failure", healthyReads, info, err)
		}
	}
}

func TestACancelledParseNeverReturnsPartialMetadata(t *testing.T) {
	data := buildAcmeShopAPK(t)
	for healthyReads := 1; ; healthyReads++ {
		ctx, cancel := context.WithCancel(context.Background())
		reader := cancelAfterReads{r: bytes.NewReader(data), remaining: healthyReads, cancel: cancel}
		info, err := ParseContext(ctx, &reader, int64(len(data)))
		wasCancelled := ctx.Err() != nil
		cancel()
		if !wasCancelled {
			return
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled at read %d: got %+v, %v; want context.Canceled", healthyReads, info, err)
		}
	}
}

func TestMoreEntriesThanDeclaredAreStillLimited(t *testing.T) {
	data := zipDeclaringEntries(t, zipWithEntries(t, 65536), 0)
	_, err := parseBytes(data, WithLimits(Limits{MaxEntries: 100}))
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want ErrLimitExceeded", err)
	}
}

func TestADirectorySizeMarkerSendsTheReaderToTheZip64Record(t *testing.T) {
	data := zipWithEntries(t, 65536)
	eocd := bytes.LastIndex(data, eocdSignature)
	binary.LittleEndian.PutUint16(data[eocd+10:], 5)
	binary.LittleEndian.PutUint32(data[eocd+16:], 0)

	dir, err := readZipDirectory(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "entries", dir.entries, 65536)
}

func TestAZip64LocatorInAFileTooSmallForItsRecordIsUnsupported(t *testing.T) {
	locator := bytes.Join([][]byte{zip64LocatorSignature, make([]byte, zip64LocatorLen-4)}, nil)
	eocd := bytes.Join([][]byte{eocdSignature, make([]byte, eocdLen-4)}, nil)
	binary.LittleEndian.PutUint16(eocd[10:], zip16BitEntryCountMax)
	_, err := parseBytes(append(locator, eocd...))
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("got %v, want ErrUnsupportedFormat", err)
	}
}

func TestTheSigningBlockCountsTowardsTheTotalLimit(t *testing.T) {
	block := signingBlock(
		signingPair{id: 0x42726577, value: make([]byte, 200_000)},
		signingPair{id: signingBlockV2ID, value: v2SignerValue(selfSignedCertificate(t, acmeReleaseSubject))},
	)
	info := mustParse(t, withSigningBlock(t, buildMinimalAPK(t), block), WithLimits(Limits{MaxTotalSize: 100_000}))
	if info.Signing != nil {
		t.Fatalf("want no signing, got %+v", info.Signing)
	}
	assertWarningMentions(t, info, "limit exceeded")
}

func TestManyPermissionsAreParsedInLinearTime(t *testing.T) {
	permissions := make([]xmlNode, 60_000)
	for i := range permissions {
		permissions[i] = usesPermission(fmt.Sprintf("com.acme.permission.P%d", i))
	}
	manifest := encodeAXML(manifestElement("com.acme.perms", nil, permissions...))

	start := time.Now()
	m, err := parseManifest(manifest, DefaultLimits().MaxDepth)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "permissions", len(m.permissions), len(permissions))
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("parsing took %v", elapsed)
	}
}

func TestBracketsInsideXMLPlistStringsAreNotNesting(t *testing.T) {
	plist := acmeInfoPlist(map[string]any{"NSCameraUsageDescription": strings.Repeat(":( ", 100)})
	info := mustParse(t, buildIPA(t, xmlPlist(t, plist)))
	assertEqual(t, "bundleId", info.BundleID, "com.acme.shop")
}

func TestAMissingResourceTableIsNamedOnceInTheWarning(t *testing.T) {
	manifest := manifestElement("com.acme.noresources", attrs(androidReference("label", attrLabel, 0x7f010000)))
	info := mustParse(t, buildAPKWithManifest(t, manifest))
	assertWarningMentions(t, info, androidResourcesPath)
	for _, w := range info.Warnings {
		if strings.Contains(w, androidResourcesPath+": "+androidResourcesPath) {
			t.Errorf("warning repeats the path: %q", w)
		}
	}
}

func TestTheChosenIconIsReadFromTheArchiveOnce(t *testing.T) {
	plist := binaryPlist(t, acmeInfoPlist(map[string]any{"CFBundleIcons": nil, "CFBundleIconFiles": []string{"Icon.png"}}))
	icon := solidPNG(t, 300, 300, acmeGreen)
	roomForOneRead := int64(len(plist) + len(icon) + 10)
	info := mustParse(t, buildIPA(t, plist, zipEntry{name: "Icon.png", data: icon}), WithLimits(Limits{MaxTotalSize: roomForOneRead}))
	if info.Icon == nil {
		t.Fatalf("want an icon; warnings: %q", info.Warnings)
	}
}

type failingAfterReads struct {
	r         io.ReaderAt
	remaining int
	failed    bool
}

func (f *failingAfterReads) ReadAt(p []byte, off int64) (int, error) {
	if f.remaining <= 0 {
		f.failed = true
		return 0, errInjectedReadFailure
	}
	f.remaining--
	return f.r.ReadAt(p, off)
}

// zipWithEntries writes n empty stored entries, the first of which is the
// Android manifest. From 65536 entries on, the writer adds a zip64 record.
func zipWithEntries(t testing.TB, n int) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for i := range n {
		name := fmt.Sprintf("file%d", i)
		if i == 0 {
			name = androidManifestPath
		}
		if _, err := w.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store}); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// zipDeclaringEntries rewrites the end of central directory record as a
// 32-bit one that declares the given number of entries, as a hostile archive
// would to slip past a check of the declared count.
func zipDeclaringEntries(t testing.TB, data []byte, declared uint16) []byte {
	t.Helper()
	directory := bytes.Index(data, []byte("PK\x01\x02"))
	directoryEnd := bytes.LastIndex(data, zip64EOCDSignature)
	eocd := bytes.LastIndex(data, eocdSignature)
	binary.LittleEndian.PutUint16(data[eocd+8:], declared)
	binary.LittleEndian.PutUint16(data[eocd+10:], declared)
	binary.LittleEndian.PutUint32(data[eocd+12:], uint32(directoryEnd-directory))
	binary.LittleEndian.PutUint32(data[eocd+16:], uint32(directory))
	return data
}

func TestMalformedAppsAreReportedAsMalformed(t *testing.T) {
	manifest := encodeAXML(manifestElement("com.acme.truncated", nil))
	malformed := map[string][]byte{
		"truncated manifest": buildZip(t, zipEntry{name: androidManifestPath, data: manifest[:len(manifest)/2]}),
		"truncated plist":    buildIPA(t, []byte("bplist00")),
	}
	for name, data := range malformed {
		t.Run(name, func(t *testing.T) {
			_, err := parseBytes(data)
			if !errors.Is(err, ErrMalformed) {
				t.Fatalf("got %v, want ErrMalformed", err)
			}
		})
	}
}

func TestFormatPlatformAndSigningUseTheExportedNames(t *testing.T) {
	apk := mustParse(t, buildAcmeShopAPK(t))
	assertEqual(t, "format", apk.Format, FormatAPK)
	assertEqual(t, "platform", apk.Platform, PlatformAndroid)
	assertSigningType(t, apk, SigningRelease)

	ipa := mustParse(t, buildAcmeShopIPA(t))
	assertEqual(t, "format", ipa.Format, FormatIPA)
	assertEqual(t, "platform", ipa.Platform, PlatformIOS)
}
