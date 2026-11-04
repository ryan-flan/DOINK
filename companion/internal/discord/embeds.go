// Package discord builds the embeds DOINK posts and sends them to webhooks.
package discord

import (
	"bytes"
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ryan-flan/DOINK/companion/internal/savedvars"
	"github.com/ryan-flan/DOINK/companion/internal/wowhead"
)

// Embed is one Discord embed, as JSON.
type Embed map[string]any

// RAID_CLASS_COLORS as of Classic Era.
var classColours = map[string]int{
	"WARRIOR": 0xC79C6E,
	"PALADIN": 0xF58CBA,
	"HUNTER":  0xABD473,
	"ROGUE":   0xFFF569,
	"PRIEST":  0xFFFFFF,
	"SHAMAN":  0x0070DE,
	"MAGE":    0x69CCF0,
	"WARLOCK": 0x9482C9,
	"DRUID":   0xFF7D0A,
}

const defaultColour = 0x99AAB5

// ITEM_QUALITY_COLORS: poor, common, uncommon, rare, epic, legendary, artifact.
var qualityColours = []int{0x9D9D9D, 0xFFFFFF, 0x1EFF00, 0x0070DD, 0xA335EE, 0xFF8000, 0xE6CC80}

// Wowhead is the base for item and quest links (verified against
// wowhead.com/forever/item=769 and /quest=783).
const Wowhead = "https://www.wowhead.com/forever"

// environment: death.environment -> what happened, third person.
var environment = map[string]string{
	"FALLING":  "Forgot they couldn't fly.",
	"DROWNING": "Forgot they couldn't swim.",
	"FATIGUE":  "Swam too far.",
	"FIRE":     "Stood in the fire.",
	"LAVA":     "Went for a swim in lava.",
	"SLIME":    "Took a bath in slime.",
}

// FormatMoney writes copper as "1g 2s 3c", leaving out zero parts.
func FormatMoney(copper int) string {
	gold, rest := copper/10000, copper%10000
	silver, copper := rest/100, rest%100
	var parts []string
	if gold != 0 {
		parts = append(parts, strconv.Itoa(gold)+"g")
	}
	if silver != 0 {
		parts = append(parts, strconv.Itoa(silver)+"s")
	}
	if copper != 0 || len(parts) == 0 {
		parts = append(parts, strconv.Itoa(copper)+"c")
	}
	return strings.Join(parts, " ")
}

var webhookURL = regexp.MustCompile(`^https://(discord|discordapp)\.com/api/webhooks/[0-9]+/[A-Za-z0-9_-]+$`)

// IsWebhookURL reports whether url looks like a Discord webhook URL.
func IsWebhookURL(url string) bool { return webhookURL.MatchString(url) }

// data reads an event's type-specific fields.
type data map[string]any

// str is the string value of key, or "".
func (d data) str(key string) string {
	s, _ := d[key].(string)
	return s
}

// num is the number value of key.
func (d data) num(key string) (float64, bool) {
	f, ok := d[key].(float64)
	return f, ok
}

// isInt reports whether key holds a whole number.
func (d data) isInt(key string) bool {
	f, ok := d.num(key)
	return ok && f == math.Trunc(f) && !math.IsInf(f, 0)
}

// truthy: present, not null, not false, not 0, not "".
func (d data) truthy(key string) bool {
	switch v := d[key].(type) {
	case nil:
		return false
	case bool:
		return v
	case float64:
		return v != 0
	case string:
		return v != ""
	}
	return true
}

func number(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// text is a field value as Discord should show it.
func text(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return number(t)
	case bool:
		return strconv.FormatBool(t)
	}
	raw, _ := json.Marshal(v)
	return string(raw)
}

// Discord's embed limits; a message over any of them is rejected whole.
const (
	maxTitle       = 256
	maxDescription = 4096
	maxFieldValue  = 1024
	maxFooter      = 2048
)

// clip cuts s to at most n runes, marking the cut with an ellipsis.
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n-1]) + "…"
}

func field(name, value string) map[string]any {
	return map[string]any{"name": name, "value": clip(value, maxFieldValue), "inline": true}
}

