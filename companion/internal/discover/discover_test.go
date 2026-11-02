package discover

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"testing"
)

func mkdir(t *testing.T, parts ...string) string {
	path := filepath.Join(parts...)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func fakeWow(t *testing.T, wow string, accounts ...string) {
	mkdir(t, wow, "_forever_", "Interface", "AddOns", "DOINK")
	for _, a := range accounts {
		mkdir(t, wow, "_forever_", "WTF", "Account", a, "SavedVariables")
	}
	// A flavor without the addon is skipped.
	mkdir(t, wow, "_classic_", "WTF", "Account", "OTHER", "SavedVariables")
}

func TestInWowDir(t *testing.T) {
	wow := t.TempDir()
	fakeWow(t, wow, "B#1", "A#1")
	want := []string{
		filepath.Join(wow, "_forever_", "WTF", "Account", "A#1", "SavedVariables", "DOINK.lua"),
		filepath.Join(wow, "_forever_", "WTF", "Account", "B#1", "SavedVariables", "DOINK.lua"),
	}
	if got := InWowDir(wow); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	if got := InWowDir(t.TempDir()); got != nil {
		t.Errorf("empty dir: %v", got)
	}
	if got := InWowDir(filepath.Join(wow, "nope")); got != nil {
		t.Errorf("missing dir: %v", got)
	}
}

func TestSavedVariablesOverTwoRoots(t *testing.T) {
	c, d := t.TempDir(), t.TempDir()
	fakeWow(t, filepath.Join(c, "Program Files (x86)", "World of Warcraft"), "A")
	fakeWow(t, filepath.Join(d, "Games", "World of Warcraft"), "B")
	got := SavedVariables([]string{c, d})
	if len(got) != 2 || filepath.Base(filepath.Dir(filepath.Dir(got[0]))) != "A" ||
		filepath.Base(filepath.Dir(filepath.Dir(got[1]))) != "B" {
		t.Errorf("got %v", got)
	}
}

func TestDriveRootsFindsWinePrefixes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Wine prefixes are a Linux thing")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	lutris := mkdir(t, home, "Games", "battlenet", "drive_c")
	mkdir(t, home, "Games", "not-a-prefix")
	steam := mkdir(t, home, ".local", "share", "Steam", "steamapps", "compatdata", "123", "pfx", "drive_c")
	roots := DriveRoots()
	if !slices.Contains(roots, lutris) || !slices.Contains(roots, steam) {
		t.Errorf("roots %v", roots)
	}
	if slices.Contains(roots, filepath.Join(home, "Games", "not-a-prefix", "drive_c")) {
		t.Errorf("roots %v", roots)
	}
	if slices.Index(roots, lutris) > slices.Index(roots, steam) {
		t.Errorf("order %v", roots)
	}
}
