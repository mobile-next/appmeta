package appmeta

import (
	"errors"
	"testing"
)

func TestInputThatIsNotAZipIsUnsupported(t *testing.T) {
	_, err := parseBytes([]byte("definitely not a zip file"))
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("got %v, want ErrUnsupportedFormat", err)
	}
}

func TestEmptyInputIsUnsupported(t *testing.T) {
	_, err := parseBytes(nil)
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("got %v, want ErrUnsupportedFormat", err)
	}
}

func TestAZipThatIsNeitherAnAPKNorAnIPAIsUnsupported(t *testing.T) {
	data := buildZip(t, zipEntry{name: "hello.txt", data: []byte("hi")})
	_, err := parseBytes(data)
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("got %v, want ErrUnsupportedFormat", err)
	}
}

func TestAZipDeclaringMoreEntriesThanTheLimitIsRejectedBeforeReadingThem(t *testing.T) {
	data := buildZip(t,
		zipEntry{name: "a", data: nil},
		zipEntry{name: "b", data: nil},
		zipEntry{name: "c", data: nil},
	)
	_, err := parseBytes(data, WithLimits(Limits{MaxEntries: 2}))
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want ErrLimitExceeded", err)
	}
}
