package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ryan-flan/DOINK/companion/internal/config"
	"github.com/ryan-flan/DOINK/companion/internal/discord"
	"github.com/ryan-flan/DOINK/companion/internal/savedvars"
	"github.com/ryan-flan/DOINK/companion/internal/state"
	"github.com/ryan-flan/DOINK/companion/internal/status"
	"github.com/ryan-flan/DOINK/companion/internal/watcher"
	"github.com/ryan-flan/DOINK/companion/internal/wowhead"
)

const (
	configHook = "https://discord.com/api/webhooks/1/config"
	gameHook   = "https://discord.com/api/webhooks/2/account"
	charHook   = "https://discord.com/api/webhooks/3/alt"
)

type fakeSender struct {
	mu   sync.Mutex
	sent [][]discord.Embed
	urls []string
	fail bool
	// rejectNext: this many sends answer as Discord does to a bad embed.
	rejectNext int
}

func (f *fakeSender) Send(url string, embeds []discord.Embed) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return errors.New("boom")
	}
	if f.rejectNext > 0 {
		f.rejectNext--
		return fmt.Errorf("%w: HTTP 400: Invalid Form Body", discord.ErrRejected)
	}
	f.sent = append(f.sent, embeds)
	f.urls = append(f.urls, url)
	return nil
}

func (f *fakeSender) titles() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []string{}
	for _, m := range f.sent {
		for _, e := range m {
			out = append(out, e["title"].(string))
		}
	}
	return out
}

func (f *fakeSender) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent, f.urls = nil, nil
}

type capture struct {
	mu   sync.Mutex
	recs []string
}

func (c *capture) Enabled(context.Context, slog.Level) bool { return true }
func (c *capture) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.recs = append(c.recs, r.Level.String()+" "+r.Message)
	return nil
}
func (c *capture) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *capture) WithGroup(string) slog.Handler      { return c }

func (c *capture) has(level, substr string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.recs {
		if strings.HasPrefix(r, level+" ") && strings.Contains(r, substr) {
			return true
		}
	}
	return false
}

func (c *capture) count(level string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, r := range c.recs {
		if strings.HasPrefix(r, level+" ") {
			n++
		}
	}
	return n
}

type fixture struct {
	t      *testing.T
	dir    string
	sv     string
	cfg    *config.Config
	sender *fakeSender
	logs   *capture
	status *status.Status
}

func newFixture(t *testing.T) *fixture {
	dir := t.TempDir()
	sv := filepath.Join(dir, "DOINK.lua")
	watcher.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	return &fixture{
		t: t, dir: dir, sv: sv,
		cfg: &config.Config{SavedVariablesPaths: []string{sv}, WebhookURL: configHook,
			StatePath: filepath.Join(dir, "state.json"), MaxBacklog: 10, PollInterval: 2},
		sender: &fakeSender{}, logs: &capture{}, status: status.New(nil),
	}
}

func (f *fixture) state() *state.State {
	s, err := state.Load(f.cfg.StatePath, true)
	if err != nil {
		f.t.Fatal(err)
	}
	return s
}

func (f *fixture) deps(s *state.State) Deps {
	return Deps{Config: f.cfg, State: s, Sender: f.sender, Status: f.status,
		Log: slog.New(f.logs), Version: "test"}
}

// run writes the file and processes it, with a fresh State from disk
// unless one is given (a restart, as far as the runner can tell).
func (f *fixture) run(text string, s *state.State) (bool, *state.State) {
	if err := os.WriteFile(f.sv, []byte(text), 0o644); err != nil {
		f.t.Fatal(err)
	}
	if s == nil {
		s = f.state()
	}
	return Process(f.deps(s), f.sv) == nil, s
}

