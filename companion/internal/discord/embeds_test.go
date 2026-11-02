package discord

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ryan-flan/DOINK/companion/internal/savedvars"
	"github.com/ryan-flan/DOINK/companion/internal/wowhead"
)

func event(kind string, d map[string]any) savedvars.Event {
	return savedvars.Event{Char: "Flano", Realm: "R", Class: "WARRIOR", Seq: 1,
		TS: 1790870764, HasTS: true, Type: kind, Data: d}
}

func fieldValues(e Embed) []string {
	fs, _ := e["fields"].([]map[string]any)
	out := []string{}
	for _, f := range fs {
		out = append(out, f["value"].(string))
	}
	return out
}

func TestLoot(t *testing.T) {
	e := BuildEmbed(event("loot", map[string]any{
		"item_id": 19019.0, "name": "Thunderfury, Blessed Blade of the Windseeker",
		"link": "|cnIQ5:|Hitem:19019::::::::|h[Thunderfury]|h|r", "quality": 5.0, "qty": 1.0,
		"vendor_value": 255355.0}), nil)
	if e["title"] != "Flano looted Thunderfury, Blessed Blade of the Windseeker" {
		t.Errorf("title %q", e["title"])
	}
	if e["url"] != "https://www.wowhead.com/forever/item=19019" {
		t.Errorf("url %q", e["url"])
	}
	if e["color"] != 0xFF8000 {
		t.Errorf("color %x", e["color"])
	}
	if got := fieldValues(e); !reflect.DeepEqual(got, []string{"25g 53s 55c"}) {
		t.Errorf("fields %v", got)
	}
	if fs := e["fields"].([]map[string]any); fs[0]["name"] != "Vendor value" || fs[0]["inline"] != true {
		t.Errorf("field %v", fs[0])
	}
}

func TestLootStackWithoutVendorValue(t *testing.T) {
	e := BuildEmbed(event("loot", map[string]any{"item_id": 2589.0, "name": "Linen Cloth",
		"quality": 1.0, "qty": 3.0, "vendor_value": 0.0}), nil)
	if e["title"] != "Flano looted Linen Cloth ×3" {
		t.Errorf("title %q", e["title"])
	}
	if _, ok := e["fields"]; ok {
		t.Error("has fields")
	}
	if e["color"] != 0xFFFFFF {
		t.Errorf("color %x", e["color"])
	}
}

func TestLootFallbacks(t *testing.T) {
	e := BuildEmbed(event("loot", map[string]any{"name": nil, "quality": 9.0}), nil)
	if e["title"] != "Flano looted an item" {
		t.Errorf("title %q", e["title"])
	}
	if _, ok := e["url"]; ok {
		t.Error("url without an item id")
	}
	if e["color"] != 0xC79C6E {
		t.Errorf("out-of-range quality: color %x", e["color"])
	}
	three := 3
	item := &wowhead.Item{ID: 6314, Name: "Wolfmaster Cape", Quality: &three, IconURL: "https://x/icon.jpg", Lines: []string{"Back"}}
	e = BuildEmbed(event("loot", map[string]any{"item_id": 6314.0, "name": nil, "quality": nil}), item)
	if e["title"] != "Flano looted Wolfmaster Cape" || e["color"] != 0x0070DD {
		t.Errorf("title %q color %x", e["title"], e["color"])
	}
	if e["description"] != "Back" || !reflect.DeepEqual(e["thumbnail"], map[string]any{"url": "https://x/icon.jpg"}) {
		t.Errorf("description %q thumbnail %v", e["description"], e["thumbnail"])
	}
}

func TestDeath(t *testing.T) {
	cases := []struct {
		data         map[string]any
		title, descr string
	}{
		{map[string]any{"zone": "Westfall", "subzone": "Moonbrook", "killer": "Defias Pillager"},
			"Flano died to Defias Pillager", "in Moonbrook, Westfall"},
		{map[string]any{"zone": "Westfall", "subzone": nil, "killer": nil},
			"Flano died", "in Westfall"},
		{map[string]any{"zone": "Thousand Needles", "subzone": nil, "killer": nil, "environment": "FALLING"},
			"Flano died", "Forgot they couldn't fly. Died in Thousand Needles."},
		{map[string]any{"zone": "X", "killer": nil, "environment": "MOON"},
			"Flano died", "in X"},
		{map[string]any{"zone": "X", "killer": "Shark", "environment": "DROWNING"},
			"Flano died to Shark", "in X"},
		{map[string]any{}, "Flano died", "in somewhere"},
	}
	for _, c := range cases {
		e := BuildEmbed(event("death", c.data), nil)
		if e["title"] != c.title || e["description"] != c.descr {
			t.Errorf("%v: got %q / %q", c.data, e["title"], e["description"])
		}
		if e["color"] != 0xC79C6E {
			t.Errorf("color %x", e["color"])
		}
	}
}

