// Package runner is the watch-and-post loop: read the SavedVariables file,
// post what hasn't been posted, remember it.
package runner

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ryan-flan/DOINK/companion/internal/config"
	"github.com/ryan-flan/DOINK/companion/internal/discord"
	"github.com/ryan-flan/DOINK/companion/internal/savedvars"
	"github.com/ryan-flan/DOINK/companion/internal/state"
	"github.com/ryan-flan/DOINK/companion/internal/status"
	"github.com/ryan-flan/DOINK/companion/internal/watcher"
	"github.com/ryan-flan/DOINK/companion/internal/wowhead"
)

// maxRetryDelay caps the back-off after failed posts.
const maxRetryDelay = 60 * time.Second

// Deps is everything the loop works with.
type Deps struct {
	Config  *config.Config // pointer: reloaded live
	State   *state.State
	Sender  discord.Sender
	Status  *status.Status
	Items   *wowhead.Cache // nil when wowhead is off
	Log     *slog.Logger
	Version string
	// Started, if set, is called once Run is watching its files (after
	// Status.Watching), so startup warnings set there aren't cleared by it.
	Started func()
}

// WebhookFor picks a character's webhook: its own in-game one, else the
// account-wide in-game one, else config.toml's.
func WebhookFor(key string, gameHooks map[string]string, cfg *config.Config) string {
	if url := gameHooks[key]; url != "" {
		return url
	}
	if url := gameHooks["*"]; url != "" {
		return url
	}
	return cfg.WebhookURL
}

// item is Wowhead's details for a loot or mount event with an integer
// item id.
func item(d Deps, e savedvars.Event) *wowhead.Item {
	if d.Items == nil || (e.Type != "loot" && e.Type != "mount") {
		return nil
	}
	id, ok := e.Data["item_id"].(float64)
	if !ok || id != math.Trunc(id) || id > math.MaxInt32 {
		return nil
	}
	return d.Items.Lookup(int(id))
}

// PostEvents posts events (one character, ascending seq) and records each
// one. It returns false if they couldn't be posted; state is saved after
// each message, so a retry resumes cleanly.
func PostEvents(d Deps, key string, events []savedvars.Event, gameHooks map[string]string, firstSightBacklog int) bool {
	url := WebhookFor(key, gameHooks, d.Config)
	if len(events) > 0 && !d.Config.DryRun {
		who := events[len(events)-1].FullName()
		if url == "" {
			d.Log.Error(fmt.Sprintf("%s: no webhook. Set one in game (/doink webhook <url>) or in config.toml", key))
			d.Status.Failed(fmt.Sprintf("no webhook for %s. Set one in game: /doink webhook <url>, or in config.toml", who))
			return false // state untouched: these post once a webhook exists
		}
		if !discord.IsWebhookURL(url) {
			d.Log.Error(fmt.Sprintf("%s: webhook isn't a Discord webhook URL", key))
			d.Status.Failed("bad webhook for " + who)
			return false
		}
	}
	if url == "" {
		url = "dry-run"
	}
	verb := "posted"
	if d.Config.DryRun {
		verb = "printed"
	}
	for start := 0; start < len(events); start += discord.MaxEmbedsPerMessage {
		batch := events[start:min(start+discord.MaxEmbedsPerMessage, len(events))]
		embeds := make([]discord.Embed, 0, len(batch))
		for _, e := range batch {
			embeds = append(embeds, discord.BuildEmbed(e, item(d, e)))
		}
		if err := d.Sender.Send(url, embeds); err != nil {
			d.Log.Error(fmt.Sprintf("%s: post failed: %v", key, err))
			d.Status.Failed(fmt.Sprintf("posting to Discord failed: %v", err))
			return false
		}
		names := make([]string, 0, len(batch))
		for _, e := range batch {
			names = append(names, fmt.Sprintf("#%d %s", e.Seq, e.Type))
		}
		d.Log.Info(fmt.Sprintf("%s: %s %s", key, verb, strings.Join(names, ", ")))
		for _, e := range batch {
			d.Status.Posted(e.FullName(), e.Type)
			d.State.MarkPosted(key, e.Seq, firstSightBacklog)
		}
	}
	return true
}

// Process posts every event in the file that hasn't been posted. It
// returns false if anything is left to retry.
func Process(d Deps, path string) bool {
	file, err := os.ReadFile(path)
	if err != nil {
		d.Log.Warn(fmt.Sprintf("can't read %s: %v", path, err))
		return false
	}
	gameHooks := savedvars.ParseWebhooks(file)
	var keys []string
	byChar := map[string][]savedvars.Event{}
	for _, e := range savedvars.ParseEvents(file) {
		k := e.Key()
		if _, seen := byChar[k]; !seen {
			keys = append(keys, k)
		}
		byChar[k] = append(byChar[k], e)
	}

	ok := true
	for _, key := range keys {
		events := byChar[key]
		sort.SliceStable(events, func(i, j int) bool { return events[i].Seq < events[j].Seq })
		newest := events[len(events)-1].Seq
		legacy := events[len(events)-1].LegacyKey()
		if legacy != key && d.State.Rename(legacy, key) {
			d.Log.Info(fmt.Sprintf("%s: carried over posting history from %s", key, legacy))
		}
		_, haveLast := d.State.LastSeen(key)

		// A wipe shows as the file going backwards. Compare with what the
		// file last showed, never with last_seen, which may run ahead of it.
		if fileSeen, known := d.State.FileSeen(key); known && newest < fileSeen {
			d.Log.Warn(fmt.Sprintf("%s: newest seq %d is below the %d the file showed before. SavedVariables reset? Starting over.",
				key, newest, fileSeen))
			d.State.Forget(key)
			haveLast = false
		}

		var fresh []savedvars.Event
		if !haveLast {
			// First time this character is seen: don't dump the whole
			// 500-entry ring buffer into Discord.
			if n := d.Config.MaxBacklog; n > 0 {
				fresh = events[max(0, len(events)-n):]
			}
			if skipped := len(events) - len(fresh); skipped > 0 {
				d.Log.Info(fmt.Sprintf("%s: new character, skipping %d older events", key, skipped))
			}
		} else {
			for _, e := range events {
				if d.State.IsPending(key, e.Seq) {
					fresh = append(fresh, e)
				}
			}
		}

		if !PostEvents(d, key, fresh, gameHooks, 0) {
			ok = false
			continue
		}
		if !haveLast {
			d.State.Baseline(key, newest) // older ones were skipped on purpose
		} else {
			present := map[int]bool{}
			for _, e := range events {
				present[e.Seq] = true
			}
			d.State.PruneMissing(key, present, newest)
		}
		d.State.MarkFile(key, newest)
	}
	return ok
}

