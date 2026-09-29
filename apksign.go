package appmeta

import (
	"bytes"
	"crypto/x509"
	"encoding/asn1"
	"encoding/binary"
	"fmt"
	"slices"
	"strings"
)

// APK signatures are read only to tell debug builds from release builds; the
// signatures themselves are not verified.

const (
	signingBlockFooterLen = 24
	signingBlockPairIDLen = 4
	signingBlockV2ID      = 0x7109871a
	signingBlockV3ID      = 0xf05368c0
	signingBlockV31ID     = 0x1b93ad61
	signingBlockLenLen    = 8
	lengthPrefixLen       = 4
)

var (
	signingBlockMagic   = []byte("APK Sig Block 42")
	errMalformedSigning = fmt.Errorf("%w: apk signature", ErrMalformed)
	// Newest scheme first: v3.1 and v3 may rotate to a new key, v2 cannot.
	signingSchemes = []uint32{signingBlockV31ID, signingBlockV3ID, signingBlockV2ID}
)

// apkSigning returns nil for an unsigned APK. It prefers the APK Signing
// Block (v2+), which modern builds may use exclusively, over v1 JAR signing.
func apkSigning(a *archive) (*Signing, error) {
	cert, err := signingBlockCertificate(a)
	if err != nil {
		return nil, err
	}
	if cert == nil {
		if cert, err = jarSignatureCertificate(a); err != nil || cert == nil {
			return nil, err
		}
	}
	parsed, err := x509.ParseCertificate(cert)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errMalformedSigning, err)
	}
	return &Signing{Type: signingTypeOf(parsed)}, nil
}

// signingTypeOf recognises the key Android build tools generate for debug
// builds (~/.android/debug.keystore).
func signingTypeOf(cert *x509.Certificate) string {
	subject := cert.Subject
	if subject.CommonName == "Android Debug" && slices.Contains(subject.Organization, "Android") && slices.Contains(subject.Country, "US") {
		return SigningDebug
	}
	return SigningRelease
}

// signingBlockCertificate finds the APK Signing Block that sits right before
// the central directory and returns the first signer's first certificate.
func signingBlockCertificate(a *archive) ([]byte, error) {
	cdOffset := a.directory.offset
	if cdOffset < signingBlockFooterLen+signingBlockLenLen {
		return nil, nil
	}
	footer, err := a.readRange(cdOffset-signingBlockFooterLen, signingBlockFooterLen)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(footer[signingBlockLenLen:], signingBlockMagic) {
		return nil, nil
	}
	// The size excludes the leading size field itself.
	size := binary.LittleEndian.Uint64(footer)
	if size < signingBlockFooterLen || size > cdOffset-signingBlockLenLen || size > uint64(a.limits.MaxEntrySize) {
		return nil, fmt.Errorf("%w: signing block of %d bytes", errMalformedSigning, size)
	}
	block, err := a.readRange(cdOffset-size-signingBlockLenLen, size+signingBlockLenLen)
	if err != nil {
		return nil, err
	}
	if binary.LittleEndian.Uint64(block) != size {
		return nil, fmt.Errorf("%w: signing block sizes disagree", errMalformedSigning)
	}
	return certificateFromSigningPairs(block[signingBlockLenLen : len(block)-signingBlockFooterLen])
}

// certificateFromSigningPairs walks the block's (length, id, value) pairs.
func certificateFromSigningPairs(pairs []byte) ([]byte, error) {
	values := map[uint32][]byte{}
	for len(pairs) > 0 {
		if len(pairs) < signingBlockLenLen {
			return nil, fmt.Errorf("%w: truncated pair", errMalformedSigning)
		}
		n := binary.LittleEndian.Uint64(pairs)
		pairs = pairs[signingBlockLenLen:]
		if n < signingBlockPairIDLen || n > uint64(len(pairs)) {
			return nil, fmt.Errorf("%w: pair of %d bytes", errMalformedSigning, n)
		}
		id := binary.LittleEndian.Uint32(pairs)
		if _, seen := values[id]; !seen {
			values[id] = pairs[signingBlockPairIDLen:n]
		}
		pairs = pairs[n:]
	}
	for _, id := range signingSchemes {
		if value, ok := values[id]; ok {
			return firstSignerCertificate(value)
		}
	}
	return nil, nil
}