func TestQuest(t *testing.T) {
	e := BuildEmbed(event("quest", map[string]any{"quest_id": 783.0, "title": "A Threat Within", "xp": 1200.0}), nil)
	if e["title"] != "Flano completed A Threat Within" || e["url"] != "https://www.wowhead.com/forever/quest=783" {
		t.Errorf("got %v", e)
	}
	if got := fieldValues(e); !reflect.DeepEqual(got, []string{"1,200"}) {
		t.Errorf("fields %v", got)
	}
	e = BuildEmbed(event("quest", map[string]any{"xp": 0.0}), nil)
	if e["title"] != "Flano completed a quest" {
		t.Errorf("title %q", e["title"])
	}
	if _, ok := e["fields"]; ok {
		t.Error("XP field for 0 xp")
	}
	if got := thousands(1234567); got != "1,234,567" {
		t.Errorf("thousands %q", got)
	}
}

func TestBossKill(t *testing.T) {
	e := BuildEmbed(event("boss_kill", map[string]any{"encounter_id": 672.0, "name": "Ragnaros",
		"instance": "Molten Core", "difficulty": 9.0, "success": true, "group_size": 40.0}), nil)
	if e["title"] != "Flano defeated Ragnaros" {
		t.Errorf("title %q", e["title"])
	}
	if got := fieldValues(e); !reflect.DeepEqual(got, []string{"Molten Core", "40"}) {
		t.Errorf("fields %v", got)
	}
	e = BuildEmbed(event("boss_kill", map[string]any{}), nil)
	if e["title"] != "Flano defeated a boss" {
		t.Errorf("title %q", e["title"])
	}
	if _, ok := e["fields"]; ok {
		t.Error("fields without data")
	}
}

func TestSkillUp(t *testing.T) {
	e := BuildEmbed(event("skill_up", map[string]any{"skill": "Blacksmithing", "rank": 150.0, "max_rank": 225.0}), nil)
	if e["title"] != "Flano reached 150 Blacksmithing" || e["description"] != "150 / 225" {
		t.Errorf("got %q / %q", e["title"], e["description"])
	}
	e = BuildEmbed(event("skill_up", map[string]any{"max_rank": nil}), nil)
	if e["title"] != "Flano reached ? a skill" {
		t.Errorf("title %q", e["title"])
	}
	if _, ok := e["description"]; ok {
		t.Error("description without max_rank")
	}
}

func TestLevelUp(t *testing.T) {
	e := BuildEmbed(event("level_up", map[string]any{"level": 20.0}), nil)
	if e["title"] != "Flano reached level 20" {
		t.Errorf("title %q", e["title"])
	}
	e = BuildEmbed(event("level_up", map[string]any{}), nil)
	if e["title"] != "Flano reached level ?" {
		t.Errorf("title %q", e["title"])
	}
}

func TestSurnameInTitleAndFooter(t *testing.T) {
	ev := event("level_up", map[string]any{"level": 10.0})
	ev.Surname = "Wren"
	e := BuildEmbed(ev, nil)
	if e["title"] != "Flano Wren reached level 10" {
		t.Errorf("title %q", e["title"])
	}
	if !reflect.DeepEqual(e["footer"], map[string]any{"text": "Flano Wren-R"}) {
		t.Errorf("footer %v", e["footer"])
	}
}

func TestTestEventsAndTimestamp(t *testing.T) {
	ev := event("level_up", map[string]any{"level": 9.0})
	ev.Realm, ev.Test = "Classic Beta PvE 2", true
	e := BuildEmbed(ev, nil)
	if e["title"] != "[TEST] Flano reached level 9" {
		t.Errorf("title %q", e["title"])
	}
	if !reflect.DeepEqual(e["footer"], map[string]any{"text": "Flano-Classic Beta PvE 2 · test event"}) {
		t.Errorf("footer %v", e["footer"])
	}
	ts := e["timestamp"].(string)
	if ts != "2026-10-01T16:06:04+00:00" || !strings.HasSuffix(ts, "+00:00") {
		t.Errorf("timestamp %q", ts)
	}
	ev.HasTS = false
	if _, ok := BuildEmbed(ev, nil)["timestamp"]; ok {
		t.Error("timestamp without ts")
	}
}

