package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lnikonl/nfs4viv/resources"
)

func resourceFormat(name string) string {
	return strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
}

func resourceCommand(format string, args []string) error {
	if format == "fce" && len(args) > 0 && args[0] == "create" {
		return createFCECommand(args[1:])
	}
	if len(args) == 0 || args[0] != "extract" && args[0] != "pack" {
		return fmt.Errorf("usage: nfs4viv %s extract INPUT [-o DIR] | pack DIR [-o FILE]", format)
	}
	command := args[0]
	flags := flag.NewFlagSet(format+" "+command, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	output := flags.String("o", "", "output directory or resource file")
	limit := flags.Int("max-texture-size", 0, "optional maximum texture width/height (default unlimited)")
	positional, err := parseFlags(flags, args[1:])
	if err != nil {
		return err
	}
	if len(positional) != 1 || *limit < 0 {
		return fmt.Errorf("usage: nfs4viv %s %s INPUT [-o OUTPUT] [--max-texture-size N]", format, command)
	}
	input := positional[0]
	if command == "extract" {
		data, err := os.ReadFile(input)
		if err != nil {
			return err
		}
		if err := resources.Validate(format, data, *limit); err != nil {
			return fmt.Errorf("%s: %w", input, err)
		}
		out := *output
		if out == "" {
			out = input + ".d"
		}
		if err := extractResourceDirectory(format, data, out); err != nil {
			return err
		}
		fmt.Printf("extracted %s to %s\n", input, out)
		return nil
	}
	input, err = filepath.Abs(input)
	if err != nil {
		return err
	}
	out := *output
	if out == "" {
		out = strings.TrimSuffix(input, ".d")
		if out == input {
			out += "." + format
		}
	}
	out, err = filepath.Abs(out)
	if err != nil {
		return err
	}
	data, err := packResourceDirectory(format, input, out)
	if err != nil {
		return err
	}
	if err := resources.Validate(format, data, *limit); err != nil {
		return err
	}
	if err := writeFileAtomic(out, data); err != nil {
		return err
	}
	fmt.Printf("packed %s into %s (%d bytes)\n", input, out, len(data))
	return nil
}

func createFCECommand(args []string) error {
	flags := flag.NewFlagSet("fce create", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	output := flags.String("o", "", "output FCE (default INPUT with .fce extension)")
	damaged := flags.String("damaged", "", "optional damaged OBJ with matching topology")
	positional, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return fmt.Errorf("usage: nfs4viv fce create INPUT.OBJ [-o OUTPUT.FCE] [--damaged DAMAGED.OBJ]")
	}
	input := positional[0]
	model, err := os.ReadFile(input)
	if err != nil {
		return err
	}
	var damage []byte
	if *damaged != "" {
		damage, err = os.ReadFile(*damaged)
		if err != nil {
			return err
		}
	}
	data, err := resources.CreateFCE(model, damage)
	if err != nil {
		return err
	}
	out := *output
	if out == "" {
		out = strings.TrimSuffix(input, filepath.Ext(input)) + ".fce"
	}
	if err := writeFileAtomic(out, data); err != nil {
		return err
	}
	fmt.Printf("created %s from %s (%d bytes)\n", out, input, len(data))
	return nil
}

func extractResourceDirectory(format string, data []byte, directory string) error {
	files, err := resources.Extract(format, data)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := writeFileAtomic(filepath.Join(directory, filepath.FromSlash(name)), files[name]); err != nil {
			return err
		}
	}
	return nil
}

func packResourceDirectory(format, directory, output string) ([]byte, error) {
	files := map[string][]byte{}
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if path == output {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", path)
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	if _, ok := files[resources.ManifestName]; !ok {
		return nil, fmt.Errorf("%s: missing %s; use %s extract to create a resource project", directory, resources.ManifestName, format)
	}
	return resources.Pack(format, files)
}
