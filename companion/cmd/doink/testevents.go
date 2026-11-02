package main

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/ryan-flan/DOINK/companion/internal/config"
	"github.com/ryan-flan/DOINK/companion/internal/discord"
	"github.com/ryan-flan/DOINK/companion/internal/savedvars"
	"github.com/ryan-flan/DOINK/companion/internal/wowhead"
)

// testFixtures are the same fake events the addon's /doink test emits
// (each notifier's Test()), so `doink test <type>` shows what Discord gets
// without logging in.
var testFixtures = map[string]map[string]any{
	"level_up": {"level": 60.0},
	"loot": {
		"item_id": 19019.0, "name": "Thunderfury, Blessed Blade of the Windseeker",
		"link":    "|cnIQ5:|Hitem:19019::::::::|h[Thunderfury, Blessed Blade of the Windseeker]|h|r",
		"quality": 5.0, "qty": 1.0, "vendor_value": 255355.0,
	},
	"death":     {"zone": "Westfall", "subzone": "Moonbrook", "killer": "Hogger", "environment": nil},
	"quest":     {"quest_id": 783.0, "title": "A Threat Within", "xp": 40.0},
	"boss_kill": {"encounter_id": 672.0, "name": "Ragnaros", "instance": "Molten Core", "difficulty": 9.0, "success": true, "group_size": 40.0},
	"skill_up":  {"skill": "Blacksmithing", "rank": 150.0, "max_rank": 150.0},
}

func testTypes() []string {
	var types []string
	for t := range testFixtures {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}

// testEvents builds one test event per requested type. Names match the
// addon's: case-insensitive, underscores optional ("levelup", "LevelUp");
// "all" means every type.
func testEvents(names []string, now time.Time) ([]savedvars.Event, error) {
	if len(names) == 0 {
		return nil, fmt.Errorf("usage: doink test <type>... [webhook URL]. Types: all, %s", strings.Join(testTypes(), ", "))
	}
	var picked []string
	for _, name := range names {
		query := strings.ReplaceAll(strings.ToLower(name), "_", "")
		if query == "all" {
			picked = append(picked, testTypes()...)
			continue
		}
		found := ""
		for _, t := range testTypes() {
			if strings.ReplaceAll(t, "_", "") == query {
				found = t
			}
		}
		if found == "" {
			return nil, fmt.Errorf("unknown type %q. Types: all, %s", name, strings.Join(testTypes(), ", "))
		}
		picked = append(picked, found)
	}
	events := make([]savedvars.Event, 0, len(picked))
	for i, t := range picked {
		data := map[string]any{}
		for k, v := range testFixtures[t] {
			data[k] = v
		}
		events = append(events, savedvars.Event{
			Seq: i + 1, Char: "Flano", Surname: "Wren", Realm: "DOINK", Class: "WARRIOR", Level: 59,
			TS: now.Unix(), HasTS: true, Type: t, Test: true, Data: data,
		})
	}
	return events, nil
}

// postTestEvents sends test events to url, ten embeds per message, with
// Wowhead details for loot when items is set. Nothing is recorded in
// state.json: these aren't from the game.
func postTestEvents(events []savedvars.Event, url string, sender discord.Sender, items *wowhead.Cache) error {
	for start := 0; start < len(events); start += discord.MaxEmbedsPerMessage {
		batch := events[start:min(start+discord.MaxEmbedsPerMessage, len(events))]
		embeds := make([]discord.Embed, 0, len(batch))
		for _, e := range batch {
			var item *wowhead.Item
			if id, ok := e.Data["item_id"].(float64); ok && items != nil && e.Type == "loot" {
				item = items.Lookup(int(id))
			}
			embeds = append(embeds, discord.BuildEmbed(e, item))
		}
		if err := sender.Send(url, embeds); err != nil {
			return err
		}
	}
	return nil
}

// runTestEvents is `doink test <type>... [url]`: the webhook is the URL
// given, else webhook_url, else the in-game account-wide one.
func runTestEvents(args []string, cfg *config.Config, log *slog.Logger) int {
	var names []string
	url := ""
	for _, a := range args {
		if strings.HasPrefix(a, "https://") {
			url = a
		} else {
			names = append(names, a)
		}
	}
	events, err := testEvents(names, time.Now())
	if err != nil {
		log.Error(err.Error())
		return 1
	}
	if url == "" {
		url = cfg.WebhookURL
	}
	if url == "" {
		url = gameWebhook(cfg)
	}
	if !cfg.DryRun {
		if url == "" {
			log.Error("no webhook. Pass one (doink test loot <url>), set webhook_url in config.toml, or /doink webhook <url> in game")
			return 1
		}
		if !discord.IsWebhookURL(url) {
			log.Error("that doesn't look like a Discord webhook URL")
			return 1
		}
	}
	d := deps(cfg, nil, nil, log)
	if err := postTestEvents(events, url, d.Sender, d.Items); err != nil {
		log.Error("test post failed: " + err.Error())
		return 1
	}
	verb := "posted"
	if cfg.DryRun {
		verb = "printed"
	}
	var kinds []string
	for _, e := range events {
		kinds = append(kinds, e.Type)
	}
	log.Info(fmt.Sprintf("%s test %s", verb, strings.Join(kinds, ", ")))
	return 0
}
