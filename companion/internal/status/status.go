// Package status holds what the companion is doing, for the tray tooltip
// and notifications. Written by the runner, read by the tray.
package status

import (
	"fmt"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ryan-flan/DOINK/companion/internal/updates"
)

const (
	tooltipMax = 127 // Windows tray tooltip limit (128 incl. terminator)
	recentMax  = 8
)

// Post is one posted event.
type Post struct {
	At        time.Time
	Who, What string
}

// Status is safe for concurrent use.
type Status struct {
	mu sync.Mutex
	// onChange(newError): newError is set only when a *new* problem
	// appears, so the tray warns once rather than on every retry; ""
	// means "refresh".
	onChange func(newError string)
	// OnNotice(text): worth a plain (non-warning) notification.
	OnNotice      func(string)
	NotifyUpdates bool // config update_notify

	watching   int
	lastPost   *Post
	recent     []Post // newest first
	err        string
	update     *updates.Info
	updateTold string
	installing string // progress line while an update installs, else ""
}

// New returns a Status that calls onChange (may be nil) on every change.
func New(onChange func(newError string)) *Status {
	if onChange == nil {
		onChange = func(string) {}
	}
	return &Status{onChange: onChange, OnNotice: func(string) {}}
}

// Watching records how many files are watched; n > 0 clears the error.
func (s *Status) Watching(n int) {
	s.mu.Lock()
	s.watching = n
	if n > 0 {
		s.err = ""
	}
	s.mu.Unlock()
	s.onChange("")
}

// Posted records a post and clears the error.
func (s *Status) Posted(who, what string) {
	p := Post{At: time.Now(), Who: who, What: what}
	s.mu.Lock()
	s.lastPost = &p
	s.recent = append([]Post{p}, s.recent...)
	if len(s.recent) > recentMax {
		s.recent = s.recent[:recentMax]
	}
	s.err = ""
	s.mu.Unlock()
	s.onChange("")
}

// Failed records a problem; onChange hears it only if it is new.
func (s *Status) Failed(msg string) {
	s.mu.Lock()
	isNew := msg != s.err
	s.err = msg
	s.mu.Unlock()
	if isNew {
		s.onChange(msg)
	}
}

// Realtime reports a /doink realtime test that arrived by screenshot.
func (s *Status) Realtime(who string) {
	s.OnNotice("Realtime works for " + who)
}

// OK clears any standing problem: everything pending was handled.
func (s *Status) OK() {
	s.mu.Lock()
	had := s.err != ""
	s.err = ""
	s.mu.Unlock()
	if had {
		s.onChange("")
	}
}

// Error is the standing problem, or "".
func (s *Status) Error() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Recent lists the last posts, newest first.
func (s *Status) Recent() []Post {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Post(nil), s.recent...)
}

// UpdateChecked stores an update check's result. A notification goes out
// only with NotifyUpdates, once per newer version.
func (s *Status) UpdateChecked(info *updates.Info) {
	s.mu.Lock()
	s.update = info
	tell := info != nil && s.NotifyUpdates && info.Newer() && info.Latest != s.updateTold
	if tell {
		s.updateTold = info.Latest
	}
	notice := s.OnNotice
	s.mu.Unlock()
	if tell {
		action := "open the download page"
		if info.Installable() {
			action = "update"
		}
		notice(fmt.Sprintf("DOINK v%s is available (you have v%s). Right-click the tray icon to %s.",
			info.Latest, info.Current, action))
	}
	s.onChange("")
}

// Installing sets the update's progress line ("" when none is running).
func (s *Status) Installing(msg string) {
	s.mu.Lock()
	s.installing = msg
	s.mu.Unlock()
	s.onChange("")
}

// Install is the update's progress line, or "".
func (s *Status) Install() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.installing
}

// Update is the last check's result, or nil.
func (s *Status) Update() *updates.Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.update
}

// Summary is the one-line tooltip text.
func (s *Status) Summary() string {
	s.mu.Lock()
	var text string
	switch {
	case s.err != "":
		text = "DOINK: " + s.err
	case s.lastPost != nil:
		text = fmt.Sprintf("DOINK: last post %s, %s: %s", s.lastPost.At.Local().Format("15:04"), s.lastPost.Who, s.lastPost.What)
	default:
		files := "files"
		if s.watching == 1 {
			files = "file"
		}
		text = fmt.Sprintf("DOINK: watching %d %s, nothing posted yet", s.watching, files)
	}
	if s.update != nil && s.update.Newer() {
		text += " · v" + s.update.Latest + " available"
	}
	s.mu.Unlock()
	if utf8.RuneCountInString(text) > tooltipMax {
		runes := []rune(text)
		text = string(runes[:tooltipMax-1]) + "…"
	}
	return text
}
