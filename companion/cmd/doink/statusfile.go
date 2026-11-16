package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/ryan-flan/DOINK/companion/internal/status"
	"github.com/ryan-flan/DOINK/companion/internal/toon"
)

// statusFileEvery is how often the status file is rewritten without a
// status change, so the character summary follows the game's logouts.
const statusFileEvery = 5 * time.Second

// statusPost is one posted event in the status file.
type statusPost struct {
	At   int64  `json:"at"`
	Who  string `json:"who"`
	What string `json:"what"`
}

// statusFile is what --status-file writes: the companion's state and the
// player's characters, for a menu bar app (mac/) to show. Read-only for
// the reader; the companion rewrites it whole (atomically) each time.
type statusFile struct {
	Version  string       `json:"version"`
	PID      int          `json:"pid"`
	Updated  int64        `json:"updated"` // Unix seconds
	Every    int          `json:"every"`   // seconds until the next rewrite at the latest
	DryRun   bool         `json:"dry_run"`
	Watching int          `json:"watching"`
	Files    []string     `json:"files"`
	Error    string       `json:"error"`
	Line     string       `json:"line"` // the tray tooltip's one-line summary
	Recent   []statusPost `json:"recent"`
	Toons    toon.Summary `json:"toons"`
}

func buildStatusFile(s *status.Status, paths []string, dryRun bool, now time.Time) statusFile {
	snap := s.Snapshot()
	out := statusFile{
		Version:  version,
		PID:      os.Getpid(),
		Updated:  now.Unix(),
		Every:    int(statusFileEvery / time.Second),
		DryRun:   dryRun,
		Watching: snap.Watching,
		Files:    append([]string{}, paths...),
		Error:    snap.Error,
		Line:     snap.Summary,
		Recent:   []statusPost{},
		Toons:    toon.Read(paths),
	}
	for _, p := range snap.Recent {
		out.Recent = append(out.Recent, statusPost{At: p.At.Unix(), Who: p.Who, What: p.What})
	}
	return out
}

// writeStatusFile replaces path atomically (temp file + rename).
func writeStatusFile(path string, v statusFile) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".status-*.json")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// runStatusFile rewrites path on every status change (poke) and every
// statusFileEvery until ctx is done. paths is the start-up file list: a
// live config reload that changes it is picked up at the next restart.
func runStatusFile(ctx context.Context, path string, s *status.Status, paths []string, dryRun bool, poke <-chan struct{}) {
	warned := false
	write := func() {
		err := writeStatusFile(path, buildStatusFile(s, paths, dryRun, time.Now()))
		if err != nil && !warned {
			slog.Default().Warn("couldn't write the status file: " + err.Error())
		}
		warned = err != nil
	}
	write()
	tick := time.NewTicker(statusFileEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = os.Remove(path) // a stale file would read as "running"
			return
		case <-tick.C:
		case <-poke:
		}
		write()
	}
}
