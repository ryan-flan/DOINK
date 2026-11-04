package config

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
)

type capture struct {
	mu   sync.Mutex
	recs []string
}

func (c *capture) Enabled(context.Context, slog.Level) bool { return true }
func (c *capture) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.recs = append(c.recs, r.Level.String()+" "+r.Message)
	return nil
}
func (c *capture) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *capture) WithGroup(string) slog.Handler      { return c }

func captureLogs(t *testing.T) *capture {
	c := &capture{}
	old := Log
	Log = slog.New(c)
	t.Cleanup(func() { Log = old })
	return c
}

const hook = "https://discord.com/api/webhooks/1/abc"

func TestUnknownKeysAreWarnedAbout(t *testing.T) {
	logs := captureLogs(t)
	path := setup(t, "webhook-url = \""+hook+"\"\nmax_backlog = 3\n[notifiers]\nloot = true\n")
	if _, err := Load(path, true); err != nil && !errors.Is(err, ErrNoWoW) {
		t.Fatal(err)
	}
	joined := strings.Join(logs.recs, "\n")
	if !strings.Contains(joined, `WARN config.toml: unknown setting "webhook-url" is ignored`) ||
		strings.Contains(joined, `"max_backlog"`) || strings.Contains(joined, `"notifiers" is ignored`) {
		t.Errorf("logs %q", logs.recs)
	}
}

