package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testHook = "https://discord.com/api/webhooks/123/abc"

func TestConfigSetAndShow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("# mine\nsavedvariables_path = '/nowhere/DOINK.lua'\n"), 0o644)
	var out, errs bytes.Buffer
	if code := runConfig(path, []string{"set", "webhook_url=" + testHook, "dry_run=true", "max_backlog=25", "poll_interval=3"}, &out, &errs); code != 0 {
		t.Fatalf("set: %d %s", code, errs.String())
	}
	file, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(file), "# mine\n") || !strings.Contains(string(file), "dry_run = true") ||
		!strings.Contains(string(file), "poll_interval = 3.0") {
		t.Errorf("file:\n%s", file)
	}

	out.Reset()
	if code := runConfig(path, []string{"show"}, &out, &errs); code != 0 {
		t.Fatalf("show: %d %s", code, errs.String())
	}
	var v configView
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Values["webhook_url"] != testHook || v.Values["dry_run"] != true || v.Values["max_backlog"] != float64(25) {
		t.Errorf("values %v", v.Values)
	}
	if _, ok := v.Values["savedvariables_path"]; ok {
		t.Errorf("show isn't meant to expose savedvariables_path: %v", v.Values)
	}

	if code := runConfig(path, []string{"set", "webhooks.loot=" + testHook}, &out, &errs); code != 0 {
		t.Fatal(errs.String())
	}
	out.Reset()
	runConfig(path, []string{"show"}, &out, &errs)
	v = configView{}
	json.Unmarshal(out.Bytes(), &v)
	if v.Routes["loot"] != testHook {
		t.Errorf("routes %v", v.Routes)
	}

	// Empty removes.
	if code := runConfig(path, []string{"set", "dry_run="}, &out, &errs); code != 0 {
		t.Fatal(errs.String())
	}
	if file, _ := os.ReadFile(path); strings.Contains(string(file), "dry_run") {
		t.Errorf("not removed:\n%s", file)
	}
}

func TestConfigSetRejects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	for _, arg := range []string{"webhook_url=https://example.com/x", "forward_url=ftp://x", "dry_run=maybe",
		"max_backlog=-1", "poll_interval=0", "webhooks.looot=" + testHook, "webhooks.loot=https://example.com/x", "nonsense=1", "wow_dir=" + t.TempDir(), "novalue"} {
		var out, errs bytes.Buffer
		if code := runConfig(path, []string{"set", arg}, &out, &errs); code != 1 || errs.Len() == 0 {
			t.Errorf("%s: code %d, %q", arg, code, errs.String())
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("a rejected set wrote the file")
	}
}