func webhooksBlock(hooks map[string]string) string {
	keys := make([]string, 0, len(hooks))
	for k := range hooks {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("[\"webhooks\"] = {\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "[%q] = %q,\n", k, hooks[k])
	}
	b.WriteString("},\n")
	return b.String()
}

func eventsFileAs(char, surname string, seqs ...int) string {
	extra := ""
	if surname != "" {
		extra = `\"surname\":\"` + surname + `\",`
	}
	var lines []string
	for _, s := range seqs {
		lines = append(lines, fmt.Sprintf(`"{%s\"char\":\"%s\",\"class\":\"MAGE\",\"data\":{\"level\":%d},\"realm\":\"R\",\"seq\":%d,\"ts\":1790870764,\"type\":\"level_up\"}",`,
			extra, char, s, s))
	}
	return "[\"events\"] = {\n" + strings.Join(lines, "\n") + "\n},"
}

func eventsFile(seqs ...int) string { return eventsFileAs("Flano", "", seqs...) }

func span(from, to int) []int {
	var out []int
	for i := from; i <= to; i++ {
		out = append(out, i)
	}
	return out
}

func event(seq int) savedvars.Event {
	return savedvars.Event{Char: "Flano", Class: "MAGE", Realm: "R", Seq: seq, TS: 1790870764, HasTS: true,
		Type: "level_up", Data: map[string]any{"level": float64(seq)}}
}

func lastSeen(s *state.State, key string) int {
	n, ok := s.LastSeen(key)
	if !ok {
		return -1
	}
	return n
}

func TestPostsOnlyNewAndSurvivesRestart(t *testing.T) {
	f := newFixture(t)
	f.run(eventsFile(1, 2), nil)
	if n := len(f.sender.titles()); n != 2 {
		t.Fatalf("posted %d", n)
	}
	f.sender.reset()
	ok, s := f.run(eventsFile(1, 2, 3), nil) // restart: fresh State from disk
	if !ok || len(f.sender.sent) != 1 || f.sender.sent[0][0]["title"] != "Flano reached level 3" {
		t.Errorf("ok %v sent %v", ok, f.sender.titles())
	}
	if lastSeen(s, "Flano-R") != 3 {
		t.Errorf("last_seen %d", lastSeen(s, "Flano-R"))
	}
}

func TestBatchesTenPerMessage(t *testing.T) {
	f := newFixture(t)
	f.cfg.MaxBacklog = 100
	f.run(eventsFile(span(1, 23)...), nil)
	var sizes []int
	for _, m := range f.sender.sent {
		sizes = append(sizes, len(m))
	}
	if !reflect.DeepEqual(sizes, []int{10, 10, 3}) {
		t.Errorf("sizes %v", sizes)
	}
}

func TestFirstSightBacklogCap(t *testing.T) {
	f := newFixture(t)
	f.cfg.MaxBacklog = 2
	_, s := f.run(eventsFile(span(1, 49)...), nil)
	if got := f.sender.titles(); !reflect.DeepEqual(got, []string{"Flano reached level 48", "Flano reached level 49"}) {
		t.Errorf("titles %v", got)
	}
	if lastSeen(s, "Flano-R") != 49 {
		t.Errorf("last_seen %d", lastSeen(s, "Flano-R"))
	}
	if !f.logs.has("INFO", "new character, skipping 47 older events") {
		t.Errorf("logs %v", f.logs.recs)
	}
}

func TestBacklogZeroJustBaselines(t *testing.T) {
	f := newFixture(t)
	f.cfg.MaxBacklog = 0
	_, s := f.run(eventsFile(1, 2, 3), nil)
	if len(f.sender.sent) != 0 || lastSeen(s, "Flano-R") != 3 {
		t.Errorf("sent %v last_seen %d", f.sender.titles(), lastSeen(s, "Flano-R"))
	}
}

func TestWebhookPrecedence(t *testing.T) {
	f := newFixture(t)
	text := webhooksBlock(map[string]string{"*": gameHook, "Alt-R": charHook}) +
		eventsFile(1) + eventsFileAs("Alt", "", 1) + eventsFileAs("Bob", "", 1)
	f.run(text, nil)
	got := append([]string(nil), f.sender.urls...)
	sort.Strings(got)
	want := []string{gameHook, gameHook, charHook}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("urls %v", f.sender.urls)
	}
	f.sender.reset()
	f.run(eventsFile(1, 2), nil) // no in-game webhooks at all
	if !reflect.DeepEqual(f.sender.urls, []string{configHook}) {
		t.Errorf("urls %v", f.sender.urls)
	}
	if WebhookFor("x", map[string]string{"*": ""}, &config.Config{}) != "" {
		t.Error("empty in-game webhook used")
	}
}

