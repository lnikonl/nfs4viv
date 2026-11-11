//go:build !windows

package main

import "os"

// enableVirtualTerminal is a no-op outside Windows: terminals exposing a
// character device already understand ANSI escape sequences.
func enableVirtualTerminal(*os.File) bool {
	return true
}
