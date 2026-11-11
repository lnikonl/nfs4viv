package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/lnikonl/nfs4viv/resources"
	"github.com/lnikonl/nfs4viv/viv"
)

const vivManifestName = ".nfs4viv.json"

// Only bytes outside entry payloads are stored in the manifest. This preserves
// headers, padding and trailing bytes without keeping a second copy of the car.
type vivManifest struct {
	Version int                `json:"version"`
	Entries []vivManifestEntry `json:"entries"`
	Gaps    []vivGap           `json:"gaps"`
}

type vivManifestEntry struct {
	Name    string `json:"name"`
	RawName []byte `json:"raw_name,omitempty"` // Names need not be valid UTF-8.
	File    string `json:"file"`
	Offset  int    `json:"offset"`
	Size    int    `json:"size"`
}

func (e vivManifestEntry) name() string {
	if e.RawName != nil {
		return string(e.RawName)
	}
	return e.Name
}

type vivGap struct {
	Offset int    `json:"offset"`
	Data   []byte `json:"data"`
}

func extractViv(input, output string, selected []string, sub bool, validation *validationFlags) error {
	data, err := os.ReadFile(input)
	if err != nil {
		return err
	}
	archive, err := viv.Read(data)
	if err != nil {
		return fmt.Errorf("%s: %w", input, err)
	}
	if err := validation.check(archive); err != nil {
		return err
	}
	full := len(selected) == 0
	var indices []int
	if full {
		for index := range archive.Entries {
			indices = append(indices, index)
		}
	} else {
		for _, name := range selected {
			index, ok := archive.Find(name)
			if !ok {
				return fmt.Errorf("entry %q not found in %s", name, input)
			}
			indices = append(indices, index)
		}
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		return err
	}
	var manifest *vivManifest
	if full {
		manifest = newVivManifest(data, archive)
	}
	used := map[string]int{vivManifestName: 1}
	for _, index := range indices {
		entry := archive.Entries[index]
		name := uniqueName(used, safeName(entry.Name))
		if full {
			manifest.Entries[index].File = name
		}
		path := filepath.Join(output, name)
		format := resourceFormat(entry.Name)
		if sub && resources.Supported(format) {
			if err := extractResourceDirectory(format, entry.Data, path); err != nil {
				return fmt.Errorf("%s: %w", entry.Name, err)
			}
		} else {
			if err := writeFileAtomic(path, entry.Data); err != nil {
				return err
			}
		}
		fmt.Printf("extracted %s\n", path)
	}
	if full {
		encoded, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return err
		}
		return writeFileAtomic(filepath.Join(output, vivManifestName), append(encoded, '\n'))
	}
	return nil
}

func newVivManifest(data []byte, archive *viv.File) *vivManifest {
	manifest := &vivManifest{Version: 1, Entries: make([]vivManifestEntry, len(archive.Entries))}
	position := 16
	for index, entry := range archive.Entries {
		item := vivManifestEntry{
			Name: entry.Name, Offset: int(binary.BigEndian.Uint32(data[position : position+4])), Size: len(entry.Data),
		}
		if !utf8.ValidString(entry.Name) {
			item.RawName = []byte(entry.Name)
		}
		manifest.Entries[index] = item
		position += 9 + len(entry.Name)
	}
	ordered := append([]vivManifestEntry(nil), manifest.Entries...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Offset < ordered[j].Offset })
	position = 0
	for _, entry := range ordered {
		if entry.Offset > position {
			manifest.Gaps = append(manifest.Gaps, vivGap{Offset: position, Data: data[position:entry.Offset]})
		}
		position = max(position, entry.Offset+entry.Size)
	}
	if position < len(data) {
		manifest.Gaps = append(manifest.Gaps, vivGap{Offset: position, Data: data[position:]})
	}
	return manifest
}

func vivPack(args []string) error {
	flags := flag.NewFlagSet("viv pack", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	output := flags.String("o", "", "output archive (default: DIR.viv next to the input directory)")
	sub := flags.Bool("sub", false, "pack supported resource subdirectories before assembling the VIV")
	validation := addValidationFlags(flags)
	positional, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return fmt.Errorf("usage: nfs4viv viv pack DIR [-o OUTPUT.VIV] [--sub] [--validate car] [--max-texture-size N]")
	}
	if err := validation.checkOptions(); err != nil {
		return err
	}
	directory, err := filepath.Abs(positional[0])
	if err != nil {
		return err
	}
	info, err := os.Stat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", directory)
	}
	out := *output
	if out == "" {
		out = directory + ".viv"
	}
	out, err = filepath.Abs(out)
	if err != nil {
		return err
	}
	if out == filepath.Join(directory, vivManifestName) {
		return fmt.Errorf("output archive would overwrite %s", vivManifestName)
	}
	manifest, err := readVivManifest(directory)
	if err != nil {
		return err
	}
	archive, err := collectVivEntries(directory, out, manifest, *sub)
	if err != nil {
		return err
	}
	if err := validation.check(archive); err != nil {
		return err
	}
	encoded, ok := restoreVivLayout(manifest, archive)
	if !ok {
		encoded, err = archive.Bytes()
		if err != nil {
			return err
		}
	}
	if err := writeFileAtomic(out, encoded); err != nil {
		return err
	}
	fmt.Printf("packed %d %s into %s (%d bytes)\n", len(archive.Entries), entryWord(len(archive.Entries)), out, len(encoded))
	return nil
}

