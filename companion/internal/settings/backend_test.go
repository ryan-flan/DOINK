package settings

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryan-flan/DOINK/companion/internal/status"
	"github.com/ryan-flan/DOINK/companion/internal/updates"
)

// fakeWoW makes <dir>/World of Warcraft/_forever_ with the addon installed
// and one account, and returns the WoW folder and the DOINK.lua path.
func fakeWoW(t *testing.T, webhooks string) (string, string) {
	t.Helper()
	wow := filepath.Join(t.TempDir(), "World of Warcraft")
	flavor := filepath.Join(wow, "_forever_")
	sv := filepath.Join(flavor, "WTF", "Account", "ACCOUNT", "SavedVariables")
	for _, dir := range []string{filepath.Join(flavor, "Interface", "AddOns", "DOINK"), sv} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(sv, "DOINK.lua")
	os.WriteFile(file, []byte("DOINKDB = {\n[\"webhooks\"] = {\n"+webhooks+"},\n}\n"), 0o644)
	return wow, file
}

func backend(t *testing.T, configText string) *Backend {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if configText != "" {
		os.WriteFile(path, []byte(configText), 0o644)
	}
	return &Backend{ConfigPath: path, Version: "1.2.3", Status: status.New(nil)}
}

func TestViewReadsConfig(t *testing.T) {
	wow, file := fakeWoW(t, `["Flano Wren-Realm"] = "`+hook+`",`+"\n")
	b := backend(t, "webhook_url = '"+hook+"'\nwow_dir = '"+wow+"'\n")
	v := b.View()
	if v.ConfigError != "" {
		t.Fatal(v.ConfigError)
	}
	if v.Webhook != hook || v.WowDir != wow || v.Version != "1.2.3" {
		t.Errorf("view: %+v", v)
	}
	if len(v.Found) != 1 || v.Found[0] != file {
		t.Errorf("found: %v", v.Found)
	}
	if v.GameWebhook != "Flano Wren-Realm" {
		t.Errorf("game webhook: %q", v.GameWebhook)
	}
	if v.Autostart != nil {
		t.Error("autostart should be unavailable without hooks")
	}
}

func TestViewBadConfig(t *testing.T) {
	b := backend(t, "webhook_url = \n")
	if v := b.View(); v.ConfigError == "" {
		t.Error("want a config error")
	}
}

func TestSaveWritesAndValidates(t *testing.T) {
	wow, _ := fakeWoW(t, "")
	b := backend(t, "# keep me\npoll_interval = 3\n")

	if r := b.Save("https://example.com/nope", ""); !strings.Contains(r.Error, "webhook") {
		t.Errorf("bad webhook accepted: %+v", r)
	}
	if r := b.Save("", filepath.Join(t.TempDir(), "empty")); !strings.Contains(r.Error, "_forever_") {
		t.Errorf("bad folder accepted: %+v", r)
	}

	// Picking _forever_ itself is corrected to the WoW folder.
	r := b.Save("  "+hook+"  ", `"`+filepath.Join(wow, "_forever_")+`"`)
	if r.Error != "" {
		t.Fatal(r.Error)
	}
	if r.View.Webhook != hook || r.View.WowDir != wow {
		t.Errorf("saved view: %+v", r.View)
	}
	text, _ := os.ReadFile(b.ConfigPath)
	if !strings.HasPrefix(string(text), "# keep me\npoll_interval = 3\n") {
		t.Errorf("existing lines lost:\n%s", text)
	}

	// Clearing both removes the keys.
	if r := b.Save("", ""); r.Error != "" || r.View.Webhook != "" || r.View.WowDir != "" {
		t.Errorf("clear: %+v", r)
	}
}

func TestSaveClearsTheProblemItFixes(t *testing.T) {
	b := backend(t, "")
	b.Status.Failed(ProblemNoWebhook)
	b.Save(hook, "")
	if got := b.Status.Error(); got != "" {
		t.Errorf("problem not cleared: %q", got)
	}
	b.Status.Failed("Discord unreachable")
	b.Save(hook, "")
	if got := b.Status.Error(); got != "Discord unreachable" {
		t.Errorf("unrelated problem cleared: %q", got)
	}
}

func TestTestWebhook(t *testing.T) {
	_, file := fakeWoW(t, `["*"] = "`+hook+`",`+"\n")
	b := backend(t, "savedvariables_path = '"+file+"'\n")
	var sent string
	b.Send = func(url string) error { sent = url; return nil }

	if msg := b.Test(""); msg != "" || sent != hook {
		t.Errorf("empty box should use the in-game webhook: %q, sent %q", msg, sent)
	}
	if msg := b.Test("nope"); !strings.Contains(msg, "doesn't look like") {
		t.Errorf("got %q", msg)
	}
	b.Send = func(string) error { return errors.New("401") }
	if msg := b.Test(hook); !strings.Contains(msg, "401") {
		t.Errorf("got %q", msg)
	}
}

func TestLiveHidesUpdateFromSummary(t *testing.T) {
	b := backend(t, "")
	b.Status.Posted("Flano Wren", "reached level 20")
	l := b.Live()
	if len(l.Recent) != 1 || l.Recent[0].What != "reached level 20" {
		t.Errorf("recent: %+v", l.Recent)
	}
	if !strings.HasPrefix(l.Summary, "last post") {
		t.Errorf("summary: %q", l.Summary)
	}
}

func TestLiveInstallable(t *testing.T) {
	b := backend(t, "")
	newer := &updates.Info{Current: "0.14.0", Latest: "0.15.0", ZipURL: "z", SHA256URL: "s"}
	b.Status.UpdateChecked(newer)
	if l := b.Live(); l.Update != "0.15.0" || l.Installable {
		t.Errorf("no Install hook: %+v", l)
	}
	b.Install = func() {}
	if l := b.Live(); !l.Installable || l.Installing != "" {
		t.Errorf("with hook: %+v", l)
	}
	b.Status.UpdateChecked(&updates.Info{Current: "0.14.0", Latest: "0.15.0"}) // no assets
	if l := b.Live(); l.Update != "0.15.0" || l.Installable {
		t.Errorf("no assets: %+v", l)
	}
	b.Status.Installing("Downloading v0.15.0… 1.0 MB")
	if l := b.Live(); l.Installing != "Downloading v0.15.0… 1.0 MB" {
		t.Errorf("installing: %+v", l)
	}
}
