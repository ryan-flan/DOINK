package main

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLineHandler(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(newLineHandler(&buf, "15:04:05", slog.LevelInfo))
	log.Debug("hidden")
	log.Info("two\nlines", "k", 1)
	out := buf.String()
	if strings.Contains(out, "hidden") || !strings.Contains(out, " INFO    two\nlines k=1\n") {
		t.Errorf("got %q", out)
	}
	if _, err := time.Parse("15:04:05", out[:8]); err != nil {
		t.Errorf("time %q", out[:8])
	}
}

func TestRotatingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doink.log")
	r, err := openRotating(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	r.Write([]byte("12345678\n"))
	r.Write([]byte("abc\n")) // would pass 10 bytes: rotate first
	r.Write([]byte("def\n"))
	r.Close()
	old, _ := os.ReadFile(path + ".1")
	cur, _ := os.ReadFile(path)
	if string(old) != "12345678\n" || string(cur) != "abc\ndef\n" {
		t.Errorf("old %q cur %q", old, cur)
	}
}

func TestOptionalString(t *testing.T) {
	var o optionalString
	o.Set("true")
	if !o.set || o.value != "" {
		t.Errorf("%+v", o)
	}
	o.Set("https://x")
	if o.value != "https://x" {
		t.Errorf("%+v", o)
	}
}
