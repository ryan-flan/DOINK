//go:build !windows

package main

import (
	"fmt"
	"os"
)

func runTray(options) int {
	fmt.Fprintln(os.Stderr, "tray mode needs Windows")
	return 1
}

func attachConsole() {}
