package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/lnikonl/nfs4viv/validation"
	"github.com/lnikonl/nfs4viv/viv"
)

type validationFlags struct {
	profile        string
	maxTextureSize int
}

func addValidationFlags(flags *flag.FlagSet) *validationFlags {
	options := &validationFlags{}
	flags.StringVar(&options.profile, "validate", "", "validate a game asset profile (car)")
	flags.IntVar(&options.maxTextureSize, "max-texture-size", 0, "optional maximum texture width/height; 0 accepts any size, 256 checks stock limits")
	return options
}

func (o *validationFlags) checkOptions() error {
	if o.profile != "" && o.profile != "car" {
		return fmt.Errorf("unknown validation profile %q (available: car)", o.profile)
	}
	if o.maxTextureSize < 0 {
		return fmt.Errorf("max-texture-size cannot be negative")
	}
	if o.maxTextureSize != 0 && o.profile == "" {
		return fmt.Errorf("--max-texture-size requires --validate car")
	}
	return nil
}

func (o *validationFlags) check(archive *viv.File) error {
	if err := o.checkOptions(); err != nil {
		return err
	}
	if o.profile == "" {
		return nil
	}
	report, err := validation.Check(o.profile, archive, validation.Options{MaxTextureSize: o.maxTextureSize})
	if err != nil {
		return err
	}
	for _, issue := range report.Issues {
		fmt.Fprintf(os.Stderr, "%s: %s: %s\n", issue.Severity, issue.Entry, issue.Message)
	}
	if !report.Valid {
		return fmt.Errorf("%s validation failed", o.profile)
	}
	return nil
}

func vivValidate(args []string) error {
	flags := flag.NewFlagSet("viv validate", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	options := addValidationFlags(flags)
	options.profile = "car"
	jsonOutput := flags.Bool("json", false, "write the validation report as JSON")
	positional, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return fmt.Errorf("usage: nfs4viv viv validate INPUT.VIV [--validate car] [--max-texture-size N] [-json]")
	}
	if err := options.checkOptions(); err != nil {
		return err
	}
	archive, err := viv.ReadFile(positional[0])
	if err != nil {
		return err
	}
	report, err := validation.Check(options.profile, archive, validation.Options{MaxTextureSize: options.maxTextureSize})
	if err != nil {
		return err
	}
	if *jsonOutput {
		if err := writeJSON(report); err != nil {
			return err
		}
	} else {
		for _, issue := range report.Issues {
			fmt.Printf("%s: %s: %s\n", issue.Severity, issue.Entry, issue.Message)
		}
		fmt.Printf("%s: %d resources checked, valid=%t\n", report.Profile, report.Checked, report.Valid)
	}
	if !report.Valid {
		return fmt.Errorf("%s validation failed", report.Profile)
	}
	return nil
}