func TestNoWebhookAnywhereKeepsEventsForLater(t *testing.T) {
	f := newFixture(t)
	f.cfg.WebhookURL = ""
	ok, s := f.run(eventsFile(1, 2), nil)
	if ok || len(f.sender.sent) != 0 || lastSeen(s, "Flano-R") != -1 {
		t.Errorf("ok %v sent %v", ok, f.sender.titles())
	}
	if !f.logs.has("ERROR", "Flano-R: no webhook. Set one in game (/doink webhook <url>) or in config.toml") {
		t.Errorf("logs %v", f.logs.recs)
	}
	if f.status.Error() != "no webhook for Flano. Set one in game: /doink webhook <url>, or in config.toml" {
		t.Errorf("status %q", f.status.Error())
	}
	ok, _ = f.run(webhooksBlock(map[string]string{"*": gameHook})+eventsFile(1, 2), s)
	if !ok || len(f.sender.sent) != 1 || len(f.sender.sent[0]) != 2 {
		t.Errorf("ok %v sent %v", ok, f.sender.titles())
	}
}

func TestBadGameWebhookIsRefused(t *testing.T) {
	f := newFixture(t)
	ok, _ := f.run(webhooksBlock(map[string]string{"*": "https://evil.example/x"})+eventsFile(1), nil)
	if ok || len(f.sender.sent) != 0 || f.logs.count("ERROR") != 1 {
		t.Errorf("ok %v sent %v logs %v", ok, f.sender.titles(), f.logs.recs)
	}
	if f.status.Error() != "bad webhook for Flano" {
		t.Errorf("status %q", f.status.Error())
	}
}

func TestFailureKeepsStateForRetry(t *testing.T) {
	f := newFixture(t)
	s := f.state()
	f.run(eventsFile(1), s)
	f.sender.fail = true
	ok, _ := f.run(eventsFile(1, 2), s)
	if ok || lastSeen(s, "Flano-R") != 1 {
		t.Errorf("ok %v last_seen %d", ok, lastSeen(s, "Flano-R"))
	}
	if f.status.Error() != "posting to Discord failed: boom" || !f.logs.has("ERROR", "Flano-R: post failed: boom") {
		t.Errorf("status %q logs %v", f.status.Error(), f.logs.recs)
	}
	f.sender.fail = false
	ok, _ = f.run(eventsFile(1, 2), s)
	titles := f.sender.titles()
	if !ok || titles[len(titles)-1] != "Flano reached level 2" {
		t.Errorf("ok %v titles %v", ok, titles)
	}
}

func TestSurnameUpgradeCarriesHistory(t *testing.T) {
	f := newFixture(t)
	f.run(eventsFile(1, 2), nil) // v0.2.0 posted #1-2 as "Flano-R"
	f.sender.reset()
	ok, s := f.run(eventsFileAs("Flano", "Wren", 1, 2, 3), nil)
	if !ok || !reflect.DeepEqual(f.sender.titles(), []string{"Flano Wren reached level 3"}) {
		t.Errorf("ok %v titles %v", ok, f.sender.titles())
	}
	if lastSeen(s, "Flano Wren-R") != 3 || lastSeen(s, "Flano-R") != -1 {
		t.Errorf("new %d legacy %d", lastSeen(s, "Flano Wren-R"), lastSeen(s, "Flano-R"))
	}
	if !f.logs.has("INFO", "Flano Wren-R: carried over posting history from Flano-R") {
		t.Errorf("logs %v", f.logs.recs)
	}
}

func TestSeqResetStartsOver(t *testing.T) {
	f := newFixture(t)
	s := f.state()
	f.run(eventsFile(40, 41), s)
	f.sender.reset()
	f.run(eventsFile(1), s) // SavedVariables wiped
	if got := f.sender.titles(); !reflect.DeepEqual(got, []string{"Flano reached level 1"}) {
		t.Errorf("titles %v", got)
	}
	if fs, _ := s.FileSeen("Flano-R"); lastSeen(s, "Flano-R") != 1 || fs != 1 {
		t.Errorf("last_seen %d file_seen %d", lastSeen(s, "Flano-R"), fs)
	}
	if !f.logs.has("WARN", "Flano-R: newest seq 1 is below the 41 the file showed before. SavedVariables reset? Starting over.") {
		t.Errorf("logs %v", f.logs.recs)
	}
}

