//go:build windows

package main

import (
	"os"
	"syscall"
	"unsafe"
)

// enableVirtualTerminalProcessing is the console mode flag that makes the
// Windows console interpret ANSI escape sequences instead of printing them
// literally. Windows Terminal sets it by itself, but Windows PowerShell 5.1,
// cmd.exe and other conhost hosts do not.
const enableVirtualTerminalProcessing = 0x0004

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode = kernel32.NewProc("SetConsoleMode")
)

// enableVirtualTerminal switches the console attached to file into VT mode so
// colorized output renders in PowerShell and cmd.exe. It reports whether ANSI
// escapes can be used on the handle.
func enableVirtualTerminal(file *os.File) bool {
	handle := syscall.Handle(file.Fd())
	var mode uint32
	if result, _, _ := procGetConsoleMode.Call(uintptr(handle), uintptr(unsafe.Pointer(&mode))); result == 0 {
		return false
	}
	if mode&enableVirtualTerminalProcessing != 0 {
		return true
	}
	if result, _, _ := procSetConsoleMode.Call(uintptr(handle), uintptr(mode|enableVirtualTerminalProcessing)); result == 0 {
		return false
	}
	return true
}
