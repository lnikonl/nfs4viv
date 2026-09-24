// Command packdist packs release files into an archive. The format is chosen
// from the output extension (.zip or .tar.gz), so the build scripts do not
// depend on external archiver tools. This is a build helper, not a user
// facing command.
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: packdist OUTPUT ARCHIVE_FILE...")
		os.Exit(2)
	}
	output := os.Args[1]
	files := os.Args[2:]
	if err := pack(output, files); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// pack writes the archive whose format is chosen from the output extension.
func pack(output string, files []string) error {
	switch {
	case strings.HasSuffix(output, ".zip"):
		return writeZip(output, files)
	case strings.HasSuffix(output, ".tar.gz"):
		return writeTarGz(output, files)
	default:
		return fmt.Errorf("unsupported archive extension: %s", output)
	}
}

func writeZip(path string, files []string) error {
	output, err := os.Create(path)
	if err != nil {
		return err
	}
	defer output.Close()
	archive := zip.NewWriter(output)
	for _, file := range files {
		if err := addZipFile(archive, file); err != nil {
			archive.Close()
			return err
		}
	}
	return archive.Close()
}

func addZipFile(archive *zip.Writer, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name = filepath.Base(path)
	header.Method = zip.Deflate
	writer, err := archive.CreateHeader(header)
	if err != nil {
		return err
	}
	source, err := os.Open(path)
	if err != nil {
		return err
	}
	defer source.Close()
	_, err = io.Copy(writer, source)
	return err
}

func writeTarGz(path string, files []string) error {
	output, err := os.Create(path)
	if err != nil {
		return err
	}
	defer output.Close()
	compressed := gzip.NewWriter(output)
	archive := tar.NewWriter(compressed)
	for _, file := range files {
		if err := addTarFile(archive, file); err != nil {
			archive.Close()
			compressed.Close()
			return err
		}
	}
	if err := archive.Close(); err != nil {
		compressed.Close()
		return err
	}
	return compressed.Close()
}

func addTarFile(archive *tar.Writer, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	header.Name = filepath.Base(path)
	if err := archive.WriteHeader(header); err != nil {
		return err
	}
	source, err := os.Open(path)
	if err != nil {
		return err
	}
	defer source.Close()
	_, err = io.Copy(archive, source)
	return err
}
