package settings

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/ryan-flan/DOINK/companion/internal/config"
	"github.com/ryan-flan/DOINK/companion/internal/discord"
	"github.com/ryan-flan/DOINK/companion/internal/discover"
	"github.com/ryan-flan/DOINK/companion/internal/savedvars"
	"github.com/ryan-flan/DOINK/companion/internal/status"
)

// The two startup problems the window can fix. The tray sets them; Save
// clears them once the setting that fixes them is saved.
const (
	ProblemNoWoW     = "World of Warcraft not found. Open Settings and choose your WoW folder"
	ProblemNoWebhook = "no webhook yet. In game: /doink webhook <url>, or paste one in Settings"
)

// Backend is what the window shows and does. Everything here is plain Go,
// so it is tested on every platform; the window only renders it.
type Backend struct {
	ConfigPath string
	DryRun     bool
	Version    string
	Status     *status.Status
	// Send posts the connection-test embed to url.
	Send func(url string) error
	// Autostart is "Start with Windows"; nil where there is none.
	AutostartGet func() bool
	AutostartSet func(on bool) error
	// Open opens "log", "folder", "config" or "update" (the release page).
	Open func(what string)
	// Install downloads and installs the newer version (the tray's
	// "Update to vX"); nil where updates can't install themselves.
	Install func()
}

// Post is one line of the recent-posts list.
type Post struct {
	At   string `json:"at"`
	Who  string `json:"who"`
	What string `json:"what"`
}

// Live is the part of the view that changes while the window is open.
type Live struct {
	Problem string `json:"problem"` // "" when all is well
	Summary string `json:"summary"`
	Update  string `json:"update"` // a newer version, or ""
	// Installable: Update can be installed from here; Installing is the
	// progress line while that runs.
	Installable bool   `json:"installable"`
	Installing  string `json:"installing"`
	Recent      []Post `json:"recent"`
}

// View is everything the window shows.
type View struct {
	Live
	Version     string   `json:"version"`
	ConfigPath  string   `json:"configPath"`
	ConfigError string   `json:"configError"` // config.toml doesn't load
	Webhook     string   `json:"webhook"`     // webhook_url in config.toml
	GameWebhook string   `json:"gameWebhook"` // "" or who has one set in game
	WowDir      string   `json:"wowDir"`      // wow_dir in config.toml
	SVPathSet   bool     `json:"svPathSet"`   // savedvariables_path overrides wow_dir
	Found       []string `json:"found"`       // the DOINK.lua files being watched
	DryRun      bool     `json:"dryRun"`
	Autostart   *bool    `json:"autostart"` // nil: not available
}

// SaveResult is Save's answer: Error for the user, or the fresh view.
type SaveResult struct {
	Error string `json:"error"`
	View  View   `json:"view"`
}

// Live reads the status.
func (b *Backend) Live() Live {
	var l Live
	if b.Status == nil {
		return l
	}
	l.Problem = b.Status.Error()
	summary := strings.TrimPrefix(b.Status.Summary(), "DOINK: ")
	if i := strings.Index(summary, " · v"); i >= 0 {
		summary = summary[:i] // the update is shown on its own
	}
	l.Summary = summary
	if u := b.Status.Update(); u != nil && u.Newer() {
		l.Update = u.Latest
		l.Installable = u.Installable() && b.Install != nil
	}
	l.Installing = b.Status.Install()
	for _, p := range b.Status.Recent() {
		l.Recent = append(l.Recent, Post{At: p.At.Local().Format("Jan 2 15:04"), Who: p.Who, What: p.What})
	}
	return l
}

// View reads config.toml and the watched files.
func (b *Backend) View() View {
	v := View{Live: b.Live(), Version: b.Version, ConfigPath: b.ConfigPath, DryRun: b.DryRun}
	if b.AutostartGet != nil {
		on := b.AutostartGet()
		v.Autostart = &on
	}
	raw := map[string]any{}
	if _, err := toml.DecodeFile(b.ConfigPath, &raw); err != nil && !errors.Is(err, fs.ErrNotExist) {
		v.ConfigError = err.Error()
		return v
	}
	v.Webhook, _ = raw["webhook_url"].(string)
	v.WowDir, _ = raw["wow_dir"].(string)
	_, v.SVPathSet = raw["savedvariables_path"]

	cfg, err := config.Load(b.ConfigPath, b.DryRun)
	if err != nil && !errors.Is(err, config.ErrNoWoW) {
		v.ConfigError = err.Error()
		return v
	}
	v.Found = cfg.SavedVariablesPaths
	v.GameWebhook = gameWebhooks(cfg.SavedVariablesPaths)
	return v
}