// thousands writes n with comma separators: 1200 -> "1,200".
func thousands(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

func levelUp(name string, d data) Embed {
	level := "?"
	if f, ok := d.num("level"); ok {
		level = number(f)
	}
	return Embed{"title": name + " reached level " + level}
}

func loot(name string, d data, item *wowhead.Item) Embed {
	what := d.str("name")
	if what == "" && item != nil {
		what = item.Name
	}
	if what == "" {
		what = "an item"
	}
	title := name + " looted " + what
	if qty, ok := d.num("qty"); ok && qty > 1 {
		title += " ×" + number(qty)
	}
	e := Embed{"title": title}
	if d.isInt("item_id") {
		id, _ := d.num("item_id")
		e["url"] = Wowhead + "/item=" + number(id)
	}
	if q, ok := d.num("quality"); ok && d.isInt("quality") && q >= 0 && int(q) < len(qualityColours) {
		e["color"] = qualityColours[int(q)]
	} else if item != nil && item.Quality != nil && *item.Quality >= 0 && *item.Quality < len(qualityColours) {
		e["color"] = qualityColours[*item.Quality]
	}
	if item != nil {
		if item.IconURL != "" {
			e["thumbnail"] = map[string]any{"url": item.IconURL}
		}
		if len(item.Lines) > 0 {
			e["description"] = strings.Join(item.Lines, "\n")
		}
	}
	if v, ok := d.num("vendor_value"); ok && d.truthy("vendor_value") {
		e["fields"] = []map[string]any{field("Vendor value", FormatMoney(int(v)))}
	}
	return e
}

func death(name string, d data) Embed {
	killer := d.str("killer")
	where := d.str("zone")
	if where == "" {
		where = "somewhere"
	}
	if sub := d.str("subzone"); sub != "" {
		where = sub + ", " + where
	}
	description := "in " + where
	if blunder, ok := environment[d.str("environment")]; ok && killer == "" {
		description = blunder + " Died in " + where + "."
	}
	title := name + " died"
	if killer != "" {
		title += " to " + killer
	}
	return Embed{"title": title, "description": description}
}

func quest(name string, d data) Embed {
	title := d.str("title")
	if title == "" {
		title = "a quest"
	}
	e := Embed{"title": name + " completed " + title}
	if d.isInt("quest_id") {
		id, _ := d.num("quest_id")
		e["url"] = Wowhead + "/quest=" + number(id)
	}
	if xp, ok := d.num("xp"); ok && d.truthy("xp") {
		e["fields"] = []map[string]any{field("XP", thousands(int64(xp)))}
	}
	return e
}

func bossKill(name string, d data) Embed {
	boss := d.str("name")
	if boss == "" {
		boss = "a boss"
	}
	e := Embed{"title": name + " defeated " + boss}
	if d.truthy("first_kill") {
		e["description"] = "First kill!"
	}
	var fields []map[string]any
	if d.truthy("instance") {
		fields = append(fields, field("Instance", text(d["instance"])))
	}
	if d.truthy("group_size") {
		fields = append(fields, field("Group size", text(d["group_size"])))
	}
	if group := stringList(d["group"]); len(group) > 0 {
		fields = append(fields, field("Group", strings.Join(group, ", ")))
	}
	if len(fields) > 0 {
		e["fields"] = fields
	}
	return e
}

// stringList is the strings in a JSON array; anything else gives nil.
func stringList(v any) []string {
	list, _ := v.([]any)
	var out []string
	for _, item := range list {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func mount(name string, d data, item *wowhead.Item) Embed {
	what := d.str("name")
	if what == "" && item != nil {
		what = item.Name
	}
	if what == "" {
		what = "a mount"
	}
	verb := " got a new mount: "
	if d.truthy("bought") {
		verb = " bought a new mount: "
	}
	e := Embed{"title": name + verb + what}
	if d.isInt("item_id") {
		id, _ := d.num("item_id")
		e["url"] = Wowhead + "/item=" + number(id)
	} else if d.isInt("spell_id") {
		id, _ := d.num("spell_id")
		e["url"] = Wowhead + "/spell=" + number(id)
	}
	if item != nil {
		if item.IconURL != "" {
			e["thumbnail"] = map[string]any{"url": item.IconURL}
		}
		if len(item.Lines) > 0 {
			e["description"] = strings.Join(item.Lines, "\n")
		}
	}
	return e
}

// makgoraColour: dark red for a duel to the death.
const makgoraColour = 0x8B0000

func duel(name string, d data) Embed {
	opponent := d.str("opponent")
	if opponent == "" {
		opponent = "someone"
	}
	kind := "duel"
	if d.truthy("makgora") {
		kind = "Mak'gora"
	}
	won := d.truthy("won")
	var e Embed
	if won {
		e = Embed{"title": name + " won a " + kind + " against " + opponent}
	} else {
		e = Embed{"title": name + " lost a " + kind + " to " + opponent}
	}
	if d.truthy("fled") {
		if won {
			e["description"] = opponent + " ran away."
		} else {
			e["description"] = "Ran away mid-duel."
		}
	}
	if d.truthy("makgora") {
		e["color"] = makgoraColour
	}
	return e
}

func battleground(name string, d data) Embed {
	bg := d.str("name")
	if bg == "" {
		bg = "a battleground"
	}
	if d.truthy("won") {
		return Embed{"title": name + " won " + bg, "description": "Victory in " + bg + "."}
	}
	return Embed{"title": name + " lost " + bg, "description": "Defeat in " + bg + "."}
}

func skillUp(name string, d data) Embed {
	skill := d.str("skill")
	if skill == "" {
		skill = "a skill"
	}
	rank := "?"
	if f, ok := d.num("rank"); ok {
		rank = number(f)
	}
	e := Embed{"title": name + " reached " + rank + " " + skill}
	if d.truthy("max_rank") {
		e["description"] = rank + " / " + text(d["max_rank"])
	}
	return e
}

// generic is the fallback for types without a builder, so nothing is lost.
func generic(name, kind string, d data) Embed {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]any(d))
	body := strings.TrimRight(buf.String(), "\n")
	if len(body) > 3900 {
		cut := 3900
		for cut > 0 && !utf8.RuneStart(body[cut]) {
			cut--
		}
		body = body[:cut]
	}
	return Embed{"title": name + ": " + kind, "description": "```json\n" + body + "\n```"}
}

// BuildEmbed turns one event into an embed. item is Wowhead's details for a
// loot or mount event, when the lookup is on and succeeded; other types
// ignore it.
func BuildEmbed(e savedvars.Event, item *wowhead.Item) Embed {
	name := e.FullName()
	d := data(e.Data)
	var embed Embed
	switch e.Type {
	case "level_up":
		embed = levelUp(name, d)
	case "loot":
		embed = loot(name, d, item)
	case "death":
		embed = death(name, d)
	case "quest":
		embed = quest(name, d)
	case "boss_kill":
		embed = bossKill(name, d)
	case "skill_up":
		embed = skillUp(name, d)
	case "mount":
		embed = mount(name, d, item)
	case "duel":
		embed = duel(name, d)
	case "battleground":
		embed = battleground(name, d)
	default:
		embed = generic(name, e.Type, d)
	}
	if _, ok := embed["color"]; !ok {
		colour, known := classColours[e.Class]
		if !known {
			colour = defaultColour
		}
		embed["color"] = colour
	}
	footer := e.Key()
	title, _ := embed["title"].(string)
	if e.Test {
		title = "[TEST] " + title
		footer += " · test event"
	}
	embed["title"] = clip(title, maxTitle)
	if description, ok := embed["description"].(string); ok {
		embed["description"] = clip(description, maxDescription)
	}
	embed["footer"] = map[string]any{"text": clip(footer, maxFooter)}
	if e.HasTS {
		embed["timestamp"] = time.Unix(e.TS, 0).UTC().Format("2006-01-02T15:04:05+00:00")
	}
	return embed
}

// ConnectionTestEmbed is what --test-webhook posts.
func ConnectionTestEmbed(version string) Embed {
	return Embed{
		"title":       "DOINK is connected",
		"description": "Level-ups, loot, deaths and more from World of Warcraft will be posted to this channel.",
		"color":       0xF2B83C, // the icon's gold
		"footer":      map[string]any{"text": "DOINK companion v" + version},
	}
}
