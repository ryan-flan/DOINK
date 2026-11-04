package status

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ryan-flan/DOINK/companion/internal/updates"
)

func TestSummaryAndChanges(t *testing.T) {
	var calls []string
	s := New(func(e string) { calls = append(calls, e) })
	if got := s.Summary(); got != "DOINK: watching 0 files, nothing posted yet" {
		t.Errorf("got %q", got)
	}
	s.Watching(1)
	if got := s.Summary(); got != "DOINK: watching 1 file, nothing posted yet" {
		t.Errorf("got %q", got)
	}
	s.Failed("no webhook")
	s.Failed("no webhook") // same problem: no second call
	if got := s.Summary(); got != "DOINK: no webhook" {
		t.Errorf("got %q", got)
	}
	s.OK()
	s.OK() // nothing to clear: no call
	s.Posted("Flano Wren", "level_up")
	want := "DOINK: last post " + time.Now().Format("15:04") + ", Flano Wren: level_up"
	if got := s.Summary(); got != want {
		t.Errorf("got %q", got)
	}
	if strings.Join(calls, "|") != "|no webhook||" {
		t.Errorf("calls %q", calls)
	}
	s.Failed("x")
	s.Watching(2)
	if s.Error() != "" {
		t.Error("Watching(2) kept the error")
	}
}

func TestRecent(t *testing.T) {
	s := New(nil)
	for i := 0; i < 10; i++ {
		s.Posted("F", string(rune('a'+i)))
	}
	r := s.Recent()
	if len(r) != 8 || r[0].What != "j" || r[7].What != "c" {
		t.Errorf("recent %+v", r)
	}
}

func TestSummaryCap(t *testing.T) {
	s := New(nil)
	s.Failed(strings.Repeat("é", 200))
	got := s.Summary()
	if utf8.RuneCountInString(got) != 127 || !strings.HasSuffix(got, "…") {
		t.Errorf("len %d %q", utf8.RuneCountInString(got), got)
	}
	s.Failed(strings.Repeat("x", 127-len("DOINK: ")))
	if got := s.Summary(); utf8.RuneCountInString(got) != 127 || strings.HasSuffix(got, "…") {
		t.Errorf("exactly 127 was cut: %q", got)
	}
}

func TestUpdateNotifiesOncePerVersion(t *testing.T) {
	var notices []string
	s := New(nil)
	s.OnNotice = func(text string) { notices = append(notices, text) }
	newer := &updates.Info{Current: "0.12.0", Latest: "0.13.0"}

	s.UpdateChecked(newer) // NotifyUpdates off
	if len(notices) != 0 {
		t.Errorf("notified with notify off: %q", notices)
	}
	if !strings.HasSuffix(s.Summary(), " · v0.13.0 available") {
		t.Errorf("summary %q", s.Summary())
	}

	s.NotifyUpdates = true
	s.UpdateChecked(newer)
	s.UpdateChecked(newer)
	s.UpdateChecked(&updates.Info{Current: "0.12.0", Latest: "0.12.0"})
	want := "DOINK v0.13.0 is available (you have v0.12.0). Right-click the tray icon to open the download page."
	if len(notices) != 1 || notices[0] != want {
		t.Errorf("notices %q", notices)
	}
	if strings.Contains(s.Summary(), "available") || s.Update().Latest != "0.12.0" {
		t.Errorf("summary %q", s.Summary())
	}
	s.UpdateChecked(&updates.Info{Current: "0.12.0", Latest: "0.14.0"})
	if len(notices) != 2 {
		t.Errorf("notices %q", notices)
	}
}

func TestInstalling(t *testing.T) {
	changes := 0
	s := New(func(string) { changes++ })
	if s.Install() != "" {
		t.Error("starts non-empty")
	}
	s.Installing("Downloading v0.15.0… 3.2 MB")
	if s.Install() != "Downloading v0.15.0… 3.2 MB" || changes != 1 {
		t.Errorf("%q, %d changes", s.Install(), changes)
	}
	s.Installing("")
	if s.Install() != "" || changes != 2 {
		t.Errorf("%q, %d changes", s.Install(), changes)
	}
}

func TestUpdateNoticeWording(t *testing.T) {
	s := New(nil)
	s.NotifyUpdates = true
	var got string
	s.OnNotice = func(text string) { got = text }
	s.UpdateChecked(&updates.Info{Current: "0.14.0", Latest: "0.15.0", ZipURL: "z", SHA256URL: "s"})
	if !strings.Contains(got, "to update.") {
		t.Errorf("installable: %q", got)
	}
	s.UpdateChecked(&updates.Info{Current: "0.14.0", Latest: "0.16.0"})
	if !strings.Contains(got, "open the download page") {
		t.Errorf("not installable: %q", got)
	}
}
