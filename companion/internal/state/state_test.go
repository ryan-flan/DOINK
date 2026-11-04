package state

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// A save failure is reported once, not on every event.
func TestSaveFailureWarnsOnce(t *testing.T) {
	var buf bytes.Buffer
	Log = slog.New(slog.NewTextHandler(&buf, nil))
	t.Cleanup(func() { Log = nil })
	s, err := Load(filepath.Join(t.TempDir(), "missing", "state.json"), true)
	if err != nil {
		t.Fatal(err)
	}
	s.MarkPosted("k", 1, 0)
	s.MarkPosted("k", 2, 0)
	if n := strings.Count(buf.String(), "level=WARN"); n != 1 || !strings.Contains(buf.String(), "can't save") {
		t.Errorf("%d warnings: %s", n, buf.String())
	}
}

func fresh(t *testing.T) (*State, string) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	return s, path
}

func reload(t *testing.T, path string) *State {
	s, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestUnknownKeyIsPending(t *testing.T) {
	s, _ := fresh(t)
	if !s.IsPending("k", 1) {
		t.Error("unknown key not pending")
	}
	if _, ok := s.LastSeen("k"); ok {
		t.Error("LastSeen of unknown key")
	}
}

func TestGapsTrackedThenFilled(t *testing.T) {
	s, path := fresh(t)
	s.MarkPosted("k", 1, 0)
	s.MarkPosted("k", 4, 0)
	if got := s.Missing("k"); !reflect.DeepEqual(got, []int{2, 3}) {
		t.Fatalf("missing %v", got)
	}
	if !s.IsPending("k", 3) || s.IsPending("k", 4) || !s.IsPending("k", 5) {
		t.Error("pending wrong")
	}
	s.MarkPosted("k", 3, 0)
	s = reload(t, path)
	if got := s.Missing("k"); !reflect.DeepEqual(got, []int{2}) {
		t.Errorf("missing %v after reload", got)
	}
	if last, _ := s.LastSeen("k"); last != 4 {
		t.Errorf("last_seen %d", last)
	}
}

func TestFirstSightBacklog(t *testing.T) {
	s, _ := fresh(t)
	s.MarkPosted("k", 3, 10)
	if got := s.Missing("k"); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Errorf("missing %v", got)
	}
	s.MarkPosted("j", 1, 10)
	if got := s.Missing("j"); !reflect.DeepEqual(got, []int{}) {
		t.Errorf("missing %v", got)
	}
	s.MarkPosted("i", 30, 5)
	if got := s.Missing("i"); !reflect.DeepEqual(got, []int{26, 27, 28, 29}) {
		t.Errorf("missing %v", got)
	}
}

func TestBaselineNeverLowersLastSeen(t *testing.T) {
	s, _ := fresh(t)
	s.MarkPosted("k", 10, 0)
	s.MarkPosted("k", 12, 0)
	s.Baseline("k", 5)
	if last, _ := s.LastSeen("k"); last != 12 {
		t.Errorf("last_seen %d", last)
	}
	if got := s.Missing("k"); len(got) != 0 {
		t.Errorf("missing %v", got)
	}
	s.Baseline("new", 7)
	if last, _ := s.LastSeen("new"); last != 7 {
		t.Errorf("last_seen %d", last)
	}
}

func TestPruneMissing(t *testing.T) {
	s, _ := fresh(t)
	s.MarkPosted("k", 1, 0)
	s.MarkPosted("k", 6, 0) // missing 2..5
	s.MarkPosted("k", 9, 0) // + 7, 8
	s.PruneMissing("k", map[int]bool{3: true, 6: true}, 6)
	if got := s.Missing("k"); !reflect.DeepEqual(got, []int{3, 7, 8}) {
		t.Errorf("missing %v", got)
	}
	s.PruneMissing("none", map[int]bool{}, 1) // no entry: no-op
	if _, ok := s.LastSeen("none"); ok {
		t.Error("prune created an entry")
	}
}

func TestMissingCapKeepsHighest(t *testing.T) {
	s, _ := fresh(t)
	s.MarkPosted("k", 1, 0)
	s.MarkPosted("k", 602, 0) // 600 missing: 2..601
	got := s.Missing("k")
	if len(got) != MissingMax || got[0] != 102 || got[len(got)-1] != 601 {
		t.Errorf("len %d, first %d, last %d", len(got), got[0], got[len(got)-1])
	}
}

func TestFileSeen(t *testing.T) {
	s, path := fresh(t)
	s.MarkFile("k", 3) // no entry: no-op
	if _, ok := s.FileSeen("k"); ok {
		t.Error("MarkFile created an entry")
	}
	s.MarkPosted("k", 22, 0)
	if _, ok := s.FileSeen("k"); ok {
		t.Error("file_seen set by MarkPosted")
	}
	s.MarkFile("k", 21)
	s.Baseline("k", 22)
	s = reload(t, path)
	if fs, ok := s.FileSeen("k"); !ok || fs != 21 {
		t.Errorf("file_seen %d %v", fs, ok)
	}
	if last, _ := s.LastSeen("k"); last != 22 {
		t.Errorf("last_seen %d", last)
	}
}

func TestOnDiskShape(t *testing.T) {
	s, path := fresh(t)
	s.MarkPosted("Flano Wren-R", 22, 0)
	raw, _ := os.ReadFile(path)
	want := "{\n  \"Flano Wren-R\": {\n    \"last_seen\": 22,\n    \"missing\": [],\n    \"file_seen\": null\n  }\n}"
	if string(raw) != want {
		t.Errorf("got\n%s", raw)
	}
}

func TestLoadPre060Shape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	os.WriteFile(path, []byte(`{"k":{"last_seen":5}}`), 0o644)
	s := reload(t, path)
	if last, _ := s.LastSeen("k"); last != 5 {
		t.Errorf("last_seen %d", last)
	}
	if got := s.Missing("k"); len(got) != 0 {
		t.Errorf("missing %v", got)
	}
	if _, ok := s.FileSeen("k"); ok {
		t.Error("file_seen known")
	}
	if s.IsPending("k", 5) || !s.IsPending("k", 6) {
		t.Error("pending wrong")
	}
}

func TestForgetAndRename(t *testing.T) {
	s, path := fresh(t)
	s.MarkPosted("old", 2, 0)
	s.MarkPosted("taken", 1, 0)
	if s.Rename("old", "taken") {
		t.Error("renamed onto an existing key")
	}
	if s.Rename("nope", "x") {
		t.Error("renamed a missing key")
	}
	if !s.Rename("old", "new") {
		t.Fatal("rename failed")
	}
	s = reload(t, path)
	if _, ok := s.LastSeen("old"); ok {
		t.Error("old still there")
	}
	if last, _ := s.LastSeen("new"); last != 2 {
		t.Errorf("new last_seen %d", last)
	}
	s.Forget("new")
	s = reload(t, path)
	if _, ok := s.LastSeen("new"); ok {
		t.Error("forget didn't persist")
	}
}

func TestNoPersistNeverWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := Load(path, false)
	if err != nil {
		t.Fatal(err)
	}
	s.MarkPosted("k", 1, 0)
	s.Baseline("k", 3)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file written: %v", err)
	}
	if last, _ := s.LastSeen("k"); last != 3 {
		t.Errorf("in-memory last_seen %d", last)
	}
}

func TestCorruptFileFailsLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	os.WriteFile(path, []byte("{not json"), 0o644)
	if _, err := Load(path, true); err == nil {
		t.Error("corrupt file loaded")
	}
}
