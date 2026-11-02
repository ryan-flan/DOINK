// Package autostart is "Start with Windows": one string value under
// HKCU\Software\Microsoft\Windows\CurrentVersion\Run. Per-user, no admin
// rights, and listed in Task Manager > Startup apps, where it can also be
// switched off. Nothing else is written.
package autostart

import (
	"os"
	"runtime"
)

// Name is the Run value's default name.
const Name = "DOINK"

// Available reports whether this OS has a Run key.
func Available() bool { return runtime.GOOS == "windows" }

// Command is what the Run value holds: this executable, quoted.
func Command() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return `"` + exe + `"`
}