func TestColours(t *testing.T) {
	ev := event("level_up", map[string]any{"level": 2.0})
	ev.Class = "MAGE"
	if c := BuildEmbed(ev, nil)["color"]; c != 0x69CCF0 {
		t.Errorf("mage %x", c)
	}
	ev.Class = ""
	if c := BuildEmbed(ev, nil)["color"]; c != 0x99AAB5 {
		t.Errorf("default %x", c)
	}
}

func TestUnknownTypeFallsBack(t *testing.T) {
	ev := savedvars.Event{Char: "Flano", Realm: "R", Seq: 1, Type: "achievement",
		Data: map[string]any{"name": "Explore Elwynn Forest", "points": 10.0, "html": "<b>&</b>"}}
	e := BuildEmbed(ev, nil)
	if e["title"] != "Flano: achievement" {
		t.Errorf("title %q", e["title"])
	}
	d := e["description"].(string)
	if !strings.Contains(d, "Explore Elwynn Forest") || !strings.HasPrefix(d, "```json\n{\n  \"") ||
		!strings.Contains(d, `"points": 10`) || !strings.Contains(d, "<b>&</b>") {
		t.Errorf("description %q", d)
	}
	big := savedvars.Event{Char: "F", Realm: "R", Type: "x", Data: map[string]any{"s": strings.Repeat("é", 3000)}}
	d = BuildEmbed(big, nil)["description"].(string)
	if len(d) > 3900+len("```json\n\n```") || !strings.HasSuffix(d, "\n```") {
		t.Errorf("len %d", len(d))
	}
}

func TestFormatMoney(t *testing.T) {
	for copper, want := range map[int]string{0: "0c", 5: "5c", 10000: "1g", 10203: "1g 2s 3c", 150: "1s 50c"} {
		if got := FormatMoney(copper); got != want {
			t.Errorf("%d: %q", copper, got)
		}
	}
}

func TestIsWebhookURL(t *testing.T) {
	for url, want := range map[string]bool{
		"https://discord.com/api/webhooks/123/abc_DEF-9":   true,
		"https://discordapp.com/api/webhooks/1/x":          true,
		"https://discord.com/api/webhooks/123/abc\n":       false,
		"http://discord.com/api/webhooks/1/x":              false,
		"https://evil.example/api/webhooks/1/x":            false,
		"https://discord.com/api/webhooks/abc/x":           false,
		"https://discord.com/api/webhooks/1/x?thread_id=2": false,
	} {
		if IsWebhookURL(url) != want {
			t.Errorf("%q: want %v", url, want)
		}
	}
}

func TestConnectionTestEmbed(t *testing.T) {
	e := ConnectionTestEmbed("0.12.0")
	if e["title"] != "DOINK is connected" || e["color"] != 0xF2B83C ||
		!reflect.DeepEqual(e["footer"], map[string]any{"text": "DOINK companion v0.12.0"}) {
		t.Errorf("got %v", e)
	}
}

func TestMount(t *testing.T) {
	item := &wowhead.Item{ID: 5656, Name: "Reins of the Gray Wolf", IconURL: "https://x/wolf.jpg",
		Lines: []string{"Use: Summons a Gray Wolf."}}
	e := BuildEmbed(event("mount", map[string]any{"name": "Gray Wolf", "item_id": 5656.0,
		"spell_id": nil, "link": "|cnIQ3:|Hitem:5656::::::::|h[Gray Wolf]|h|r", "bought": true}), item)
	if e["title"] != "Flano bought a new mount: Gray Wolf" {
		t.Errorf("title %q", e["title"])
	}
	if e["url"] != "https://www.wowhead.com/forever/item=5656" {
		t.Errorf("url %q", e["url"])
	}
	if e["description"] != "Use: Summons a Gray Wolf." || e["thumbnail"] == nil {
		t.Errorf("item details %v / %v", e["description"], e["thumbnail"])
	}
	if e["color"] != 0xC79C6E {
		t.Errorf("color %x", e["color"])
	}

	e = BuildEmbed(event("mount", map[string]any{"name": "Swift Razzashi Raptor", "item_id": nil,
		"spell_id": 24242.0, "link": nil, "bought": false}), nil)
	if e["title"] != "Flano got a new mount: Swift Razzashi Raptor" {
		t.Errorf("title %q", e["title"])
	}
	if e["url"] != "https://www.wowhead.com/forever/spell=24242" {
		t.Errorf("url %q", e["url"])
	}
	if _, ok := e["description"]; ok {
		t.Error("description without an item")
	}

	e = BuildEmbed(event("mount", map[string]any{"name": "Odd", "item_id": 1.5, "spell_id": "x"}), nil)
	if _, ok := e["url"]; ok {
		t.Errorf("url from non-integers: %v", e["url"])
	}
}

