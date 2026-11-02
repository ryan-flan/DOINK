package main

import (
	"reflect"
	"testing"
	"time"

	"github.com/ryan-flan/DOINK/companion/internal/discord"
	"github.com/ryan-flan/DOINK/companion/internal/wowhead"
)

type recorder struct {
	urls   []string
	embeds [][]discord.Embed
}

func (r *recorder) Send(url string, embeds []discord.Embed) error {
	r.urls = append(r.urls, url)
	r.embeds = append(r.embeds, embeds)
	return nil
}

func TestTestEventNames(t *testing.T) {
	events, err := testEvents([]string{"levelup", "Boss_Kill", "LOOT"}, time.Unix(1790870764, 0))
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for _, e := range events {
		types = append(types, e.Type)
		if !e.Test || e.Key() != "Flano Wren-DOINK" || !e.HasTS {
			t.Errorf("event %+v", e)
		}
	}
	if !reflect.DeepEqual(types, []string{"level_up", "boss_kill", "loot"}) {
		t.Errorf("types %v", types)
	}
	if all, _ := testEvents([]string{"all"}, time.Now()); len(all) != len(testFixtures) {
		t.Errorf("all gave %d", len(all))
	}
	if _, err := testEvents([]string{"nope"}, time.Now()); err == nil {
		t.Error("unknown type accepted")
	}
	if _, err := testEvents(nil, time.Now()); err == nil {
		t.Error("no type accepted")
	}
}

func TestPostTestEvents(t *testing.T) {
	events, _ := testEvents([]string{"all"}, time.Now())
	var asked []int
	items := wowhead.NewCache(func(id int) *wowhead.Item {
		asked = append(asked, id)
		return &wowhead.Item{ID: id, Name: "Thunderfury", IconURL: "https://x/i.jpg"}
	})
	r := &recorder{}
	if err := postTestEvents(events, "https://discord.com/api/webhooks/1/x", r, items); err != nil {
		t.Fatal(err)
	}
	if len(r.embeds) != 1 || len(r.embeds[0]) != len(testFixtures) {
		t.Fatalf("messages %d", len(r.embeds))
	}
	titles := map[string]bool{}
	for _, e := range r.embeds[0] {
		titles[e["title"].(string)] = true
		if e["title"] == "[TEST] Flano Wren looted Thunderfury, Blessed Blade of the Windseeker" && e["thumbnail"] == nil {
			t.Error("loot not enriched")
		}
	}
	for _, want := range []string{"[TEST] Flano Wren reached level 60", "[TEST] Flano Wren died to Hogger",
		"[TEST] Flano Wren defeated Ragnaros", "[TEST] Flano Wren reached 150 Blacksmithing",
		"[TEST] Flano Wren completed A Threat Within", "[TEST] Flano Wren bought a new mount: Gray Wolf",
		"[TEST] Flano Wren won a duel against Grakkor Highmountain", "[TEST] Flano Wren won Warsong Gulch"} {
		if !titles[want] {
			t.Errorf("missing %q in %v", want, titles)
		}
	}
	for _, e := range r.embeds[0] {
		switch e["title"] {
		case "[TEST] Flano Wren bought a new mount: Gray Wolf":
			if e["thumbnail"] == nil {
				t.Error("mount not enriched")
			}
		case "[TEST] Flano Wren defeated Ragnaros":
			if e["description"] != "First kill!" {
				t.Errorf("boss description %v", e["description"])
			}
		}
	}
	// "all" is sorted, so loot (19019) is looked up before mount (5656).
	if !reflect.DeepEqual(asked, []int{19019, 5656}) {
		t.Errorf("wowhead asked %v", asked)
	}
}
