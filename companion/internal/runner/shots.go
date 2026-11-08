package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ryan-flan/DOINK/companion/internal/savedvars"
	"github.com/ryan-flan/DOINK/companion/internal/shot"
)

// Realtime: with /doink realtime on, the addon takes a screenshot with a
// code in the corner for every event (Transports/Screenshot.lua). Shots
// finds those in each flavor's Screenshots folder, posts the event and
// deletes the file. Screenshots without a valid code are the player's
// own: they are read at most once per run and never changed.

const (
	// shotMaxAge: on the first scan of a folder, older files are never
	// opened (bounds start-up for players with thousands of screenshots).
	shotMaxAge = 14 * 24 * time.Hour
	// shotSettle: a file this new may still be being written.
	shotSettle = 500 * time.Millisecond
	// shotRetry: an unreadable image this young is tried again next poll.
	shotRetry = 10 * time.Second
)

var shotName = regexp.MustCompile(`(?i)^WoWScrnShot_\d{6}_\d{6}(_\d+)?\.(jpe?g|png)$`)

// ScreenshotDirs maps each <flavor>/WTF/Account/<acct>/SavedVariables/DOINK.lua
// to <flavor>/Screenshots, deduped, in order, with the SavedVariables files
// of that flavor (for its webhooks). Paths of another shape are skipped.
func ScreenshotDirs(svPaths []string) ([]string, map[string][]string) {
	var dirs []string
	byDir := map[string][]string{}
	for _, sv := range svPaths {
		up := filepath.Dir(sv) // SavedVariables
		acct := filepath.Dir(up)
		account := filepath.Dir(acct)
		wtf := filepath.Dir(account)
		if !strings.EqualFold(filepath.Base(up), "SavedVariables") ||
			!strings.EqualFold(filepath.Base(account), "Account") ||
			!strings.EqualFold(filepath.Base(wtf), "WTF") {
			continue
		}
		dir := filepath.Join(filepath.Dir(wtf), "Screenshots")
		if _, seen := byDir[dir]; !seen {
			dirs = append(dirs, dir)
		}
		byDir[dir] = append(byDir[dir], sv)
	}
	return dirs, byDir
}

type shotSig struct {
	mtime time.Time
	size  int64
}

// Shots remembers which screenshots it has dealt with this run. Not safe
// for concurrent use: Run owns it, on the same goroutine as Process, so
// realtime posts and file posts never race.
type Shots struct {
	seen    map[string]shotSig
	scanned map[string]bool // folders scanned at least once
	now     func() time.Time
	decode  func(path string) (shot.Result, bool, error)
}

func NewShots() *Shots {
	return &Shots{seen: map[string]shotSig{}, scanned: map[string]bool{},
		now: time.Now, decode: shot.DecodeFile}
}

// Scan handles every new screenshot in dirs.
func (s *Shots) Scan(d Deps, dirs []string, svByDir map[string][]string) {
	for _, dir := range dirs {
		s.scanDir(d, dir, svByDir[dir])
	}
}

func (s *Shots) scanDir(d Deps, dir string, svPaths []string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return // no Screenshots folder yet: the first real scan is still ahead
	}
	first := !s.scanned[dir]
	s.scanned[dir] = true
	now := s.now()
	for _, entry := range entries {
		if entry.IsDir() || !shotName.MatchString(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		sig := shotSig{info.ModTime(), info.Size()}
		if old, ok := s.seen[path]; ok && old == sig {
			continue
		}
		age := now.Sub(info.ModTime())
		if first && age > shotMaxAge {
			s.seen[path] = sig
			continue
		}
		if age < shotSettle {
			continue
		}
		msg, ok, err := s.decode(path)
		if err != nil {
			if age < shotRetry {
				continue
			}
			s.seen[path] = sig
			d.Log.Debug(fmt.Sprintf("can't read screenshot %s: %v", entry.Name(), err))
			continue
		}
		s.seen[path] = sig
		if !ok {
			continue // the player's own screenshot
		}
		if msg.Hello {
			s.hello(d, path, msg.Payload)
			continue
		}
		s.event(d, path, msg.Payload, svPaths)
	}
}

func (s *Shots) hello(d Deps, path string, payload []byte) {
	var h struct {
		Addon, Char, Surname, Realm string
	}
	_ = json.Unmarshal(payload, &h)
	who := strings.TrimSpace(h.Char + " " + h.Surname)
	if who == "" {
		who = "your character"
	}
	d.Log.Info(fmt.Sprintf("realtime check from %s-%s (addon v%s)", who, h.Realm, h.Addon))
	d.Status.Realtime(who)
	s.remove(d, path)
}

func (s *Shots) event(d Deps, path string, payload []byte, svPaths []string) {
	e, err := savedvars.DecodeEvent(payload)
	if err != nil {
		d.Log.Warn(fmt.Sprintf("screenshot %s: bad event: %v", filepath.Base(path), err))
		return
	}
	key := e.Key()
	if !d.State.IsPending(key, e.Seq) {
		s.remove(d, path) // posted already (from the file, or before a restart)
		return
	}
	hooks := map[string]string{}
	for _, sv := range svPaths {
		if file, err := os.ReadFile(sv); err == nil {
			for k, v := range savedvars.ParseWebhooks(file) {
				hooks[k] = v
			}
		}
	}
	// On failure the file stays; the event posts from SavedVariables after
	// the next /reload, and the next run deletes the screenshot.
	if PostEvents(d, key, []savedvars.Event{e}, hooks, d.Config.MaxBacklog) == nil {
		s.remove(d, path)
	}
}

func (s *Shots) remove(d Deps, path string) {
	if d.Config.DryRun {
		d.Log.Info("would delete " + filepath.Base(path) + " (dry run)")
		return
	}
	if err := os.Remove(path); err != nil {
		d.Log.Warn(fmt.Sprintf("can't delete screenshot %s: %v", filepath.Base(path), err))
	}
}
