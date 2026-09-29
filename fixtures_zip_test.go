package appmeta

import (
	"archive/zip"
	"bytes"
	"testing"
)

type zipEntry struct {
	name string
	data []byte
}

// buildZip writes the entries in order, deflated.
func buildZip(t testing.TB, entries ...zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range entries {
		f, err := w.Create(e.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(e.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func parseBytes(data []byte, opts ...Option) (*Info, error) {
	return Parse(bytes.NewReader(data), int64(len(data)), opts...)
}

func mustParse(t testing.TB, data []byte, opts ...Option) *Info {
	t.Helper()
	info, err := parseBytes(data, opts...)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return info
}