// gameWebhooks describes the webhooks set in game, which win over
// config.toml's: "all characters", "Flano Wren-Realm", "3 characters".
func gameWebhooks(paths []string) string {
	all := false
	chars := map[string]bool{}
	for _, path := range paths {
		file, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for key := range savedvars.ParseWebhooks(file) {
			if key == "*" {
				all = true
			} else {
				chars[key] = true
			}
		}
	}
	switch {
	case all:
		return "all characters"
	case len(chars) == 1:
		for name := range chars {
			return name
		}
	case len(chars) > 1:
		names := make([]string, 0, len(chars))
		for name := range chars {
			names = append(names, name)
		}
		sort.Strings(names)
		return fmt.Sprintf("%d characters", len(names))
	}
	return ""
}

// Save validates and writes webhook_url and wow_dir ("" removes either).
// The companion's live reload picks the file up within a poll.
func (b *Backend) Save(webhook, wowDir string) SaveResult {
	webhook = strings.TrimSpace(webhook)
	wowDir = strings.Trim(strings.TrimSpace(wowDir), `"'`)
	if webhook != "" && !discord.IsWebhookURL(webhook) {
		return SaveResult{Error: "That doesn't look like a Discord webhook URL. In Discord: Server Settings → Integrations → Webhooks → Copy Webhook URL."}
	}
	if wowDir != "" {
		fixed, ok := WowFolder(wowDir)
		if !ok {
			return SaveResult{Error: "No World of Warcraft install with the DOINK addon was found there. Choose the folder that contains _forever_."}
		}
		wowDir = fixed
	}
	if err := Set(b.ConfigPath, []KV{{"webhook_url", webhook}, {"wow_dir", wowDir}}); err != nil {
		return SaveResult{Error: err.Error()}
	}
	if b.Status != nil {
		switch b.Status.Error() {
		case ProblemNoWebhook:
			if webhook != "" {
				b.Status.OK()
			}
		case ProblemNoWoW:
			if wowDir != "" {
				b.Status.OK()
			}
		}
	}
	return SaveResult{View: b.View()}
}

// WowFolder accepts the WoW folder, or a folder inside it that people
// pick by mistake (_forever_, its Interface or AddOns), and returns the
// WoW folder if DOINK is installed there.
func WowFolder(dir string) (string, bool) {
	for i := 0; i < 4 && dir != ""; i++ {
		if len(discover.InWowDir(dir)) > 0 {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", false
}

// Test posts the connection-test message to webhook, or to the webhook
// DOINK would use (config.toml, then the in-game one for all characters)
// when the box is empty. "" means it worked.
func (b *Backend) Test(webhook string) string {
	url := strings.TrimSpace(webhook)
	if url == "" {
		v := b.View()
		url = v.Webhook
		if url == "" {
			for _, path := range v.Found {
				if file, err := os.ReadFile(path); err == nil {
					if u := savedvars.ParseWebhooks(file)["*"]; u != "" {
						url = u
						break
					}
				}
			}
		}
	}
	if url == "" {
		return "Paste a webhook URL first."
	}
	if !discord.IsWebhookURL(url) {
		return "That doesn't look like a Discord webhook URL."
	}
	if b.Send == nil {
		return "Sending isn't available."
	}
	if err := b.Send(url); err != nil {
		return "Discord didn't accept it: " + err.Error()
	}
	return ""
}

// SetAutostart switches "Start with Windows"; "" means it worked.
func (b *Backend) SetAutostart(on bool) string {
	if b.AutostartSet == nil {
		return "Not available on this system."
	}
	if err := b.AutostartSet(on); err != nil {
		return err.Error()
	}
	return ""
}
