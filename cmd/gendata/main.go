// Command gendata writes the synthetic car fixtures into a folder so that
// nfs4viv can be tried out without any original game data. The generated
// archives contain only locally produced stand-in blobs.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"gitlab.babun.tv/nfs4/nfs4-tools/nfs4viv/cartest"
)

func main() {
	root := filepath.Join(".", "DATA", "CARS")
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	if err := cartest.Write(root, cartest.Cars()); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %d synthetic cars to %s\n", len(cartest.Cars()), root)
}