func TestPostedAheadOfTheFileIsNotAReset(t *testing.T) {
	// Seen live (2026-10-02): a realtime post of #22 then a restart, with
	// the file still at #21, re-posted ten events.
	f := newFixture(t)
	s := f.state()
	f.run(eventsFile(span(12, 21)...), s)
	PostEvents(f.deps(s), "Flano-R", []savedvars.Event{event(22)}, nil, 10)
	f.sender.reset()
	ok, _ := f.run(eventsFile(span(12, 21)...), s) // same file
	if !ok || len(f.sender.sent) != 0 {
		t.Errorf("ok %v re-posted %v", ok, f.sender.titles())
	}
	if f.logs.count("WARN") != 0 {
		t.Errorf("logs %v", f.logs.recs)
	}
	if fs, _ := s.FileSeen("Flano-R"); lastSeen(s, "Flano-R") != 22 || fs != 21 {
		t.Errorf("last_seen %d file_seen %d", lastSeen(s, "Flano-R"), fs)
	}
}

func TestStateWithoutFileSeenLearnsIt(t *testing.T) {
	f := newFixture(t)
	os.WriteFile(f.cfg.StatePath, []byte(`{"Flano-R": {"last_seen": 22, "missing": []}}`), 0o644)
	s := f.state()
	f.run(eventsFile(20, 21), s)
	if len(f.sender.sent) != 0 || f.logs.count("WARN") != 0 {
		t.Errorf("sent %v logs %v", f.sender.titles(), f.logs.recs)
	}
	if fs, ok := s.FileSeen("Flano-R"); !ok || fs != 21 {
		t.Errorf("file_seen %d %v", fs, ok)
	}
}

func TestPostAheadThenFileFillsTheGapsOnly(t *testing.T) {
	f := newFixture(t)
	s := f.state()
	if PostEvents(f.deps(s), "Flano-R", []savedvars.Event{event(3)}, nil, 10) != nil {
		t.Fatal("post failed")
	}
	if got := s.Missing("Flano-R"); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Errorf("missing %v", got)
	}
	f.sender.reset()
	ok, _ := f.run(eventsFile(1, 2, 3, 4), s)
	want := []string{"Flano reached level 1", "Flano reached level 2", "Flano reached level 4"}
	if !ok || !reflect.DeepEqual(f.sender.titles(), want) {
		t.Errorf("ok %v titles %v", ok, f.sender.titles())
	}
	if got := s.Missing("Flano-R"); len(got) != 0 || lastSeen(s, "Flano-R") != 4 {
		t.Errorf("missing %v last_seen %d", got, lastSeen(s, "Flano-R"))
	}
}

func TestDryRunPostsWithoutAWebhook(t *testing.T) {
	f := newFixture(t)
	f.cfg.WebhookURL, f.cfg.DryRun = "", true
	ok, _ := f.run(eventsFile(1), nil)
	if !ok || !reflect.DeepEqual(f.sender.urls, []string{"dry-run"}) || !f.logs.has("INFO", "Flano-R: printed #1 level_up") {
		t.Errorf("ok %v urls %v logs %v", ok, f.sender.urls, f.logs.recs)
	}
}

func TestLootLooksUpWowhead(t *testing.T) {
	f := newFixture(t)
	var asked []int
	s := f.state()
	d := f.deps(s)
	d.Items = wowhead.NewCache(func(id int) *wowhead.Item {
		asked = append(asked, id)
		return &wowhead.Item{ID: id, Name: "Wolfmaster Cape", IconURL: "https://x/i.jpg"}
	})
	loot := savedvars.Event{Char: "Flano", Realm: "R", Seq: 1, Type: "loot", Data: map[string]any{"item_id": 6314.0}}
	odd := savedvars.Event{Char: "Flano", Realm: "R", Seq: 2, Type: "loot", Data: map[string]any{"item_id": "6314"}}
	PostEvents(d, "Flano-R", []savedvars.Event{loot, odd, event(3)}, nil, 0)
	if !reflect.DeepEqual(asked, []int{6314}) {
		t.Errorf("asked %v", asked)
	}
	if f.sender.sent[0][0]["thumbnail"] == nil {
		t.Error("no thumbnail")
	}
}