// firstSignerCertificate reads signers -> signer -> signed data -> (skip
// digests) -> certificates -> first certificate, each prefixed by a uint32
// length (the v2 and v3 layouts agree up to here).
func firstSignerCertificate(value []byte) ([]byte, error) {
	signers, _, err := lengthPrefixed(value)
	if err != nil {
		return nil, err
	}
	signer, _, err := lengthPrefixed(signers)
	if err != nil {
		return nil, err
	}
	signedData, _, err := lengthPrefixed(signer)
	if err != nil {
		return nil, err
	}
	_, rest, err := lengthPrefixed(signedData)
	if err != nil {
		return nil, err
	}
	certs, _, err := lengthPrefixed(rest)
	if err != nil {
		return nil, err
	}
	cert, _, err := lengthPrefixed(certs)
	return cert, err
}

func lengthPrefixed(b []byte) ([]byte, []byte, error) {
	if len(b) < lengthPrefixLen {
		return nil, nil, fmt.Errorf("%w: truncated length", errMalformedSigning)
	}
	n := uint64(binary.LittleEndian.Uint32(b))
	if n > uint64(len(b)-lengthPrefixLen) {
		return nil, nil, fmt.Errorf("%w: length %d overflows", errMalformedSigning, n)
	}
	return b[lengthPrefixLen : lengthPrefixLen+n], b[lengthPrefixLen+n:], nil
}

// jarSignatureCertificate reads the first v1 signature block
// (META-INF/*.RSA, .DSA or .EC), a PKCS#7 SignedData.
func jarSignatureCertificate(a *archive) ([]byte, error) {
	var path string
	for name := range a.files {
		file, ok := strings.CutPrefix(name, "META-INF/")
		isSignature := strings.HasSuffix(file, ".RSA") || strings.HasSuffix(file, ".DSA") || strings.HasSuffix(file, ".EC")
		if ok && isSignature && !strings.Contains(file, "/") && (path == "" || name < path) {
			path = name
		}
	}
	if path == "" {
		return nil, nil
	}
	data, err := a.read(path)
	if err != nil {
		return nil, err
	}
	return pkcs7FirstCertificate(data)
}

// pkcs7FirstCertificate walks ContentInfo -> [0] SignedData -> [0]
// certificates with encoding/asn1, which checks every length against the
// input.
func pkcs7FirstCertificate(der []byte) ([]byte, error) {
	var contentInfo asn1.RawValue
	if _, err := asn1.Unmarshal(der, &contentInfo); err != nil {
		return nil, fmt.Errorf("%w: %v", errMalformedSigning, err)
	}
	var oid asn1.ObjectIdentifier
	rest, err := asn1.Unmarshal(contentInfo.Bytes, &oid)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errMalformedSigning, err)
	}
	var explicit, signedData asn1.RawValue
	if _, err := asn1.Unmarshal(rest, &explicit); err != nil {
		return nil, fmt.Errorf("%w: %v", errMalformedSigning, err)
	}
	if _, err := asn1.Unmarshal(explicit.Bytes, &signedData); err != nil {
		return nil, fmt.Errorf("%w: %v", errMalformedSigning, err)
	}
	fields := signedData.Bytes
	for len(fields) > 0 {
		var field asn1.RawValue
		if fields, err = asn1.Unmarshal(fields, &field); err != nil {
			return nil, fmt.Errorf("%w: %v", errMalformedSigning, err)
		}
		if field.Class == asn1.ClassContextSpecific && field.Tag == 0 {
			var cert asn1.RawValue
			if _, err := asn1.Unmarshal(field.Bytes, &cert); err != nil {
				return nil, fmt.Errorf("%w: %v", errMalformedSigning, err)
			}
			return cert.FullBytes, nil
		}
	}
	return nil, fmt.Errorf("%w: no certificates", errMalformedSigning)
}
