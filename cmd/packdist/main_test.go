package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestPackdist(t *testing.T) {
	source := t.TempDir()
	files := []string{
		filepath.Join(source, "nfs4viv"),
		filepath.Join(source, "README.md"),
		filepath.Join(source, "LICENSE"),
	}
	contents := map[string]string{
		"nfs4viv":   "binary",
		"README.md": "# nfs4viv",
		"LICENSE":   "MIT",
	}
	for _, file := range files {
		if err := os.WriteFile(file, []byte(contents[filepath.Base(file)]), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	output := t.TempDir()
	zipPath := filepath.Join(output, "release.zip")
	tarPath := filepath.Join(output, "release.tar.gz")
	if err := pack(zipPath, files); err != nil {
		t.Fatalf("zip: %v", err)
	}
	if err := pack(tarPath, files); err != nil {
		t.Fatalf("tar.gz: %v", err)
	}

	checkZip(t, zipPath, contents)
	checkTarGz(t, tarPath, contents)
}

func TestPackdistUnknownExtension(t *testing.T) {
	if err := pack(filepath.Join(t.TempDir(), "release.rar"), nil); err == nil {
		t.Fatal("expected an error for an unsupported extension")
	}
}

func checkZip(t *testing.T, path string, contents map[string]string) {
	t.Helper()
	archive, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("OpenReader(%s): %v", path, err)
	}
	defer archive.Close()
	if len(archive.File) != len(contents) {
		t.Fatalf("%s: %d files, want %d", path, len(archive.File), len(contents))
	}
	for _, file := range archive.File {
		want, ok := contents[file.Name]
		if !ok {
			t.Errorf("%s: unexpected file %q", path, file.Name)
			continue
		}
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != want {
			t.Errorf("%s: %s = %q, want %q", path, file.Name, data, want)
		}
	}
}

func checkTarGz(t *testing.T, path string, contents map[string]string) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer compressed.Close()
	archive := tar.NewReader(compressed)
	found := map[string]string{}
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(archive)
		if err != nil {
			t.Fatal(err)
		}
		found[header.Name] = string(data)
	}
	if len(found) != len(contents) {
		t.Fatalf("%s: %d files, want %d", path, len(found), len(contents))
	}
	for name, want := range contents {
		if found[name] != want {
			t.Errorf("%s: %s = %q, want %q", path, name, found[name], want)
		}
	}
}
