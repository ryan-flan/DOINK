package runner

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/ryan-flan/DOINK/companion/internal/shot"
	"github.com/ryan-flan/DOINK/companion/internal/state"
)

// shotFixture is a runner fixture inside a real-shaped WoW tree:
// <root>/_forever_/WTF/Account/ACC/SavedVariables/DOINK.lua and
// <root>/_forever_/Screenshots.
type shotFixture struct {
	*fixture
	shots   string
	s       *Shots
	decodes int
	notices []string
	now     time.Time
}

func newShotFixture(t *testing.T) *shotFixture {
	f := newFixture(t)
	flavor := filepath.Join(f.dir, "_forever_")
	svDir := filepath.Join(flavor, "WTF", "Account", "ACC", "SavedVariables")
	shots := filepath.Join(flavor, "Screenshots")
	for _, d := range []string{svDir, shots} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f.sv = filepath.Join(svDir, "DOINK.lua")
	f.cfg.SavedVariablesPaths = []string{f.sv}
	f.cfg.Realtime = true
	if err := os.WriteFile(f.sv, []byte("DOINKDB = {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sf := &shotFixture{fixture: f, shots: shots, now: time.Now().Add(time.Minute)}
	sf.s = NewShots()
	sf.s.now = func() time.Time { return sf.now }
	sf.s.decode = func(path string) (shot.Result, bool, error) {
		sf.decodes++
		return shot.DecodeFile(path)
	}
	sf.status.OnNotice = func(msg string) { sf.notices = append(sf.notices, msg) }
	return sf
}

func (sf *shotFixture) scan(s *state.State) {
	dirs, byDir := ScreenshotDirs(sf.cfg.SavedVariablesPaths)
	sf.s.Scan(sf.deps(s), dirs, byDir)
}

func pngOf(t *testing.T, payload string, flags uint8) []byte {
	t.Helper()
	rows, err := shot.Rows([]byte(payload), flags, 959)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, shot.Render(rows, 4)); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func eventJSON(seq int) string {
	return `{"char":"Flano","class":"MAGE","data":{"level":` + strconv.Itoa(seq) + `},"realm":"R","seq":` + strconv.Itoa(seq) +
		`,"ts":1790870764,"type":"level_up"}`
}

func (sf *shotFixture) write(name string, data []byte) string {
	sf.t.Helper()
	path := filepath.Join(sf.shots, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		sf.t.Fatal(err)
	}
	return path
}

func exists(path string) bool { _, err := os.Stat(path); return err == nil }

func TestShotEventPostedOnceAndDeleted(t *testing.T) {
	sf := newShotFixture(t)
	path := sf.write("WoWScrnShot_110226_230648.jpg", pngOf(t, eventJSON(5), 0)) // png bytes, jpg name: decoded by content
	s := sf.state()
	sf.scan(s)
	if len(sf.sender.sent) != 1 || sf.sender.urls[0] != configHook {
		t.Fatalf("sent %v to %v", sf.sender.titles(), sf.sender.urls)
	}
	if exists(path) {
		t.Error("screenshot not deleted")
	}
	if lastSeen(s, "Flano-R") != 5 {
		t.Errorf("last seen %d", lastSeen(s, "Flano-R"))
	}
	sf.scan(s)
	if len(sf.sender.sent) != 1 {
		t.Error("posted twice")
	}
}

func TestShotUsesTheInGameWebhook(t *testing.T) {
	sf := newShotFixture(t)
	os.WriteFile(sf.sv, []byte("DOINKDB = {\n"+webhooksBlock(map[string]string{"*": gameHook})+"}\n"), 0o644)
	sf.write("WoWScrnShot_110226_230648.png", pngOf(t, eventJSON(5), 0))
	sf.scan(sf.state())
	if len(sf.sender.urls) != 1 || sf.sender.urls[0] != gameHook {
		t.Errorf("urls %v", sf.sender.urls)
	}
}

func TestPlayersOwnScreenshotIsLeftAlone(t *testing.T) {
	sf := newShotFixture(t)
	var buf bytes.Buffer
	png.Encode(&buf, shot.Render(nil, 4)) // no grid: just black
	path := sf.write("WoWScrnShot_110226_230648.png", buf.Bytes())
	other := sf.write("holiday.png", pngOf(t, eventJSON(5), 0)) // not a WoW screenshot name
	s := sf.state()
	sf.scan(s)
	sf.scan(s)
	if !exists(path) || !exists(other) {
		t.Error("a file was deleted")
	}
	if sf.decodes != 1 {
		t.Errorf("decoded %d times, want 1", sf.decodes)
	}
	if len(sf.sender.sent) != 0 {
		t.Error("posted")
	}
}

func TestShotAlreadyPostedIsDeleted(t *testing.T) {
	sf := newShotFixture(t)
	s := sf.state()
	s.MarkPosted("Flano-R", 5, 0)
	path := sf.write("WoWScrnShot_110226_230648.jpg", pngOf(t, eventJSON(5), 0))
	sf.scan(s)
	if len(sf.sender.sent) != 0 || exists(path) {
		t.Errorf("sent %d, exists %v", len(sf.sender.sent), exists(path))
	}
}

func TestShotPostFailureKeepsTheFile(t *testing.T) {
	sf := newShotFixture(t)
	sf.sender.fail = true
	path := sf.write("WoWScrnShot_110226_230648.jpg", pngOf(t, eventJSON(5), 0))
	s := sf.state()
	sf.scan(s)
	sf.scan(s)
	if !exists(path) {
		t.Error("deleted after a failed post")
	}
	if n := sf.logs.count("ERROR"); n != 1 {
		t.Errorf("%d failed posts, want 1 (no retry in the same run)", n)
	}
	if lastSeen(s, "Flano-R") != -1 {
		t.Error("state advanced")
	}
}

func TestShotDryRunDeletesNothing(t *testing.T) {
	sf := newShotFixture(t)
	sf.cfg.DryRun = true
	path := sf.write("WoWScrnShot_110226_230648.jpg", pngOf(t, eventJSON(5), 0))
	s, _ := state.Load(sf.cfg.StatePath, false)
	sf.scan(s)
	sf.scan(s)
	if !exists(path) || !sf.logs.has("INFO", "would delete WoWScrnShot_110226_230648.jpg") {
		t.Errorf("exists %v, logs %v", exists(path), sf.logs.recs)
	}
	if len(sf.sender.sent) != 1 {
		t.Errorf("printed %d times", len(sf.sender.sent))
	}
}

func TestShotHello(t *testing.T) {
	sf := newShotFixture(t)
	path := sf.write("WoWScrnShot_110226_230648.jpg",
		pngOf(t, `{"addon":"0.14.0","char":"Flano","realm":"R","surname":"Wren","test":true,"type":"hello"}`, shot.FlagHello))
	sf.scan(sf.state())
	if !reflect.DeepEqual(sf.notices, []string{"Realtime works for Flano Wren"}) {
		t.Errorf("notices %v", sf.notices)
	}
	if exists(path) || len(sf.sender.sent) != 0 {
		t.Error("hello not consumed, or posted")
	}
	if !sf.logs.has("INFO", "realtime check from Flano Wren-R (addon v0.14.0)") {
		t.Errorf("logs %v", sf.logs.recs)
	}
}

func TestShotAgeRules(t *testing.T) {
	sf := newShotFixture(t)
	old := sf.write("WoWScrnShot_101526_120000.jpg", pngOf(t, eventJSON(3), 0))
	longAgo := time.Now().Add(-15 * 24 * time.Hour)
	os.Chtimes(old, longAgo, longAgo)
	fresh := sf.write("WoWScrnShot_110226_230648.jpg", pngOf(t, eventJSON(5), 0))
	s := sf.state()

	sf.now = time.Now() // fresh is still being written, as far as we know
	sf.scan(s)
	if sf.decodes != 0 {
		t.Fatalf("decoded %d files on the first scan, want 0", sf.decodes)
	}
	sf.now = time.Now().Add(time.Minute)
	sf.scan(s)
	if sf.decodes != 1 || exists(fresh) {
		t.Errorf("decodes %d, fresh exists %v", sf.decodes, exists(fresh))
	}
	if !exists(old) {
		t.Error("an old screenshot was touched")
	}
}

// A Screenshots folder that appears after the companion started still
// gets the first-scan age rule (older files are never opened).
func TestShotFolderAppearingLaterKeepsAgeRule(t *testing.T) {
	sf := newShotFixture(t)
	if err := os.Remove(sf.shots); err != nil {
		t.Fatal(err)
	}
	s := sf.state()
	sf.scan(s) // nothing there yet
	if err := os.Mkdir(sf.shots, 0o755); err != nil {
		t.Fatal(err)
	}
	old := sf.write("WoWScrnShot_101526_120000.jpg", pngOf(t, eventJSON(3), 0))
	longAgo := time.Now().Add(-15 * 24 * time.Hour)
	if err := os.Chtimes(old, longAgo, longAgo); err != nil {
		t.Fatal(err)
	}
	sf.scan(s)
	if sf.decodes != 0 || !exists(old) || len(sf.sender.sent) != 0 {
		t.Errorf("decoded %d, exists %v, sent %d", sf.decodes, exists(old), len(sf.sender.sent))
	}
}

func TestShotStillBeingWrittenIsRetried(t *testing.T) {
	sf := newShotFixture(t)
	full := pngOf(t, eventJSON(5), 0)
	path := sf.write("WoWScrnShot_110226_230648.jpg", full[:len(full)/2])
	info, _ := os.Stat(path)
	s := sf.state()
	sf.now = info.ModTime().Add(2 * time.Second)
	sf.scan(s)
	sf.scan(s)
	if sf.decodes != 2 || len(sf.sender.sent) != 0 {
		t.Fatalf("decodes %d, sent %d", sf.decodes, len(sf.sender.sent))
	}
	os.WriteFile(path, full, 0o644)
	sf.scan(s)
	if len(sf.sender.sent) != 1 || exists(path) {
		t.Errorf("sent %d, exists %v", len(sf.sender.sent), exists(path))
	}

	// Unreadable for longer than shotRetry: given up on, left alone.
	broken := sf.write("WoWScrnShot_110226_230700.jpg", []byte("not an image"))
	sf.now = time.Now().Add(time.Minute)
	before := sf.decodes
	sf.scan(s)
	sf.scan(s)
	if sf.decodes != before+1 || !exists(broken) {
		t.Errorf("decodes %d (want %d), exists %v", sf.decodes, before+1, exists(broken))
	}
}

func TestShotThenFilePassNoDoublePost(t *testing.T) {
	sf := newShotFixture(t)
	s := sf.state()
	sf.write("WoWScrnShot_110226_230648.jpg", pngOf(t, eventJSON(5), 0))
	sf.scan(s)
	if len(sf.sender.sent) != 1 {
		t.Fatalf("realtime sent %d", len(sf.sender.sent))
	}
	// After /reload the file shows seqs 1..5: 1..4 post, 5 doesn't again.
	sf.run(eventsFile(span(1, 5)...), s)
	titles := sf.sender.titles()
	if len(titles) != 5 {
		t.Errorf("posted %d events in all, want 5: %v", len(titles), titles)
	}
}

func TestRunOnceScansScreenshots(t *testing.T) {
	sf := newShotFixture(t)
	path := sf.write("WoWScrnShot_110226_230648.jpg", pngOf(t, eventJSON(5), 0))
	os.Chtimes(path, time.Now().Add(-time.Minute), time.Now().Add(-time.Minute))
	if err := RunOnce(sf.deps(sf.state())); err != nil {
		t.Fatal(err)
	}
	if len(sf.sender.sent) != 1 || exists(path) {
		t.Errorf("sent %d, exists %v", len(sf.sender.sent), exists(path))
	}
	sf.cfg.Realtime = false
	path = sf.write("WoWScrnShot_110226_230700.jpg", pngOf(t, eventJSON(6), 0))
	os.Chtimes(path, time.Now().Add(-time.Minute), time.Now().Add(-time.Minute))
	RunOnce(sf.deps(sf.state()))
	if !exists(path) {
		t.Error("realtime = false still read screenshots")
	}
}

func TestScreenshotDirs(t *testing.T) {
	sv := func(parts ...string) string {
		return filepath.Join(append([]string{"/w", "_forever_", "WTF", "Account"}, parts...)...)
	}
	a := sv("A", "SavedVariables", "DOINK.lua")
	b := sv("B", "SavedVariables", "DOINK.lua")
	c := filepath.Join("/w", "_classic_", "WTF", "Account", "C", "SavedVariables", "DOINK.lua")
	odd := filepath.Join("/tmp", "DOINK.lua")
	dirs, byDir := ScreenshotDirs([]string{a, b, c, odd})
	want := []string{filepath.Join("/w", "_forever_", "Screenshots"), filepath.Join("/w", "_classic_", "Screenshots")}
	if !reflect.DeepEqual(dirs, want) {
		t.Errorf("dirs %v", dirs)
	}
	if !reflect.DeepEqual(byDir[want[0]], []string{a, b}) {
		t.Errorf("byDir %v", byDir)
	}
}
