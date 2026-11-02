//go:build windows

package main

import (
	"fmt"
	"os"
)

// Placeholder until the tray lands; console mode works meanwhile.
func runTray(options) int {
	fmt.Fprintln(os.Stderr, "tray mode isn't built yet; use --console")
	return 1
}

func attachConsole() {}
