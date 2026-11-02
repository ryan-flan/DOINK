// Package watcher polls a file for changes.
//
// Deliberately (mtime, size) polling, not OS file-watch hooks: simple, and
// reliable across filesystems (Wine prefixes, network drives, WSL's /mnt).
package watcher

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
)

// Log receives watcher messages; nil means slog.Default().
var Log *slog.Logger

func logger() *slog.Logger {
	if Log != nil {
		return Log
	}
	return slog.Default()
}

type sig struct {
	mtime int64
	size  int64
}

// Watcher reports a change once the file has changed *and then held still
// for one poll*, so it is never read while WoW is halfway through writing it.
type Watcher struct {
	Path          string
	seen, pending *sig
	missingLogged bool
}

// New watches path. The first report needs two polls.
func New(path string) *Watcher { return &Watcher{Path: path} }

// Poll reports whether the file changed and has settled since the last report.
func (w *Watcher) Poll() bool {
	st, err := os.Stat(w.Path)
	if errors.Is(err, fs.ErrNotExist) {
		if !w.missingLogged {
			logger().Warn(fmt.Sprintf("waiting for %s to exist (log in and /reload once)", w.Path))
			w.missingLogged = true
		}
		return false
	}
	if err != nil {
		return false
	}
	w.missingLogged = false
	s := &sig{st.ModTime().UnixNano(), st.Size()}
	if w.seen != nil && *s == *w.seen {
		return false
	}
	if w.pending == nil || *s != *w.pending {
		w.pending = s
		return false
	}
	w.seen = s
	return true
}
