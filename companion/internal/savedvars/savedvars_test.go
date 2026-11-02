package savedvars

import (
	"context"
	"log/slog"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// capture records log messages at WARN and above.
type capture struct {
	mu   sync.Mutex
	msgs []string
}

func (c *capture) Enabled(_ context.Context, l slog.Level) bool { return l >= slog.LevelWarn }
func (c *capture) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.msgs = append(c.msgs, r.Message)
	return nil
}
func (c *capture) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *capture) WithGroup(string) slog.Handler      { return c }

func captureWarnings(t *testing.T) *capture {
	c := &capture{}
	old := Log
	Log = slog.New(c)
	t.Cleanup(func() { Log = old })
	return c
}

// luaEvent is one event as WoW writes it: a double-quoted Lua string of JSON.
func luaEvent(json string) string {
	json = strings.ReplaceAll(json, `\`, `\\`)
	return `"` + strings.ReplaceAll(json, `"`, `\"`) + `",`
}

func ev(char string, seq int) string {
	return luaEvent(`{"char":"` + char + `","realm":"R","seq":` + strconv.Itoa(seq) + `,"type":"level_up","data":{"level":2}}`)
}

func block(lines ...string) string {
	return "[\"events\"] = {\n" + strings.Join(lines, "\n") + "\n},\n"
}

type cs struct {
	char string
	seq  int
}

func charsSeqs(events []Event) []cs {
	out := []cs{}
	for _, e := range events {
		out = append(out, cs{e.Char, e.Seq})
	}
	return out
}

func seqs(events []Event) []int {
	out := []int{}
	for _, e := range events {
		out = append(out, e.Seq)
	}
	return out
}

func TestParseEvents(t *testing.T) {
	t.Run("real file", func(t *testing.T) {
		file, err := os.ReadFile("testdata/DOINK.lua")
		if err != nil {
			t.Fatal(err)
		}
		events := ParseEvents(file)
		if len(events) != 1 {
			t.Fatalf("got %d events", len(events))
		}
		e := events[0]
		if e.Seq != 1 || !e.Test || e.Key() != "Flano-Classic Beta PvE 2" {
			t.Errorf("got %+v", e)
		}
		if !reflect.DeepEqual(e.Data, map[string]any{"level": 9.0}) {
			t.Errorf("data %#v", e.Data)
		}
		if e.Class != "WARRIOR" || e.Level != 8 || !e.HasTS || e.TS != 1790870764 || e.Type != "level_up" {
			t.Errorf("got %+v", e)
		}
	})

	t.Run("multiple chars", func(t *testing.T) {
		file := "DOINKDB = {\n" + block(ev("Flano", 1), ev("Flano", 2)) + block(ev("Alt", 7)) + "}"
		got := charsSeqs(ParseEvents([]byte(file)))
		want := []cs{{"Flano", 1}, {"Flano", 2}, {"Alt", 7}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v", got)
		}
	})

	t.Run("braces and escapes", func(t *testing.T) {
		tricky := luaEvent(`{"char":"Flano","realm":"R","seq":1,"type":"loot","data":{"name":"a } b \\ \"q\""}}`)
		file := block(tricky, ev("Flano", 2))
		events := ParseEvents([]byte(file))
		if len(events) != 2 {
			t.Fatalf("got %d events", len(events))
		}
		if got := events[0].Data["name"]; got != `a } b \ "q"` {
			t.Errorf("name %q", got)
		}
	})

	t.Run("item link pipes", func(t *testing.T) {
		link := `|cff0070dd|Hitem:2140::::::::9:::::::|h[Carving Knife]|h|r`
		file := block(luaEvent(`{"char":"Flano","realm":"R","seq":1,"type":"loot","data":{"link":"` + link + `"}}`))
		events := ParseEvents([]byte(file))
		if len(events) != 1 || events[0].Data["link"] != link {
			t.Errorf("got %+v", events)
		}
	})

	t.Run("raw UTF-8", func(t *testing.T) {
		file := block(ev("Pàul", 1))
		events := ParseEvents([]byte(file))
		if len(events) != 1 || events[0].Char != "Pàul" {
			t.Errorf("got %+v", events)
		}
	})

	t.Run("decimal escapes", func(t *testing.T) {
		file := block(`"{\"char\":\"P\195\160ul\",\"realm\":\"R\",\"seq\":1,\"type\":\"level_up\"}",`)
		events := ParseEvents([]byte(file))
		if len(events) != 1 || events[0].Char != "Pàul" {
			t.Errorf("got %+v", events)
		}
	})

	t.Run("comments", func(t *testing.T) {
		file := block(ev("Flano", 1)+" -- [1]", ev("Flano", 2)+" -- [2]")
		if got := seqs(ParseEvents([]byte(file))); !reflect.DeepEqual(got, []int{1, 2}) {
			t.Errorf("got %v", got)
		}
	})

	t.Run("bad entries skipped", func(t *testing.T) {
		warnings := captureWarnings(t)
		file := block(
			ev("Flano", 1),
			`"not json",`,
			luaEvent(`{"seq":2}`),
			luaEvent(`{"char":"Flano","realm":"R","seq":"3","type":"level_up"}`),
			ev("Flano", 4),
		)
		if got := seqs(ParseEvents([]byte(file))); !reflect.DeepEqual(got, []int{1, 4}) {
			t.Errorf("got %v", got)
		}
		if len(warnings.msgs) != 3 {
			t.Errorf("want 3 warnings, got %q", warnings.msgs)
		}
	})

	t.Run("seq must be an integer number", func(t *testing.T) {
		for _, seq := range []string{`1.0`, `true`, `"3"`} {
			if _, err := DecodeEvent([]byte(`{"char":"F","realm":"R","type":"x","seq":` + seq + `}`)); err == nil {
				t.Errorf("seq %s accepted", seq)
			}
		}
	})

	t.Run("empty and missing", func(t *testing.T) {
		if got := ParseEvents([]byte("[\"events\"] = {\n},")); len(got) != 0 {
			t.Errorf("got %v", got)
		}
		if got := ParseEvents([]byte("DOINKDB = nil")); len(got) != 0 {
			t.Errorf("got %v", got)
		}
	})

	t.Run("unexpected token aborts block", func(t *testing.T) {
		captureWarnings(t)
		file := `["events"] = { [1] = "x", ` + ev("Flano", 1) + " }\n" + block(ev("Second", 2))
		got := charsSeqs(ParseEvents([]byte(file)))
		if !reflect.DeepEqual(got, []cs{{"Second", 2}}) {
			t.Errorf("got %v", got)
		}
	})

	t.Run("unterminated string aborts block", func(t *testing.T) {
		captureWarnings(t)
		file := block(ev("Flano", 1), `"{\"seq\":2`, ev("Flano", 3))
		if got := seqs(ParseEvents([]byte(file))); !reflect.DeepEqual(got, []int{1}) {
			t.Errorf("got %v", got)
		}
	})
}

