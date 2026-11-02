package watcher

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSettle(t *testing.T) {
	var buf bytes.Buffer
	Log = slog.New(slog.NewTextHandler(&buf, nil))
	defer func() { Log = nil }()
	path := filepath.Join(t.TempDir(), "DOINK.lua")
	w := New(path)

	if w.Poll() || w.Poll() {
		t.Error("missing file reported")
	}
	if n := strings.Count(buf.String(), "waiting for"); n != 1 {
		t.Errorf("logged %d times", n)
	}

	os.WriteFile(path, []byte("a"), 0o644)
	if w.Poll() {
		t.Error("reported before settling")
	}
	if !w.Poll() {
		t.Error("not reported once settled")
	}
	if w.Poll() {
		t.Error("reported twice")
	}

	os.WriteFile(path, []byte("ab"), 0o644)
	if w.Poll() {
		t.Error("change reported before settling")
	}
	os.WriteFile(path, []byte("abc"), 0o644) // still being written
	os.Chtimes(path, time.Now(), time.Now().Add(time.Second))
	if w.Poll() {
		t.Error("reported while still changing")
	}
	if !w.Poll() {
		t.Error("not reported after settling")
	}

	os.Remove(path)
	w.Poll()
	if n := strings.Count(buf.String(), "waiting for"); n != 2 {
		t.Errorf("not re-armed: logged %d times", n)
	}
}