// RunOnce processes every file once, then any realtime screenshots; an
// error if any file failed.
func RunOnce(d Deps) error {
	failed := 0
	for _, path := range d.Config.SavedVariablesPaths {
		if !Process(d, path) {
			failed++
		}
	}
	if d.Config.Realtime {
		dirs, byDir := ScreenshotDirs(d.Config.SavedVariablesPaths)
		NewShots().Scan(d, dirs, byDir)
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d files had events that couldn't be posted", failed, len(d.Config.SavedVariablesPaths))
	}
	return nil
}

// fileSig is a file's (mtime, size), or "missing".
type fileSig struct {
	missing     bool
	mtime, size int64
}

func statSig(path string) fileSig {
	st, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) || err != nil {
		return fileSig{missing: true}
	}
	return fileSig{mtime: st.ModTime().UnixNano(), size: st.Size()}
}

func duration(seconds float64) time.Duration {
	return time.Duration(seconds * float64(time.Second))
}

func (d Deps) watch() []*watcher.Watcher {
	var ws []*watcher.Watcher
	suffix := ""
	if d.Config.DryRun {
		suffix = " (dry run)"
	}
	for _, path := range d.Config.SavedVariablesPaths {
		d.Log.Info("watching " + path + suffix)
		ws = append(ws, watcher.New(path))
	}
	d.Status.Watching(len(ws))
	return ws
}

// Run polls the files until ctx is done. If reload is not nil, configPath
// is polled too and a settled change reloads the settings in place.
func Run(ctx context.Context, d Deps, configPath string, reload func() (*config.Config, error)) error {
	watchers := d.watch()
	shots := NewShots()
	shotDirs, svByDir := ScreenshotDirs(d.Config.SavedVariablesPaths)
	if d.Config.Realtime {
		for _, dir := range shotDirs {
			d.Log.Info("realtime: watching " + dir)
		}
	}
	if d.Started != nil {
		d.Started()
	}
	dirty := map[string]bool{}
	failures := 0
	var retryAt time.Time

	cfgSeen := statSig(configPath)
	var cfgPending *fileSig

	for {
		for _, w := range watchers {
			if w.Poll() {
				dirty[w.Path] = true
			}
		}
		if len(dirty) > 0 && !time.Now().Before(retryAt) {
			for path := range dirty {
				if Process(d, path) {
					delete(dirty, path)
				}
			}
			if len(dirty) > 0 {
				failures++
				delay := min(maxRetryDelay, duration(d.Config.PollInterval*math.Pow(2, float64(failures))))
				d.Log.Info(fmt.Sprintf("retrying in %.0fs", delay.Seconds()))
				retryAt = time.Now().Add(delay)
			} else {
				failures = 0
				d.Status.OK()
			}
		}
		if d.Config.Realtime {
			shots.Scan(d, shotDirs, svByDir)
		}

		if reload != nil {
			sig := statSig(configPath)
			switch {
			case sig == cfgSeen:
				cfgPending = nil
			case cfgPending == nil || *cfgPending != sig:
				cfgPending = &sig
			default: // changed and held still for one poll
				cfgSeen, cfgPending = sig, nil
				cfg, err := reload()
				if err != nil {
					d.Log.Warn(fmt.Sprintf("settings not reloaded: %v", err))
					break
				}
				oldPaths := d.Config.SavedVariablesPaths
				// These were consumed at start-up (the Sender's dry run,
				// state persistence, the Wowhead cache, the update
				// checker), so a live change would only half apply.
				startupOnly := cfg.DryRun != d.Config.DryRun || cfg.Wowhead != d.Config.Wowhead ||
					cfg.UpdateCheck != d.Config.UpdateCheck || cfg.UpdateNotify != d.Config.UpdateNotify
				cfg.DryRun, cfg.Wowhead = d.Config.DryRun, d.Config.Wowhead
				cfg.UpdateCheck, cfg.UpdateNotify = d.Config.UpdateCheck, d.Config.UpdateNotify
				*d.Config = *cfg
				d.Log.Info("settings reloaded")
				if startupOnly {
					d.Log.Warn("dry_run, wowhead, update_check and update_notify changes take effect after a restart")
				}
				if !slices.Equal(oldPaths, cfg.SavedVariablesPaths) {
					watchers = d.watch()
					dirty = map[string]bool{}
					shotDirs, svByDir = ScreenshotDirs(cfg.SavedVariablesPaths)
				}
			}
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(duration(d.Config.PollInterval)):
		}
	}
}
