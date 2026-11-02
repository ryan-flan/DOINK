//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"time"

	"golang.org/x/sys/windows"

	"github.com/ryan-flan/DOINK/companion/internal/autostart"
	"github.com/ryan-flan/DOINK/companion/internal/config"
	"github.com/ryan-flan/DOINK/companion/internal/discord"
	"github.com/ryan-flan/DOINK/companion/internal/runner"
	"github.com/ryan-flan/DOINK/companion/internal/savedvars"
	"github.com/ryan-flan/DOINK/companion/internal/settings"
	"github.com/ryan-flan/DOINK/companion/internal/state"
	"github.com/ryan-flan/DOINK/companion/internal/status"
	"github.com/ryan-flan/DOINK/companion/internal/tray"
	"github.com/ryan-flan/DOINK/companion/internal/updates"
)

const configTemplate = `# DOINK companion settings. Every key is optional; see config.example.toml.
# webhook_url = "https://discord.com/api/webhooks/..."
# wow_dir = 'C:\Program Files (x86)\World of Warcraft'
`

// runTray is the packaged Windows app: tray icon and menu, logs to
// doink.log next to the exe (there's no console to print to).
func runTray(o options) int {
	dir := appDir()
	logPath := filepath.Join(dir, "doink.log")
	level := slog.LevelInfo
	if o.verbose {
		level = slog.LevelDebug
	}
	var log *slog.Logger
	if f, err := openRotating(logPath, 1_000_000); err == nil {
		defer f.Close()
		log = slog.New(newLineHandler(f, "2006-01-02 15:04:05", level))
	} else {
		log = slog.New(newLineHandler(os.Stderr, "2006-01-02 15:04:05", level))
	}
	slog.SetDefault(log)

	if !tray.AcquireSingleInstance(`Local\DOINK-companion`) {
		tray.MessageBox("DOINK is already running. Look for its icon in the system tray (you may need to click ^ to see it).", false)
		return 0
	}

	var icon atomic.Pointer[tray.Tray]
	var window atomic.Pointer[settings.Window]
	st := status.New(func(newError string) {
		if t := icon.Load(); t != nil {
			t.Notify(newError)
		}
		if w := window.Load(); w != nil {
			w.Refresh()
		}
	})

	cfg, err := config.Load(o.config, o.dryRun)
	noWoW := errors.Is(err, config.ErrNoWoW)
	if err != nil && !noWoW { // e.g. a hand-edited config.toml that doesn't parse
		log.Error(err.Error())
		tray.MessageBox(err.Error(), true)
		return 1
	}
	stateFile, err := state.Load(cfg.StatePath, !cfg.DryRun)
	if err != nil {
		log.Error(err.Error())
		tray.MessageBox(err.Error(), true)
		return 1
	}
	st.NotifyUpdates = cfg.UpdateNotify

	checker := updates.NewChecker(
		func() (*updates.Info, error) {
			return updates.FetchLatest(nil, updates.LatestURL, version, version)
		},
		st.UpdateChecked, updates.FirstCheck, updates.Interval, updates.Retry)

	backend := &settings.Backend{
		ConfigPath: o.config,
		DryRun:     o.dryRun,
		Version:    version,
		Status:     st,
		Send: func(url string) error {
			pool := discord.NewPool(nil, false, version, log)
			return pool.Send(url, []discord.Embed{discord.ConnectionTestEmbed(version)})
		},
		AutostartGet: func() bool {
			on, _ := autostart.IsEnabled(autostart.Name, autostart.Command())
			return on
		},
		AutostartSet: func(on bool) error {
			return autostart.SetEnabled(on, autostart.Name, autostart.Command())
		},
		Open: func(what string) {
			switch what {
			case "log":
				open(log, logPath)
			case "folder":
				open(log, dir)
			case "config":
				openConfig(log, o.config)
			case "update":
				if u := st.Update(); u != nil && u.Newer() {
					open(log, u.URL)
				}
			}
		},
	}
	win := settings.NewWindow(backend)
	win.Fallback = func() { openConfig(log, o.config) }
	win.Icon = tray.Icon
	window.Store(win)

	t := tray.New(st, version, tray.Hooks{
		OnQuit: func() {
			if t := icon.Load(); t != nil {
				t.Quit()
			}
		},
		OnCheckUpdates: checker.CheckNow,
		OpenConfig:     win.Show,
		OpenLog:        func() { open(log, logPath) },
		OpenFolder:     func() { open(log, dir) },
	})
	icon.Store(t)
	st.OnNotice = t.Inform
	log.Info("DOINK companion v" + version + " started")

	ctx, cancel := context.WithCancel(context.Background())
	if cfg.UpdateCheck {
		checker.Start(ctx)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		reload := func() (*config.Config, error) { return config.Load(o.config, o.dryRun) }
		d := deps(cfg, stateFile, st, log)
		// After Run's Status.Watching, which would clear these.
		d.Started = func() {
			if noWoW {
				st.Failed(settings.ProblemNoWoW)
			} else if cfg.WebhookURL == "" && !anyGameWebhooks(cfg) && !cfg.DryRun {
				st.Failed(settings.ProblemNoWebhook)
			}
		}
		if err := runner.Run(ctx, d, o.config, reload); err != nil {
			log.Error(err.Error())
		}
	}()

	runtime.LockOSThread()
	if err := t.Run(); err != nil {
		log.Error("tray: " + err.Error())
		tray.MessageBox("DOINK couldn't create its tray icon: "+err.Error(), true)
	}
	win.Close()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		log.Warn("the watcher didn't stop within 5 s")
	}
	log.Info("DOINK companion stopped")
	return 0
}

func open(log *slog.Logger, path string) {
	if err := tray.OpenPath(path); err != nil {
		log.Warn(fmt.Sprintf("couldn't open %s: %v", path, err))
	}
}

// openConfig opens config.toml, creating it from a short template first.
func openConfig(log *slog.Logger, path string) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(path, []byte(configTemplate), 0o644); err != nil {
			log.Warn(fmt.Sprintf("couldn't create %s: %v", path, err))
		}
	}
	open(log, path)
}

// anyGameWebhooks reports whether a watched file has a webhooks table.
func anyGameWebhooks(cfg *config.Config) bool {
	for _, path := range cfg.SavedVariablesPaths {
		if file, err := os.ReadFile(path); err == nil && len(savedvars.ParseWebhooks(file)) > 0 {
			return true
		}
	}
	return false
}

// attachConsole gives console mode somewhere to write: doink.exe is a GUI
// program (no console of its own), so borrow the one it was started from,
// unless output is already redirected.
func attachConsole() {
	if h, err := windows.GetStdHandle(windows.STD_ERROR_HANDLE); err == nil && h != 0 && h != windows.InvalidHandle {
		return
	}
	attach := windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")
	const attachParentProcess = ^uintptr(0) // ATTACH_PARENT_PROCESS = (DWORD)-1
	if ok, _, _ := attach.Call(attachParentProcess & 0xFFFFFFFF); ok == 0 {
		return
	}
	name, _ := windows.UTF16PtrFromString("CONOUT$")
	h, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return
	}
	out := os.NewFile(uintptr(h), "CONOUT$")
	os.Stdout, os.Stderr = out, out
}