// setup writes config.toml (if body != "") in a temp dir and isolates
// discovery from the real machine by pointing HOME at an empty dir.
func setup(t *testing.T, body string) string {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if body != "" {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func fakeWow(t *testing.T) string {
	wow := t.TempDir()
	os.MkdirAll(filepath.Join(wow, "_forever_", "Interface", "AddOns", "DOINK"), 0o755)
	os.MkdirAll(filepath.Join(wow, "_forever_", "WTF", "Account", "A", "SavedVariables"), 0o755)
	return wow
}

func TestDefaults(t *testing.T) {
	sv := filepath.Join(t.TempDir(), "DOINK.lua")
	path := setup(t, "savedvariables_path = '"+sv+"'\n")
	cfg, err := Load(path, false)
	if err != nil {
		t.Fatal(err)
	}
	want := &Config{
		SavedVariablesPaths: []string{sv},
		StatePath:           filepath.Join(filepath.Dir(path), "state.json"),
		PollInterval:        2.0, MaxBacklog: 10, Wowhead: true, UpdateCheck: true, Realtime: true,
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("got %+v", cfg)
	}
}

func TestEveryKey(t *testing.T) {
	sv := filepath.Join(t.TempDir(), "DOINK.lua")
	path := setup(t, "savedvariables_path = '"+sv+"'\nwebhook_url = \""+hook+"\"\ndry_run = true\n"+
		"poll_interval = 5\nmax_backlog = 0\nwowhead = false\nupdate_check = false\nupdate_notify = true\nunknown = 1\n")
	cfg, err := Load(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WebhookURL != hook || !cfg.DryRun || cfg.PollInterval != 5 || cfg.MaxBacklog != 0 ||
		cfg.Wowhead || cfg.UpdateCheck || !cfg.UpdateNotify {
		t.Errorf("got %+v", cfg)
	}
}

func TestDryRunFlag(t *testing.T) {
	sv := filepath.Join(t.TempDir(), "DOINK.lua")
	cfg, err := Load(setup(t, "savedvariables_path = '"+sv+"'\n"), true)
	if err != nil || !cfg.DryRun {
		t.Errorf("err %v cfg %+v", err, cfg)
	}
}

func TestPathPrecedence(t *testing.T) {
	wow := fakeWow(t)
	sv := filepath.Join(t.TempDir(), "DOINK.lua")
	// savedvariables_path beats wow_dir.
	cfg, err := Load(setup(t, "wow_dir = '"+wow+"'\nsavedvariables_path = '"+sv+"'\n"), false)
	if err != nil || !reflect.DeepEqual(cfg.SavedVariablesPaths, []string{sv}) {
		t.Errorf("err %v paths %v", err, cfg.SavedVariablesPaths)
	}
	// wow_dir beats discovery.
	cfg, err = Load(setup(t, "wow_dir = '"+wow+"'\n"), false)
	want := []string{filepath.Join(wow, "_forever_", "WTF", "Account", "A", "SavedVariables", "DOINK.lua")}
	if err != nil || !reflect.DeepEqual(cfg.SavedVariablesPaths, want) || cfg.WowDir != wow {
		t.Errorf("err %v cfg %+v", err, cfg)
	}
}

func TestDiscoveryInAWinePrefix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux discovery")
	}
	path := setup(t, "")
	home := os.Getenv("HOME")
	wow := filepath.Join(home, "Games", "battlenet", "drive_c", "Program Files (x86)", "World of Warcraft")
	os.MkdirAll(filepath.Join(wow, "_forever_", "Interface", "AddOns", "DOINK"), 0o755)
	os.MkdirAll(filepath.Join(wow, "_forever_", "WTF", "Account", "A", "SavedVariables"), 0o755)
	logs := captureLogs(t)
	cfg, err := Load(path, false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range cfg.SavedVariablesPaths {
		found = found || strings.HasPrefix(p, wow)
	}
	if !found {
		t.Errorf("paths %v", cfg.SavedVariablesPaths)
	}
	if len(logs.recs) != 1 || logs.recs[0] != "INFO no settings in config.toml; using defaults" {
		t.Errorf("logs %q", logs.recs)
	}
}

func TestNoWoW(t *testing.T) {
	wow := t.TempDir() // no flavor with the addon
	path := setup(t, "wow_dir = '"+wow+"'\n")
	cfg, err := Load(path, false)
	if !errors.Is(err, ErrNoWoW) {
		t.Fatalf("err %v", err)
	}
	want := "couldn't find World of Warcraft with the DOINK addon installed. Set wow_dir or savedvariables_path in " + path
	if err.Error() != want {
		t.Errorf("message %q", err)
	}
	if cfg == nil || len(cfg.SavedVariablesPaths) != 0 {
		t.Errorf("cfg %+v: tray mode starts with no watchers", cfg)
	}
}

func TestBadWebhook(t *testing.T) {
	sv := filepath.Join(t.TempDir(), "DOINK.lua")
	path := setup(t, "savedvariables_path = '"+sv+"'\nwebhook_url = \"https://evil.example/x\"\n")
	_, err := Load(path, false)
	if err == nil || err.Error() != path+": webhook_url doesn't look like a Discord webhook URL" {
		t.Errorf("err %v", err)
	}
}

func TestParseAndTypeErrors(t *testing.T) {
	for _, body := range []string{"this is = = not toml", "poll_interval = \"fast\"", "dry_run = 1", "max_backlog = -1"} {
		if _, err := Load(setup(t, body), false); err == nil || errors.Is(err, ErrNoWoW) {
			t.Errorf("%q: err %v", body, err)
		}
	}
}

func TestIgnoredKeysAreLogged(t *testing.T) {
	sv := filepath.Join(t.TempDir(), "DOINK.lua")
	path := setup(t, "savedvariables_path = '"+sv+"'\n[notifiers]\nloot = false\n")
	logs := captureLogs(t)
	if _, err := Load(path, false); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(logs.recs, "\n")
	if !strings.Contains(joined, "WARN [notifiers] is ignored now") {
		t.Errorf("logs %q", logs.recs)
	}
}

func TestRealtime(t *testing.T) {
	sv := filepath.Join(t.TempDir(), "DOINK.lua")
	base := "savedvariables_path = '" + sv + "'\n"
	for body, want := range map[string]bool{base: true, base + "realtime = false\n": false, base + "realtime = true\n": true} {
		cfg, err := Load(setup(t, body), false)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Realtime != want {
			t.Errorf("%q: realtime %v", body, cfg.Realtime)
		}
	}
	if _, err := Load(setup(t, base+"realtime = 'yes'\n"), false); err == nil {
		t.Error("a string was accepted")
	}
}

func TestNativePath(t *testing.T) {
	if runtime.GOOS == "windows" {
		if NativePath(`C:\x\y`) != `C:\x\y` {
			t.Error("changed on Windows")
		}
		return
	}
	t.Setenv("HOME", t.TempDir())
	if _, err := os.Stat("/mnt/c/x/y"); err == nil {
		t.Skip("/mnt/c/x/y exists here")
	}
	if got := NativePath(`C:\x\y`); got != "/mnt/c/x/y" {
		t.Errorf("got %q", got)
	}
	if got := NativePath(`D:/Games\World of Warcraft`); got != "/mnt/d/Games/World of Warcraft" {
		t.Errorf("got %q", got)
	}
	if got := NativePath("/home/you/wow"); got != "/home/you/wow" {
		t.Errorf("got %q", got)
	}
	prefix := filepath.Join(os.Getenv("HOME"), "Games", "battlenet", "drive_c")
	os.MkdirAll(filepath.Join(prefix, "x", "y"), 0o755)
	if got := NativePath(`C:\x\y`); got != filepath.Join(prefix, "x", "y") {
		t.Errorf("got %q", got)
	}
}