func TestMountLooksUpWowhead(t *testing.T) {
	f := newFixture(t)
	var asked []int
	s := f.state()
	d := f.deps(s)
	d.Items = wowhead.NewCache(func(id int) *wowhead.Item {
		asked = append(asked, id)
		return &wowhead.Item{ID: id, Name: "Reins of the Gray Wolf", IconURL: "https://x/i.jpg"}
	})
	byItem := savedvars.Event{Char: "Flano", Realm: "R", Seq: 1, Type: "mount",
		Data: map[string]any{"name": "Gray Wolf", "item_id": 5656.0}}
	bySpell := savedvars.Event{Char: "Flano", Realm: "R", Seq: 2, Type: "mount",
		Data: map[string]any{"name": "Raptor", "item_id": nil, "spell_id": 24242.0}}
	PostEvents(d, "Flano-R", []savedvars.Event{byItem, bySpell}, nil, 0)
	if !reflect.DeepEqual(asked, []int{5656}) {
		t.Errorf("asked %v", asked)
	}
	if f.sender.sent[0][0]["thumbnail"] == nil {
		t.Error("no thumbnail")
	}
	if f.sender.sent[0][1]["thumbnail"] != nil {
		t.Error("thumbnail without an item id")
	}
}

func TestRunOnce(t *testing.T) {
	f := newFixture(t)
	os.WriteFile(f.sv, []byte(eventsFile(1)), 0o644)
	if err := RunOnce(f.deps(f.state())); err != nil || len(f.sender.sent) != 1 {
		t.Errorf("err %v sent %v", err, f.sender.titles())
	}
	f.sender.fail = true
	os.WriteFile(f.sv, []byte(eventsFile(1, 2)), 0o644)
	if err := RunOnce(f.deps(f.state())); err == nil {
		t.Error("no error on failure")
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A batch Discord rejects (HTTP 400) can never succeed; it is dropped so
// the character's later events aren't stuck behind it forever.
func TestPostEventsSkipsRejected(t *testing.T) {
	f := newFixture(t)
	s := f.state()
	f.sender.rejectNext = 1
	if PostEvents(f.deps(s), "Flano-R", []savedvars.Event{event(1), event(2)}, nil, 0) != nil {
		t.Fatal("a rejected batch must count as handled, not retried")
	}
	if s.IsPending("Flano-R", 1) || s.IsPending("Flano-R", 2) || len(f.sender.titles()) != 0 {
		t.Errorf("pending %v %v, sent %v", s.IsPending("Flano-R", 1), s.IsPending("Flano-R", 2), f.sender.titles())
	}
	if !f.logs.has("ERROR", "skipped #1 ") || !strings.Contains(f.status.Error(), "rejected #1 ") {
		t.Errorf("logs %v status %q", f.logs.recs, f.status.Error())
	}
	// The next one posts as usual and clears the problem.
	if PostEvents(f.deps(s), "Flano-R", []savedvars.Event{event(3)}, nil, 0) != nil || len(f.sender.titles()) != 1 || f.status.Error() != "" {
		t.Errorf("sent %v status %q", f.sender.titles(), f.status.Error())
	}
	// Anything else is still retried.
	f.sender.fail = true
	if PostEvents(f.deps(s), "Flano-R", []savedvars.Event{event(4)}, nil, 0) == nil || !s.IsPending("Flano-R", 4) {
		t.Error("a network failure must be retried")
	}
}

func TestRunReloadsConfigAndStops(t *testing.T) {
	f := newFixture(t)
	f.cfg.PollInterval = 0.01
	configPath := filepath.Join(f.dir, "config.toml")
	os.WriteFile(configPath, []byte("# v1\n"), 0o644)
	os.WriteFile(f.sv, []byte(eventsFile(1)), 0o644)
	newHook := "https://discord.com/api/webhooks/9/new"
	reloads := 0
	reload := func() (*config.Config, error) {
		reloads++
		c := *f.cfg
		c.WebhookURL = newHook
		c.DryRun = true // start-up only: must be kept, with a warning
		return &c, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- Run(ctx, f.deps(f.state()), configPath, reload) }()

	waitFor(t, "the first post", func() bool { return len(f.sender.titles()) == 1 })
	os.WriteFile(configPath, []byte("webhook_url = 'changed'\n"), 0o644)
	waitFor(t, "the reload", func() bool { return f.logs.has("INFO", "settings reloaded") })
	os.WriteFile(f.sv, []byte(eventsFile(1, 2)), 0o644)
	waitFor(t, "the second post", func() bool { return len(f.sender.titles()) == 2 })
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run didn't stop")
	}
	f.sender.mu.Lock()
	defer f.sender.mu.Unlock()
	if !reflect.DeepEqual(f.sender.urls, []string{configHook, newHook}) || reloads != 1 {
		t.Errorf("urls %v reloads %d", f.sender.urls, reloads)
	}
	if !f.logs.has("INFO", "watching "+f.sv) {
		t.Errorf("logs %v", f.logs.recs)
	}
	if f.cfg.DryRun || f.cfg.WebhookURL != newHook || !f.logs.has("WARN", "take effect after a restart") {
		t.Errorf("dry_run %v webhook %q logs %v", f.cfg.DryRun, f.cfg.WebhookURL, f.logs.recs)
	}
}

func TestStartedWarningSurvivesWatching(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d := f.deps(f.state())
	d.Started = func() { f.status.Failed("no webhook yet") }
	if err := Run(ctx, d, filepath.Join(f.dir, "config.toml"), nil); err != nil {
		t.Fatal(err)
	}
	if got := f.status.Summary(); got != "DOINK: no webhook yet" {
		t.Errorf("summary %q", got)
	}
}

func TestRunWithACancelledContext(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error)
	go func() { done <- Run(ctx, f.deps(f.state()), filepath.Join(f.dir, "config.toml"), nil) }()
	select {
	case err := <-done:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run didn't stop")
	}
	if !strings.HasPrefix(f.status.Summary(), "DOINK: watching 1 file, nothing posted yet") {
		t.Errorf("summary %q", f.status.Summary())
	}
}

