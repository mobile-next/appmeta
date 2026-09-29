package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestTheCLIPrintsAnErrorForAFileThatIsNotAnApp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.zip")
	writeZipWithOneFile(t, path, "notes.txt")
	var out bytes.Buffer
	err := run(&out, path, "", false)
	if err == nil {
		t.Fatal("want an error")
	}
}

func TestTheCLIFailsForAMissingFile(t *testing.T) {
	var out bytes.Buffer
	if err := run(&out, filepath.Join(t.TempDir(), "missing.apk"), "", false); err == nil {
		t.Fatal("want an error")
	}
}

func TestErrorsArePrintedAsJSON(t *testing.T) {
	var out bytes.Buffer
	printJSON(&out, map[string]string{"error": "boom"})
	var got map[string]string
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || got["error"] != "boom" {
		t.Fatalf("got %q, %v", out.String(), err)
	}
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
