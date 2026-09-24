// Package appinfo contains shared metadata for command-line utilities.
package appinfo

import (
	"fmt"
	"io"
)

// Version is overridden by release builds with -ldflags -X.
var Version = "dev"

// Copyright holds the project copyright banner.
const Copyright = "Copyright (c) nfs4.com"

// PrintVersion writes the program name, version and copyright banner.
func PrintVersion(output io.Writer, program string) {
	fmt.Fprintf(output, "%s %s\n%s\n", program, Version, Copyright)
}