func readVivManifest(directory string) (*vivManifest, error) {
	path := filepath.Join(directory, vivManifestName)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var manifest vivManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if manifest.Version != 1 {
		return nil, fmt.Errorf("%s: unsupported manifest version %d", path, manifest.Version)
	}
	used := map[string]bool{}
	for _, entry := range manifest.Entries {
		key := strings.ToLower(entry.File)
		if !filepath.IsLocal(entry.File) || strings.ContainsAny(entry.File, `/\`) || key == vivManifestName || used[key] {
			return nil, fmt.Errorf("%s: invalid or duplicate entry file %q", path, entry.File)
		}
		used[key] = true
	}
	return &manifest, nil
}

func collectVivEntries(directory, output string, manifest *vivManifest, sub bool) (*viv.File, error) {
	// WalkDir supplies a deterministic lexical order for folders without a
	// manifest and for newly added files. Symlinks are not followed.
	files := map[string][]byte{}
	var ordered []string
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path == directory {
				return nil
			}
			format := resourceFormat(entry.Name())
			if resources.Supported(format) {
				_, manifestErr := os.Lstat(filepath.Join(path, resources.ManifestName))
				if manifestErr == nil || sub {
					if !sub {
						return fmt.Errorf("%s is an extracted resource; use --sub to rebuild it", path)
					}
					data, err := packResourceDirectory(format, path, "")
					if err != nil {
						return fmt.Errorf("%s: %w", path, err)
					}
					files[relative] = data
					ordered = append(ordered, relative)
					return filepath.SkipDir
				}
			}
			return nil
		}
		if relative == vivManifestName || path == output {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[relative] = data
		ordered = append(ordered, relative)
		return nil
	})
	if err != nil {
		return nil, err
	}
	archive := &viv.File{}
	if manifest != nil {
		for _, entry := range manifest.Entries {
			if filepath.Join(directory, entry.File) == output {
				return nil, fmt.Errorf("output archive would overwrite entry file %q", entry.File)
			}
			if data, ok := files[entry.File]; ok {
				archive.Entries = append(archive.Entries, viv.Entry{Name: entry.name(), Data: data})
				delete(files, entry.File)
			}
		}
	}
	for _, name := range ordered {
		if data, ok := files[name]; ok {
			archive.Entries = append(archive.Entries, viv.Entry{Name: filepath.ToSlash(name), Data: data})
		}
	}
	return archive, nil
}

// Restore the original layout when names, order and sizes still match. Check
// overlapping ranges and reparse the result so edited, aliased entries cannot
// silently overwrite each other or the directory. Changed layouts use Bytes.
func restoreVivLayout(manifest *vivManifest, archive *viv.File) ([]byte, bool) {
	if manifest == nil || len(manifest.Entries) != len(archive.Entries) {
		return nil, false
	}
	ranges := append([]vivGap(nil), manifest.Gaps...)
	for index, entry := range archive.Entries {
		original := manifest.Entries[index]
		if original.name() != entry.Name || original.Size != len(entry.Data) || original.Offset < 0 {
			return nil, false
		}
		ranges = append(ranges, vivGap{Offset: original.Offset, Data: entry.Data})
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].Offset < ranges[j].Offset })
	var out []byte
	for _, part := range ranges {
		if part.Offset < 0 || part.Offset > len(out) {
			return nil, false
		}
		overlap := min(len(out)-part.Offset, len(part.Data))
		if !bytes.Equal(out[part.Offset:part.Offset+overlap], part.Data[:overlap]) {
			return nil, false
		}
		out = append(out, part.Data[overlap:]...)
	}
	parsed, err := viv.Read(out)
	if err != nil || len(parsed.Entries) != len(archive.Entries) {
		return nil, false
	}
	for index, entry := range archive.Entries {
		if parsed.Entries[index].Name != entry.Name || !bytes.Equal(parsed.Entries[index].Data, entry.Data) {
			return nil, false
		}
	}
	return out, true
}