func TestBackoffIsCapped(t *testing.T) {
	for _, c := range []struct {
		failures int
		want     time.Duration
	}{{0, 2 * time.Second}, {1, 4 * time.Second}, {4, 32 * time.Second}, {5, 60 * time.Second}, {33, 60 * time.Second}, {1000, 60 * time.Second}} {
		if got := backoff(2, c.failures); got != c.want {
			t.Errorf("backoff(2, %d) = %v, want %v", c.failures, got, c.want)
		}
	}
}

func TestRunWaitsForAWebhookWithoutRetrying(t *testing.T) {
	f := newFixture(t)
	f.cfg.PollInterval = 0.01
	f.cfg.WebhookURL = ""
	configPath := filepath.Join(f.dir, "config.toml")
	os.WriteFile(configPath, []byte("# v1\n"), 0o644)
	os.WriteFile(f.sv, []byte(eventsFile(1)), 0o644)
	reload := func() (*config.Config, error) {
		c := *f.cfg
		c.WebhookURL = configHook
		return &c, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- Run(ctx, f.deps(f.state()), configPath, reload) }()

	waitFor(t, "the error", func() bool { return f.logs.has("ERROR", "Flano-R: no webhook") })
	time.Sleep(100 * time.Millisecond) // ten polls
	if n := f.logs.count("ERROR"); n != 1 || f.logs.has("INFO", "retrying in") {
		t.Errorf("errors %d logs %v", n, f.logs.recs)
	}
	if f.status.Error() == "" {
		t.Error("the no-webhook state was cleared")
	}
	os.WriteFile(configPath, []byte("webhook_url = 'set'\n"), 0o644)
	waitFor(t, "the post", func() bool { return len(f.sender.titles()) == 1 })
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run didn't stop")
	}
	if f.status.Error() != "" || f.logs.count("ERROR") != 1 {
		t.Errorf("status %q logs %v", f.status.Error(), f.logs.recs)
	}
}
