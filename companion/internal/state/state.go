// Package state remembers what has been posted per character, persisted so
// restarts never re-post.
//
// Per character: last_seen, the highest seq posted; missing, seqs below it
// that haven't been posted yet; and file_seen, the highest seq the
// SavedVariables file has ever shown. file_seen is what the "SavedVariables
// wiped" check compares against: last_seen may run ahead of the file when
// an event arrives by another path before the next /reload.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"sort"
	"sync"
)

// Log receives save warnings; nil means slog.Default().
var Log *slog.Logger

func logger() *slog.Logger {
	if Log != nil {
		return Log
	}
	return slog.Default()
}

// MissingMax is the addon's ring buffer size; older gaps can't be filled.
const MissingMax = 500

type entry struct {
	LastSeen int   `json:"last_seen"`
	Missing  []int `json:"missing"`
	FileSeen *int  `json:"file_seen"`
}

// State is safe for concurrent use.
type State struct {
	mu      sync.Mutex
	path    string
	persist bool
	chars   map[string]*entry
	// lastSaveErr is the standing save failure, warned about once.
	lastSaveErr string
}

// Load reads path; a missing file is an empty state. A file that exists but
// can't be read or parsed is an error: starting empty could re-post a backlog.
// With persist false nothing is ever written (dry run).
func Load(path string, persist bool) (*State, error) {
	s := &State{path: path, persist: persist, chars: map[string]*entry{}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("can't read state file %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, &s.chars); err != nil {
		return nil, fmt.Errorf("can't read state file %s: %w", path, err)
	}
	for k, e := range s.chars {
		if e == nil {
			return nil, fmt.Errorf("can't read state file %s: %q is null", path, k)
		}
		if e.Missing == nil {
			e.Missing = []int{} // entries written before v0.6.0
		}
	}
	return s, nil
}

// LastSeen is the highest seq posted for key.
func (s *State) LastSeen(key string) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.chars[key]; e != nil {
		return e.LastSeen, true
	}
	return 0, false
}

// FileSeen is the highest seq the SavedVariables file has shown for key, if
// a file pass has recorded one.
func (s *State) FileSeen(key string) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.chars[key]; e != nil && e.FileSeen != nil {
		return *e.FileSeen, true
	}
	return 0, false
}

// MarkFile records that a file pass saw seqs up to newest.
func (s *State) MarkFile(key string, newest int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.chars[key]
	if e == nil {
		return // nothing posted or baselined yet; the next pass records it
	}
	if e.FileSeen == nil || *e.FileSeen != newest {
		e.FileSeen = &newest
		s.save()
	}
}

// Missing returns a sorted copy of key's unposted seqs below last_seen.
func (s *State) Missing(key string) []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []int{}
	if e := s.chars[key]; e != nil {
		out = append(out, e.Missing...)
	}
	sort.Ints(out)
	return out
}

// IsPending reports whether seq still needs posting.
func (s *State) IsPending(key string, seq int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.chars[key]
	if e == nil {
		return true
	}
	if seq > e.LastSeen {
		return true
	}
	for _, m := range e.Missing {
		if m == seq {
			return true
		}
	}
	return false
}

// MarkPosted records seq as posted. Seqs skipped over become missing. For a
// character never seen before, firstSightBacklog older seqs are marked
// missing so the usual backlog rule applies on the next file pass.
func (s *State) MarkPosted(key string, seq, firstSightBacklog int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.chars[key]
	if e == nil {
		low := seq
		if firstSightBacklog != 0 {
			low = max(1, seq-firstSightBacklog+1)
		}
		missing := []int{}
		for m := low; m < seq; m++ {
			missing = append(missing, m)
		}
		e = &entry{LastSeen: seq, Missing: missing}
		s.chars[key] = e
	} else {
		set := map[int]bool{}
		for _, m := range e.Missing {
			set[m] = true
		}
		if seq > e.LastSeen {
			for m := e.LastSeen + 1; m < seq; m++ {
				set[m] = true
			}
			e.LastSeen = seq
		} else {
			delete(set, seq)
		}
		missing := make([]int, 0, len(set))
		for m := range set {
			missing = append(missing, m)
		}
		sort.Ints(missing)
		e.Missing = missing
	}
	if len(e.Missing) > MissingMax {
		e.Missing = append([]int{}, e.Missing[len(e.Missing)-MissingMax:]...)
	}
	s.save()
}

// Baseline marks everything up to seq as accounted for (posted or
// deliberately skipped): no gaps. last_seen never goes down; file_seen is kept.
func (s *State) Baseline(key string, seq int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.chars[key]
	if e == nil {
		s.chars[key] = &entry{LastSeen: seq, Missing: []int{}}
	} else {
		e.LastSeen = max(seq, e.LastSeen)
		e.Missing = []int{}
	}
	s.save()
}

// PruneMissing forgets gaps at or below newest that the file doesn't
// contain: they can never be filled.
func (s *State) PruneMissing(key string, present map[int]bool, newest int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.chars[key]
	if e == nil {
		return
	}
	keep := []int{}
	for _, m := range e.Missing {
		if m > newest || present[m] {
			keep = append(keep, m)
		}
	}
	if len(keep) != len(e.Missing) {
		e.Missing = keep
		s.save()
	}
}

// Forget drops key entirely.
func (s *State) Forget(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.chars[key]; ok {
		delete(s.chars, key)
		s.save()
	}
}

// Rename moves old's entry to key if key has none. Reports whether it moved.
func (s *State) Rename(old, key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.chars[old]
	if !ok {
		return false
	}
	if _, taken := s.chars[key]; taken {
		return false
	}
	s.chars[key] = e
	delete(s.chars, old)
	s.save()
	return true
}

// save writes the file (caller holds mu). Write-then-rename, so a crash
// mid-write can't corrupt it. A failure is warned about once (until it
// changes or clears): the next save tries again, the in-memory state
// stays correct, but a restart would re-post what wasn't recorded.
func (s *State) save() {
	if !s.persist {
		return
	}
	err := s.write()
	switch {
	case err == nil:
		s.lastSaveErr = ""
	case err.Error() != s.lastSaveErr:
		s.lastSaveErr = err.Error()
		logger().Warn(fmt.Sprintf("can't save %s (a restart may re-post events): %v", s.path, err))
	}
}

func (s *State) write() error {
	raw, err := json.MarshalIndent(s.chars, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