func TestDuel(t *testing.T) {
	cases := []struct {
		d           map[string]any
		title, desc string
		colour      int
	}{
		{map[string]any{"opponent": "Grakkor", "won": true, "fled": false, "makgora": false},
			"Flano won a duel against Grakkor", "", 0xC79C6E},
		{map[string]any{"opponent": "Grakkor", "won": true, "fled": true, "makgora": false},
			"Flano won a duel against Grakkor", "Grakkor ran away.", 0xC79C6E},
		{map[string]any{"opponent": "Grakkor", "won": false, "fled": false, "makgora": false},
			"Flano lost a duel to Grakkor", "", 0xC79C6E},
		{map[string]any{"opponent": "Grakkor", "won": false, "fled": true, "makgora": false},
			"Flano lost a duel to Grakkor", "Ran away mid-duel.", 0xC79C6E},
		{map[string]any{"opponent": "Grakkor", "won": true, "fled": false, "makgora": true},
			"Flano won a Mak'gora against Grakkor", "", 0x8B0000},
		{map[string]any{"opponent": "Grakkor", "won": false, "fled": false, "makgora": true},
			"Flano lost a Mak'gora to Grakkor", "", 0x8B0000},
	}
	for _, c := range cases {
		e := BuildEmbed(event("duel", c.d), nil)
		desc, _ := e["description"].(string)
		if e["title"] != c.title || desc != c.desc || e["color"] != c.colour {
			t.Errorf("%v: got %q / %q / %x", c.d, e["title"], desc, e["color"])
		}
	}
}

func TestBattleground(t *testing.T) {
	e := BuildEmbed(event("battleground", map[string]any{"name": "Warsong Gulch", "won": true}), nil)
	if e["title"] != "Flano won Warsong Gulch" || e["description"] != "Victory in Warsong Gulch." {
		t.Errorf("won: %q / %q", e["title"], e["description"])
	}
	e = BuildEmbed(event("battleground", map[string]any{"name": "Arathi Basin", "won": false}), nil)
	if e["title"] != "Flano lost Arathi Basin" || e["description"] != "Defeat in Arathi Basin." {
		t.Errorf("lost: %q / %q", e["title"], e["description"])
	}
	if e["color"] != 0xC79C6E {
		t.Errorf("color %x", e["color"])
	}
}

func TestBossKillFirstKillAndGroup(t *testing.T) {
	e := BuildEmbed(event("boss_kill", map[string]any{"name": "Edwin VanCleef", "instance": "Deadmines",
		"group_size": 5.0, "first_kill": true,
		"group": []any{"Grakkor Highmountain (Shaman 24)", "Mira (Mage 22)"}}), nil)
	if e["title"] != "Flano defeated Edwin VanCleef" || e["description"] != "First kill!" {
		t.Errorf("%q / %q", e["title"], e["description"])
	}
	if got := fieldValues(e); !reflect.DeepEqual(got, []string{"Deadmines", "5",
		"Grakkor Highmountain (Shaman 24), Mira (Mage 22)"}) {
		t.Errorf("fields %v", got)
	}
	if fs := e["fields"].([]map[string]any); fs[2]["name"] != "Group" {
		t.Errorf("field %v", fs[2])
	}

	e = BuildEmbed(event("boss_kill", map[string]any{"name": "Edwin VanCleef", "first_kill": false,
		"group": nil}), nil)
	if _, ok := e["description"]; ok {
		t.Error("description on a repeat kill")
	}
	if _, ok := e["fields"]; ok {
		t.Errorf("fields %v", e["fields"])
	}
	e = BuildEmbed(event("boss_kill", map[string]any{"name": "X", "group": []any{}}), nil)
	if _, ok := e["fields"]; ok {
		t.Error("empty group made a field")
	}
}
