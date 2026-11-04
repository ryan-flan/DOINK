// Package config loads config.toml. Every setting is optional: with no file
// at all, the SavedVariables files are discovered and the webhook comes
// from the game.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/ryan-flan/DOINK/companion/internal/discord"
	"github.com/ryan-flan/DOINK/companion/internal/discover"
)

// Log receives config messages; nil means slog.Default().
var Log *slog.Logger

func logger() *slog.Logger {
	if Log != nil {
		return Log
	}
	return slog.Default()
}

// Config is the companion's settings.
type Config struct {
	SavedVariablesPaths []string
	StatePath           string
	WowDir              string
	WebhookURL          string // fallback when the game hasn't set one
	DryRun              bool
	PollInterval        float64 // seconds
	MaxBacklog          int
	Wowhead             bool // look up looted items on Wowhead for icon + stats
	UpdateCheck         bool // daily GET to GitHub for the latest release version
	UpdateNotify        bool // also pop a notification (the tray tooltip always shows it)
	Realtime            bool // read the screenshots the addon takes for instant posts
}

// knownKeys are the settings Load reads; anything else gets a warning.
var knownKeys = []string{"savedvariables_path", "wow_dir", "webhook_url", "dry_run", "poll_interval",
	"max_backlog", "wowhead", "update_check", "update_notify", "realtime", "notifiers"}

// ErrNoWoW: no SavedVariables file to watch.
var ErrNoWoW = errors.New("couldn't find World of Warcraft with the DOINK addon installed")

var windowsPath = regexp.MustCompile(`^([A-Za-z]):[\\/](.*)$`)

// NativePath accepts Windows paths everywhere. Off Windows, C:\x becomes
// /mnt/c/x if that exists (WSL), else the same path inside a Wine prefix's
// drive if one has it, else the /mnt form.
func NativePath(raw string) string {
	if runtime.GOOS == "windows" {
		return raw
	}
	m := windowsPath.FindStringSubmatch(raw)
	if m == nil {
		return raw
	}
	parts := strings.FieldsFunc(m[2], func(r rune) bool { return r == '\\' || r == '/' })
	rest := filepath.Join(parts...)
	mnt := filepath.Join("/mnt", strings.ToLower(m[1]), rest)
	if exists(mnt) {
		return mnt
	}
	for _, root := range discover.DriveRoots() {
		if strings.HasPrefix(root, "/mnt/") {
			continue // the /mnt form was tried above
		}
		if candidate := filepath.Join(root, rest); exists(candidate) {
			return candidate
		}
	}
	return mnt
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Load reads path. A missing file means defaults. dryRun (the flag) is
// or-ed with the file's dry_run.
func Load(path string, dryRun bool) (*Config, error) {
	raw := map[string]any{}
	if _, err := toml.DecodeFile(path, &raw); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(raw) == 0 {
		logger().Info(fmt.Sprintf("no settings in %s; using defaults", filepath.Base(path)))
	}
	if _, ok := raw["notifiers"]; ok {
		logger().Warn(fmt.Sprintf("[notifiers] is ignored now. Toggle notifiers in game: /doink enable|disable <type> (in %s)", filepath.Base(path)))
	}
	for _, key := range slices.Sorted(maps.Keys(raw)) {
		if !slices.Contains(knownKeys, key) {
			logger().Warn(fmt.Sprintf("%s: unknown setting %q is ignored (a typo?)", filepath.Base(path), key))
		}
	}

	cfg := &Config{
		StatePath:    filepath.Join(filepath.Dir(path), "state.json"),
		PollInterval: 2.0,
		MaxBacklog:   10,
		Wowhead:      true,
		UpdateCheck:  true,
	}
	var err error
	str := func(key string) string {
		if err != nil {
			return ""
		}
		v, present := raw[key]
		if !present {
			return ""
		}
		s, ok := v.(string)
		if !ok {
			err = fmt.Errorf("%s: %s must be a string", path, key)
		}
		return s
	}
	boolean := func(key string, def bool) bool {
		if err != nil {
			return def
		}
		v, present := raw[key]
		if !present {
			return def
		}
		b, ok := v.(bool)
		if !ok {
			err = fmt.Errorf("%s: %s must be true or false", path, key)
		}
		return b
	}
	number := func(key string, def float64) float64 {
		if err != nil {
			return def
		}
		switch v := raw[key].(type) {
		case nil:
			return def
		case int64:
			return float64(v)
		case float64:
			return v
		}
		err = fmt.Errorf("%s: %s must be a number", path, key)
		return def
	}

	svPath := str("savedvariables_path")
	wowDir := str("wow_dir")
	cfg.WebhookURL = str("webhook_url")
	cfg.DryRun = boolean("dry_run", false) || dryRun
	cfg.PollInterval = number("poll_interval", 2.0)
	backlog := number("max_backlog", 10)
	cfg.Wowhead = boolean("wowhead", true)
	cfg.UpdateCheck = boolean("update_check", true)
	cfg.UpdateNotify = boolean("update_notify", false)
	cfg.Realtime = boolean("realtime", true)
	if err != nil {
		return nil, err
	}
	if backlog != math.Trunc(backlog) || backlog < 0 {
		return nil, fmt.Errorf("%s: max_backlog must be a whole number, 0 or more", path)
	}
	cfg.MaxBacklog = int(backlog)
	if cfg.PollInterval <= 0 {
		return nil, fmt.Errorf("%s: poll_interval must be more than 0", path)
	}
	if cfg.WebhookURL != "" && !discord.IsWebhookURL(cfg.WebhookURL) {
		return nil, fmt.Errorf("%s: webhook_url doesn't look like a Discord webhook URL", path)
	}

	switch {
	case svPath != "":
		cfg.SavedVariablesPaths = []string{NativePath(svPath)}
	case wowDir != "":
		cfg.WowDir = NativePath(wowDir)
		cfg.SavedVariablesPaths = discover.InWowDir(cfg.WowDir)
	default:
		cfg.SavedVariablesPaths = discover.SavedVariables(discover.DriveRoots())
	}
	if len(cfg.SavedVariablesPaths) == 0 {
		return cfg, fmt.Errorf("%w. Set wow_dir or savedvariables_path in %s", ErrNoWoW, path)
	}
	return cfg, nil
}