func TestNames(t *testing.T) {
	e := Event{Char: "Flano", Surname: "Wren", Realm: "R"}
	if e.FullName() != "Flano Wren" || e.Key() != "Flano Wren-R" || e.LegacyKey() != "Flano-R" {
		t.Errorf("%q %q %q", e.FullName(), e.Key(), e.LegacyKey())
	}
	e.Surname = ""
	if e.FullName() != "Flano" || e.Key() != "Flano-R" {
		t.Errorf("%q %q", e.FullName(), e.Key())
	}
}

func TestParseWebhooks(t *testing.T) {
	t.Run("wow style", func(t *testing.T) {
		file := "DOINKDB = {\n[\"webhooks\"] = {\n" +
			"[\"*\"] = \"https://discord.com/api/webhooks/1/a\", -- [1]\n" +
			"[\"Pàul-R\"] = \"https://discord.com/api/webhooks/2/b\",\n" +
			"},\n}"
		got := ParseWebhooks([]byte(file))
		want := map[string]string{
			"*":      "https://discord.com/api/webhooks/1/a",
			"Pàul-R": "https://discord.com/api/webhooks/2/b",
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v", got)
		}
	})

	t.Run("no block", func(t *testing.T) {
		if got := ParseWebhooks([]byte("DOINKDB = {}")); len(got) != 0 || got == nil {
			t.Errorf("got %#v", got)
		}
	})

	t.Run("malformed second entry keeps the first", func(t *testing.T) {
		warnings := captureWarnings(t)
		file := "[\"webhooks\"] = {\n[\"*\"] = \"https://discord.com/api/webhooks/1/a\",\n[\"x\"] = 5,\n}"
		got := ParseWebhooks([]byte(file))
		if !reflect.DeepEqual(got, map[string]string{"*": "https://discord.com/api/webhooks/1/a"}) {
			t.Errorf("got %v", got)
		}
		if len(warnings.msgs) != 1 {
			t.Errorf("warnings %q", warnings.msgs)
		}
	})
}
