package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const hook = "https://discord.com/api/webhooks/1/abc"

func setAndRead(t *testing.T, before string, values ...KV) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if before != "" {
		if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := Set(path, values); err != nil {
		t.Fatalf("Set: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Error("temp file left behind")
	}
	return string(after)
}

func TestSet(t *testing.T) {
	cases := []struct {
		name, before string
		values       []KV
		want         string
	}{
		{"new file", "", []KV{{"webhook_url", hook}},
			"webhook_url = '" + hook + "'\n"},
		{"replace in place, keep comments and order",
			"# my settings\nwebhook_url = \"old\"\n# the folder\nwow_dir = 'D:\\WoW'\npoll_interval = 3\n",
			[]KV{{"webhook_url", hook}},
			"# my settings\nwebhook_url = '" + hook + "'\n# the folder\nwow_dir = 'D:\\WoW'\npoll_interval = 3\n"},
		{"commented-out example stays, key added after it",
			"# webhook_url = \"https://...\"\ndry_run = true\n",
			[]KV{{"webhook_url", hook}},
			"# webhook_url = \"https://...\"\ndry_run = true\nwebhook_url = '" + hook + "'\n"},
		{"empty value removes the key",
			"webhook_url = 'x'\nwow_dir = 'C:\\Games\\WoW'\n",
			[]KV{{"wow_dir", ""}},
			"webhook_url = 'x'\n"},
		{"removing a missing key is a no-op", "dry_run = false\n", []KV{{"wow_dir", ""}},
			"dry_run = false\n"},
		{"new key goes above a table",
			"dry_run = false\n\n[notifiers]\nloot = true\n",
			[]KV{{"wow_dir", `C:\Program Files (x86)\World of Warcraft`}},
			"dry_run = false\nwow_dir = 'C:\\Program Files (x86)\\World of Warcraft'\n\n[notifiers]\nloot = true\n"},
		{"a key inside a table is not top-level",
			"[other]\nwow_dir = 'nope'\n",
			[]KV{{"wow_dir", "/games/wow"}},
			"wow_dir = '/games/wow'\n[other]\nwow_dir = 'nope'\n"},
		{"CRLF kept", "# hi\r\nwebhook_url = 'a'\r\n", []KV{{"webhook_url", hook}},
			"# hi\r\nwebhook_url = '" + hook + "'\r\n"},
		{"two keys at once", "", []KV{{"webhook_url", hook}, {"wow_dir", "/w"}},
			"webhook_url = '" + hook + "'\nwow_dir = '/w'\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := setAndRead(t, c.before, c.values...); got != c.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, c.want)
			}
		})
	}
}

func TestQuote(t *testing.T) {
	cases := map[string]string{
		`C:\WoW`:    `'C:\WoW'`,
		"it's":      `"it's"`,
		`a"b'\c`:    `"a\"b'\\c"`,
		"tab\there": `"tab\u0009here"`,
	}
	for in, want := range cases {
		if got := Quote(in); got != want {
			t.Errorf("Quote(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestSetRefusesBrokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	broken := "webhook_url = 'unterminated\n"
	os.WriteFile(path, []byte(broken), 0o644)
	err := Set(path, []KV{{"wow_dir", "/w"}})
	if err == nil || !strings.Contains(err.Error(), "edit it by hand") {
		t.Fatalf("want a refusal, got %v", err)
	}
	if after, _ := os.ReadFile(path); string(after) != broken {
		t.Errorf("file changed: %q", after)
	}
}
