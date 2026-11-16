package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ryan-flan/DOINK/companion/internal/status"
)

// The macOS app (mac/Sources/DOINKBar/Status.swift) decodes these keys.
func TestStatusFileKeys(t *testing.T) {
	s := status.New(nil)
	s.Watching(1)
	s.Posted("Flano Wren", "level_up")
	path := filepath.Join(t.TempDir(), "status.json")
	if err := writeStatusFile(path, buildStatusFile(s, nil, true, time.Unix(100, 0))); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"version", "pid", "updated", "every", "dry_run", "watching", "files", "error", "line", "recent", "toons"} {
		if _, ok := got[key]; !ok {
			t.Errorf("missing %q in %s", key, raw)
		}
	}
	toons := got["toons"].(map[string]any)
	if _, ok := toons["last"]; !ok || toons["others"] == nil {
		t.Errorf("toons = %v", toons)
	}
	if recent := got["recent"].([]any); len(recent) != 1 || recent[0].(map[string]any)["who"] != "Flano Wren" {
		t.Errorf("recent = %v", recent)
	}
	if got["updated"].(float64) != 100 || got["dry_run"] != true || got["watching"].(float64) != 1 {
		t.Errorf("got %s", raw)
	}
}

func TestStatusFileRemovedOnExit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { runStatusFile(ctx, path, status.New(nil), nil, false, nil); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("status file never written")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("still there: %v", err)
	}
}
