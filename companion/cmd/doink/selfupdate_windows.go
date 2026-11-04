//go:build windows

package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/ryan-flan/DOINK/companion/internal/status"
	"github.com/ryan-flan/DOINK/companion/internal/updates"
)

// selfUpdater is the tray's and the settings window's "Update" action:
// download and verify the release, swap doink.exe by renaming, start the
// new copy and quit. The new copy waits for this one's mutex, then
// deletes doink.exe.old.
type selfUpdater struct {
	in   *updates.Installer
	st   *status.Status
	log  *slog.Logger
	tell func(string) // a tray notification
	quit func()       // the normal tray shutdown
	busy atomic.Bool
}

// Install runs on its own goroutine. It returns only on failure (and
// when nothing is installable); on success the process quits.
func (u *selfUpdater) Install() {
	if !u.busy.CompareAndSwap(false, true) {
		return
	}
	defer u.busy.Store(false)
	defer u.st.Installing("")

	info := u.st.Update()
	if info == nil || !info.Installable() {
		return
	}
	fail := func(err error) {
		u.log.Error("update failed: " + err.Error())
		u.tell("Update failed: " + err.Error() + ". You can download it from the releases page: tray menu, Open download page.")
	}
	// Swap and restart act on doink.exe in the app folder, so they must
	// be the running program.
	if exe, err := os.Executable(); err == nil && !strings.EqualFold(filepath.Base(exe), updates.ExeName) {
		fail(fmt.Errorf("DOINK is running as %s; rename it to doink.exe to update from here", filepath.Base(exe)))
		return
	}

	u.tell("Downloading DOINK v" + info.Latest + "…")
	if err := u.in.Download(info, u.st.Installing); err != nil {
		fail(err)
		return
	}
	u.st.Installing("Installing v" + info.Latest + "…")
	if err := u.in.Swap(); err != nil {
		fail(err)
		return
	}
	cmd := exec.Command(filepath.Join(u.in.Dir, updates.ExeName), restartArgs(os.Args[1:], u.in.Version)...)
	// No cmd.Dir: the new copy keeps this one's working directory, so a
	// relative --config still means the same file.
	if err := cmd.Start(); err != nil {
		if rerr := u.in.Rollback(); rerr != nil {
			u.log.Error("update: rollback failed: " + rerr.Error())
		}
		fail(fmt.Errorf("couldn't start the new version: %w", err))
		return
	}
	u.log.Info(fmt.Sprintf("update: v%s → v%s, restarting", u.in.Version, info.Latest))
	u.quit()
}

// restartArgs is args without any --updated-from, plus one naming the
// version being replaced.
func restartArgs(args []string, version string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := strings.TrimLeft(args[i], "-")
		if a == "updated-from" {
			i++ // its value
			continue
		}
		if strings.HasPrefix(a, "updated-from=") && strings.HasPrefix(args[i], "-") {
			continue
		}
		out = append(out, args[i])
	}
	return append(out, "--updated-from", version)
}
