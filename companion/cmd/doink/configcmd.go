package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/ryan-flan/DOINK/companion/internal/config"
	"github.com/ryan-flan/DOINK/companion/internal/discord"
	"github.com/ryan-flan/DOINK/companion/internal/settings"
)

// Settings `doink config set` accepts, by type. Live reload applies them
// within a poll except startupOnly, which need a restart.
var (
	configStrings = []string{"webhook_url", "forward_url", "wow_dir"}
	configBools   = []string{"dry_run", "wowhead", "realtime", "update_check", "update_notify"}
	configInts    = []string{"max_backlog"}
	configFloats  = []string{"poll_interval"}
)

// configView is what `doink config show` prints: the settings as written
// in config.toml (absent keys are absent: the reader knows the defaults),
// plus what the companion would do with them.
type configView struct {
	Path         string            `json:"path"`
	Values       map[string]any    `json:"values"`
	Found        []string          `json:"found"`         // DOINK.lua files it would watch
	WowDirs      []string          `json:"wow_dirs"`      // their WoW folders
	GameWebhooks string            `json:"game_webhooks"` // "all characters", "Flano Wren-Realm", ""
	Routes       map[string]string `json:"routes"`        // [webhooks]: event type -> URL
	Error        string            `json:"error"`         // config.Load's complaint, if any
}

// runConfig is `doink config show` and `doink config set key=value...`.
// It prints JSON (show) or nothing (set) on stdout and problems on
// stderr, one line, for the macOS app to show.
func runConfig(path string, args []string, stdout, stderr io.Writer) int {
	// config.Load logs ("no settings in ..."); keep stderr for errors.
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: doink config show | doink config set key=value...")
		return 2
	}
	switch args[0] {
	case "show":
		return showConfig(path, stdout, stderr)
	case "set":
		if err := setConfig(path, args[1:]); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		return 0
	}
	fmt.Fprintf(stderr, "unknown config command %q (show or set)\n", args[0])
	return 2
}

func showConfig(path string, stdout, stderr io.Writer) int {
	v := configView{Path: path, Values: map[string]any{}, Found: []string{}, WowDirs: []string{}, Routes: map[string]string{}}
	raw, err := readRaw(path)
	if err != nil {
		v.Error = err.Error()
	}
	for _, list := range [][]string{configStrings, configBools, configInts, configFloats} {
		for _, key := range list {
			if val, ok := raw[key]; ok {
				v.Values[key] = val
			}
		}
	}
	if table, ok := raw["webhooks"].(map[string]any); ok {
		for typ, url := range table {
			if s, ok := url.(string); ok {
				v.Routes[typ] = s
			}
		}
	}
	cfg, err := config.Load(path, false)
	if err != nil {
		v.Error = err.Error()
	}
	if cfg != nil {
		v.Found = append(v.Found, cfg.SavedVariablesPaths...)
		seen := map[string]bool{}
		for _, p := range cfg.SavedVariablesPaths {
			// <wow>/<flavor>/WTF/Account/<acct>/SavedVariables/DOINK.lua
			wow := p
			for range 6 {
				wow = filepath.Dir(wow)
			}
			if !seen[wow] {
				seen[wow] = true
				v.WowDirs = append(v.WowDirs, wow)
			}
		}
		v.GameWebhooks = settings.GameWebhooks(cfg.SavedVariablesPaths)
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	return 0
}

func readRaw(path string) (map[string]any, error) {
	raw := map[string]any{}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return raw, nil
	}
	if err != nil {
		return raw, err
	}
	_, err = toml.Decode(string(b), &raw)
	return raw, err
}

func contains(list []string, key string) bool {
	for _, k := range list {
		if k == key {
			return true
		}
	}
	return false
}

// parseSetting checks one key=value and returns it typed. An empty value
// removes the key (back to the default).
func parseSetting(arg string) (settings.KV, error) {
	key, value, ok := strings.Cut(arg, "=")
	key, value = strings.TrimSpace(key), strings.TrimSpace(value)
	if !ok || key == "" {
		return settings.KV{}, fmt.Errorf("expected key=value, got %q", arg)
	}
	if typ, ok := strings.CutPrefix(key, "webhooks."); ok {
		if !contains(config.EventTypes, typ) {
			return settings.KV{}, fmt.Errorf("%q isn't an event type (%s)", typ, strings.Join(config.EventTypes, ", "))
		}
		if value != "" && !discord.IsWebhookURL(value) {
			return settings.KV{}, errors.New("that doesn't look like a Discord webhook URL. In Discord: Server Settings → Integrations → Webhooks → Copy Webhook URL")
		}
		return settings.KV{Key: key, Value: value}, nil
	}
	if value == "" {
		return settings.KV{Key: key}, nil
	}
	switch {
	case key == "webhook_url":
		if !discord.IsWebhookURL(value) {
			return settings.KV{}, errors.New("that doesn't look like a Discord webhook URL. In Discord: Server Settings → Integrations → Webhooks → Copy Webhook URL")
		}
	case key == "forward_url":
		if !config.IsForwardURL(value) {
			return settings.KV{}, errors.New("the forward URL must start with http:// or https://")
		}
	case key == "wow_dir":
		fixed, ok := settings.WowFolder(strings.Trim(value, `"'`))
		if !ok {
			return settings.KV{}, errors.New("no World of Warcraft install with the DOINK addon was found there. Choose the folder that contains _forever_")
		}
		value = fixed
	case contains(configBools, key):
		b, err := strconv.ParseBool(value)
		if err != nil {
			return settings.KV{}, fmt.Errorf("%s must be true or false", key)
		}
		return settings.KV{Key: key, Value: b}, nil
	case contains(configInts, key):
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			return settings.KV{}, fmt.Errorf("%s must be a whole number, 0 or more", key)
		}
		return settings.KV{Key: key, Value: n}, nil
	case contains(configFloats, key):
		f, err := strconv.ParseFloat(value, 64)
		if err != nil || f <= 0 {
			return settings.KV{}, fmt.Errorf("%s must be a number above 0", key)
		}
		return settings.KV{Key: key, Value: f}, nil
	case !contains(configStrings, key):
		return settings.KV{}, fmt.Errorf("unknown setting %q", key)
	}
	return settings.KV{Key: key, Value: value}, nil
}

// setConfig writes the settings, then loads the file the way the
// companion will; if that fails (other than "WoW not found"), the old
// file is put back.
func setConfig(path string, args []string) error {
	if len(args) == 0 {
		return errors.New("nothing to set: doink config set key=value")
	}
	var kvs []settings.KV
	for _, arg := range args {
		kv, err := parseSetting(arg)
		if err != nil {
			return err
		}
		kvs = append(kvs, kv)
	}
	old, readErr := os.ReadFile(path)
	if err := settings.Set(path, kvs); err != nil {
		return err
	}
	if _, err := config.Load(path, false); err != nil && !errors.Is(err, config.ErrNoWoW) {
		if readErr == nil {
			_ = os.WriteFile(path, old, 0o644)
		} else {
			_ = os.Remove(path)
		}
		return err
	}
	return nil
}
