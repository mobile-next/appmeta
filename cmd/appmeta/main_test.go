package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"

	"github.com/mobile-next/appmeta"
)

func TestAFileThatIsNotAnAppIsReportedAsUnsupported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.zip")
	writeZipWithOneFile(t, path, "notes.txt")
	err := runCommand(t, path)
	if !errors.Is(err, appmeta.ErrUnsupportedFormat) {
		t.Fatalf("got %v, want ErrUnsupportedFormat", err)
	}
}

func TestAMissingFileIsAnError(t *testing.T) {
	if err := runCommand(t, filepath.Join(t.TempDir(), "missing.apk")); err == nil {
		t.Fatal("want an error")
	}
}

func TestLongFlagsAreAccepted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.zip")
	writeZipWithOneFile(t, path, "notes.txt")
	err := runCommand(t, "--no-icon", "--icon", filepath.Join(t.TempDir(), "icon.png"), path)
	if !errors.Is(err, appmeta.ErrUnsupportedFormat) {
		t.Fatalf("flags were not parsed: %v", err)
	}
}

func TestExactlyOnePathIsRequired(t *testing.T) {
	if err := runCommand(t); err == nil {
		t.Fatal("want an error without a path")
	}
	if err := runCommand(t, "a.apk", "b.apk"); err == nil {
		t.Fatal("want an error with two paths")
	}
}

func TestErrorsArePrintedAsJSON(t *testing.T) {
	var out bytes.Buffer
	if err := printJSON(&out, map[string]string{"error": "boom"}); err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || got["error"] != "boom" {
		t.Fatalf("got %q, %v", out.String(), err)
	}
}

func TestAFailedWriteOfTheOutputIsAnError(t *testing.T) {
	err := printJSON(closedPipe{}, map[string]string{"name": "Acme"})
	if !errors.Is(err, errClosedPipe) {
		t.Fatalf("got %v, want the write error", err)
	}
}

var errClosedPipe = errors.New("closed pipe")

type closedPipe struct{}

func (closedPipe) Write([]byte) (int, error) {
	return 0, errClosedPipe
}

func runCommand(t *testing.T, args ...string) error {
	t.Helper()
	var out bytes.Buffer
	cmd := newRootCommand(&out)
	cmd.SetArgs(args)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	return cmd.Execute()
}

func writeZipWithOneFile(t *testing.T, path, name string) {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	if _, err := w.Create(name); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestThereAreNoShortFlagsExceptHelp(t *testing.T) {
	cmd := newRootCommand(&bytes.Buffer{})
	cmd.InitDefaultHelpFlag()
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Shorthand != "" && f.Name != "help" {
			t.Errorf("--%s has short form -%s", f.Name, f.Shorthand)
		}
	})
}
