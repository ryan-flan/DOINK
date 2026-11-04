package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ryan-flan/DOINK/companion/internal/updates"
)

// release.yml ships a half when its version source equals the tag number
// as an exact string. Keep both sources plain X.Y.Z so a stray "v", space
// or suffix can't silently drop a half from a release.
var strictVersion = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

func TestVersionFile(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	v := strings.TrimSuffix(s, "\n")
	if !strings.HasSuffix(s, "\n") || strings.ContainsAny(v, " \t\r\n") {
		t.Fatalf("companion/VERSION must be one line ending in a single newline, got %q", s)
	}
	if !strictVersion.MatchString(v) {
		t.Fatalf("companion/VERSION must be X.Y.Z with no leading v, got %q", v)
	}
	if _, ok := updates.ParseVersion(v); !ok {
		t.Fatalf("companion/VERSION %q doesn't parse", v)
	}
}

func TestTOCVersion(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "addon", "DOINK", "DOINK.toc"))
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "## Version:"); ok {
			found = append(found, strings.TrimSpace(v))
		}
	}
	if len(found) != 1 || !strictVersion.MatchString(found[0]) {
		t.Fatalf("DOINK.toc needs exactly one '## Version: X.Y.Z' line, got %q", found)
	}
}
