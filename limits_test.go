package appmeta

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"errors"
	"hash/crc32"
	"runtime"
	"testing"
)

func TestAnEntryLargerThanTheEntryLimitIsRejected(t *testing.T) {
	data := buildMinimalAPK(t)
	_, err := parseBytes(data, WithLimits(Limits{MaxEntrySize: 64}))
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want ErrLimitExceeded", err)
	}
}

func TestReadingMoreThanTheTotalLimitIsRejected(t *testing.T) {
	data := buildAcmeShopAPK(t)
	_, err := parseBytes(data, WithLimits(Limits{MaxTotalSize: 600}))
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want ErrLimitExceeded", err)
	}
}

func TestAZipBombManifestIsRejectedWithoutInflatingIt(t *testing.T) {
	zeros := make([]byte, 128<<20)
	data := buildZip(t, zipEntry{name: androidManifestPath, data: zeros})
	if len(data) > 1<<20 {
		t.Fatalf("fixture should be small, is %d bytes", len(data))
	}
	_, err := parseBytes(data)
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want ErrLimitExceeded", err)
	}
}

func TestAnEntryThatInflatesBeyondItsDeclaredSizeIsCutOffAtTheLimit(t *testing.T) {
	data := buildZipWithFalseSize(t, androidManifestPath, make([]byte, 1<<20), 100)
	_, err := parseBytes(data, WithLimits(Limits{MaxEntrySize: 1000}))
	if err == nil {
		t.Fatal("want an error")
	}
}

func TestParseNeverLetsAPanicEscape(t *testing.T) {
	info, err := Parse(panickingReader{}, 1<<20)
	if info != nil || err == nil {
		t.Fatalf("got %v, %v; want an error", info, err)
	}
}

func TestZeroLimitFieldsKeepTheirDefaults(t *testing.T) {
	cfg := config{limits: DefaultLimits()}
	WithLimits(Limits{MaxDepth: 3})(&cfg)
	want := DefaultLimits()
	want.MaxDepth = 3
	assertEqual(t, "limits", cfg.limits, want)
}

type panickingReader struct{}

func (panickingReader) ReadAt([]byte, int64) (int, error) {
	panic("storage backend exploded")
}

// buildZipWithFalseSize writes an entry whose header understates its
// uncompressed size, as a hostile archive would.
func buildZipWithFalseSize(t testing.TB, name string, content []byte, declared uint64) []byte {
	t.Helper()
	var compressed bytes.Buffer
	fw, _ := flate.NewWriter(&compressed, flate.BestCompression)
	_, _ = fw.Write(content)
	_ = fw.Close()

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.CreateRaw(&zip.FileHeader{
		Name:               name,
		Method:             zip.Deflate,
		CRC32:              crc32.ChecksumIEEE(content),
		CompressedSize64:   uint64(compressed.Len()),
		UncompressedSize64: declared,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write(compressed.Bytes())
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestAStringPoolDeclaringBillionsOfStringsAllocatesAlmostNothing(t *testing.T) {
	// 36 bytes that made the surveyed androidbinary library allocate 1 GiB.
	hostile := encodeChunk(chunkXML, nil, encodeChunk(chunkStringPool, u32s(0x08000000, 0, 0, 0, 0), nil))
	allocated := bytesAllocatedBy(func() {
		_ = parseAXML(hostile, DefaultLimits().MaxDepth, func([]string, []xmlAttr) {})
	})
	if allocated > 1<<20 {
		t.Fatalf("allocated %d bytes for a %d-byte input", allocated, len(hostile))
	}
}

func bytesAllocatedBy(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}
