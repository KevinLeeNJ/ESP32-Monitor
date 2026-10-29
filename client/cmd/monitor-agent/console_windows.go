//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	kernel32              = syscall.NewLazyDLL("kernel32.dll")
	user32                = syscall.NewLazyDLL("user32.dll")
	getConsoleWindow      = kernel32.NewProc("GetConsoleWindow")
	getConsoleProcessList = kernel32.NewProc("GetConsoleProcessList")
	showWindow            = user32.NewProc("ShowWindow")
)

// hideOwnConsole hides a console created solely for this process (for example
// when ShellExecute runs the Agent elevated) without touching a terminal that
// the user opened to run --collector-scan.
func hideOwnConsole() {
	hwnd, _, _ := getConsoleWindow.Call()
	if hwnd == 0 {
		return
	}
	var pids [2]uint32
	count, _, _ := getConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), 2)
	if count == 1 {
		showWindow.Call(hwnd, 0)
	}
}
