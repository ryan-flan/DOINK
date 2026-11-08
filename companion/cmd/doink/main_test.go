package main

import (
	"bytes"
	"image/png"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ryan-flan/DOINK/companion/internal/shot"
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

// When the old log can't be moved aside, writes keep going to the same
// file and rotation is retried only after another limit's worth.
func TestRotatingFileWhenRenameFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doink.log")
	// A non-empty directory in the way: Remove and Rename both fail.
	if err := os.MkdirAll(filepath.Join(path+".1", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := openRotating(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"12345678\n", "abc\n", "def\n", "ghijklmnop\n"} {
		if _, err := r.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	r.Close()
	cur, _ := os.ReadFile(path)
	if string(cur) != "12345678\nabc\ndef\nghijklmnop\n" {
		t.Errorf("cur %q", cur)
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

func TestDecodeScreenshots(t *testing.T) {
	dir := t.TempDir()
	payload := `{"char":"Flano","class":"MAGE","data":{"level":5},"realm":"R","seq":5,"surname":"Wren","ts":1790870764,"type":"level_up"}`
	rows, err := shot.Rows([]byte(payload), 0, 959)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, shot.Render(rows, 4)); err != nil {
		t.Fatal(err)
	}
	withCode := filepath.Join(dir, "WoWScrnShot_110226_230648.png")
	os.WriteFile(withCode, buf.Bytes(), 0o644)
	buf.Reset()
	png.Encode(&buf, shot.Render(nil, 4))
	plain := filepath.Join(dir, "plain.png")
	os.WriteFile(plain, buf.Bytes(), 0o644)

	var out bytes.Buffer
	if code := decodeScreenshots([]string{withCode, plain}, &out); code != 0 {
		t.Errorf("exit %d", code)
	}
	text := out.String()
	if !strings.Contains(text, "event #5 level_up Flano Wren-R") || !strings.Contains(text, "plain.png: no DOINK code found") {
		t.Errorf("output:\n%s", text)
	}
	if _, err := os.Stat(withCode); err != nil {
		t.Error("decode deleted the file")
	}
	if code := decodeScreenshots([]string{filepath.Join(dir, "missing.png")}, &out); code != 1 {
		t.Errorf("missing file: exit %d", code)
	}
}
