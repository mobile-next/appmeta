package appmeta

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/binary"
	"math/big"
	"testing"
	"time"
)

type pkcs7TestContent struct {
	Type asn1.ObjectIdentifier
}

type pkcs7TestSignedData struct {
	Version          int
	DigestAlgorithms []asn1.RawValue `asn1:"set"`
	Content          pkcs7TestContent
	Certificates     asn1.RawValue
	SignerInfos      []asn1.RawValue `asn1:"set"`
}

type pkcs7TestContentInfo struct {
	Type    asn1.ObjectIdentifier
	Content pkcs7TestSignedData `asn1:"explicit,tag:0"`
}

type signingPair struct {
	id    uint32
	value []byte
}

var (
	androidDebugSubject = pkix.Name{CommonName: "Android Debug", Organization: []string{"Android"}, Country: []string{"US"}}
	acmeReleaseSubject  = pkix.Name{CommonName: "Acme Shop", Organization: []string{"Acme"}, Country: []string{"US"}}
)

// selfSignedCertificate returns the DER of a throwaway certificate.
func selfSignedCertificate(t testing.TB, subject pkix.Name) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      subject,
		NotBefore:    time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:     time.Date(2050, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

func withLengthPrefix(parts ...[]byte) []byte {
	body := bytes.Join(parts, nil)
	return append(u32s(uint32(len(body))), body...)
}

// v2SignerValue lays out signers -> signer -> signed data with one
// certificate; the signature and public key are empty since appmeta does not
// verify them.
func v2SignerValue(cert []byte) []byte {
	signedData := bytes.Join([][]byte{
		withLengthPrefix(),                       // digests
		withLengthPrefix(withLengthPrefix(cert)), // certificates
		withLengthPrefix(),                       // additional attributes
	}, nil)
	signer := bytes.Join([][]byte{withLengthPrefix(signedData), withLengthPrefix(), withLengthPrefix()}, nil)
	return withLengthPrefix(withLengthPrefix(signer))
}

func signingBlock(pairs ...signingPair) []byte {
	var body bytes.Buffer
	for _, p := range pairs {
		_ = binary.Write(&body, binary.LittleEndian, uint64(signingBlockPairIDLen+len(p.value)))
		body.Write(u32s(p.id))
		body.Write(p.value)
	}
	size := uint64(body.Len() + signingBlockFooterLen)
	var block bytes.Buffer
	_ = binary.Write(&block, binary.LittleEndian, size)
	block.Write(body.Bytes())
	_ = binary.Write(&block, binary.LittleEndian, size)
	block.Write(signingBlockMagic)
	return block.Bytes()
}

// withSigningBlock inserts the block before the central directory and moves
// the directory offset, as apksigner does.
func withSigningBlock(t testing.TB, zipData, block []byte) []byte {
	t.Helper()
	eocd := bytes.LastIndex(zipData, eocdSignature)
	cd := binary.LittleEndian.Uint32(zipData[eocd+16:])
	out := bytes.Join([][]byte{zipData[:cd], block, zipData[cd:]}, nil)
	binary.LittleEndian.PutUint32(out[eocd+len(block)+16:], cd+uint32(len(block)))
	return out
}

func pkcs7WithCertificate(t testing.TB, cert []byte) []byte {
	t.Helper()
	der, err := asn1.Marshal(pkcs7TestContentInfo{
		Type: asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2},
		Content: pkcs7TestSignedData{
			Version:      1,
			Content:      pkcs7TestContent{Type: asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}},
			Certificates: asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: cert},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return der
}

func signWithV2(t testing.TB, apk []byte, subject pkix.Name) []byte {
	block := signingBlock(signingPair{id: signingBlockV2ID, value: v2SignerValue(selfSignedCertificate(t, subject))})
	return withSigningBlock(t, apk, block)
}
