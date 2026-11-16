// Package toon summarises the player's characters for a status display:
// which one logged in last, its class and level, and what DOINK recorded
// for it lately.
//
// "Last logged in" comes from the game's own per-character folder,
// <flavor>/WTF/Account/<account>/<realm>/<Name-Surname>/, whose files the
// client rewrites at logout and /reload. The realm folder there is a
// number, not the realm's name, so folders are matched to DOINK's
// characters by full name only. Class, level and the recent events come
// from DOINK.lua; a character the folder knows but DOINK hasn't recorded
// anything for has only a name and a time.
package toon

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ryan-flan/DOINK/companion/internal/savedvars"
)

// RecentMax is how many events a Toon carries, newest first.
const RecentMax = 8

// Event is one recorded event, worded for a list.
type Event struct {
	Type string `json:"type"`
	TS   int64  `json:"ts"`
	Text string `json:"text"`
}

// Toon is one character.
type Toon struct {
	Name    string `json:"name"`
	Surname string `json:"surname,omitempty"`
	Realm   string `json:"realm,omitempty"`
	Class   string `json:"class,omitempty"` // WARRIOR, MAGE, ...; "" when unknown
	Level   int    `json:"level,omitempty"` // 0 when unknown
	// LastSeen is when the character was last logged in (Unix seconds),
	// from its WTF folder, else its newest event; 0 when unknown.
	LastSeen int64 `json:"last_seen,omitempty"`
	// Played is the total time played in seconds from the newest event
	// that carries it (/doink played on); 0 when unknown.
	Played int64          `json:"played,omitempty"`
	Counts map[string]int `json:"counts"` // queued events per type
	Recent []Event        `json:"recent"` // newest first
}

// FullName is "Name Surname", or "Name".
func (t Toon) FullName() string {
	if t.Surname == "" {
		return t.Name
	}
	return t.Name + " " + t.Surname
}

// Summary is every character, the last one logged in first.
type Summary struct {
	Last   *Toon  `json:"last"` // nil when nothing is known
	Others []Toon `json:"others"`
}

// charKey matches a character entry in DOINKDB.chars: ["Name Surname-Realm"] = {
var charKey = regexp.MustCompile(`(?m)^\s*\["([^"\-]+)-([^"]+)"\]\s*=\s*\{`)

// Read builds the summary from DOINK.lua files (as config's
// SavedVariablesPaths). Unreadable files are skipped.
func Read(paths []string) Summary {
	byName := map[string]*Toon{} // full name + realm
	order := []string{}
	get := func(full, realm string) *Toon {
		key := full + "-" + realm
		if t, ok := byName[key]; ok {
			return t
		}
		name, surname, _ := strings.Cut(full, " ")
		t := &Toon{Name: name, Surname: surname, Realm: realm, Counts: map[string]int{}}
		byName[key] = t
		order = append(order, key)
		return t
	}
	seen := map[string]int64{} // full name -> newest WTF folder time
	for _, path := range paths {
		for name, at := range lastLogins(path) {
			if at > seen[name] {
				seen[name] = at
			}
		}
		file, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, m := range charKey.FindAllSubmatch(file, -1) {
			get(string(m[1]), string(m[2]))
		}
		events := savedvars.ParseEvents(file)
		sort.SliceStable(events, func(i, j int) bool { return events[i].Seq < events[j].Seq })
		for _, e := range events {
			if e.Test {
				continue
			}
			t := get(e.FullName(), e.Realm)
			if e.Class != "" {
				t.Class = e.Class
			}
			if e.Level > 0 {
				t.Level = e.Level
			}
			if e.Type == "level_up" {
				if l, ok := e.Data["level"].(float64); ok && int(l) > t.Level {
					t.Level = int(l)
				}
			}
			if e.HasPlayed {
				t.Played = e.Played
			}
			if e.HasTS && e.TS > t.LastSeen {
				t.LastSeen = e.TS
			}
			t.Counts[e.Type]++
			t.Recent = append([]Event{{Type: e.Type, TS: e.TS, Text: Describe(e)}}, t.Recent...)
			if len(t.Recent) > RecentMax {
				t.Recent = t.Recent[:RecentMax]
			}
		}
	}
	// Characters the game knows but DOINK has no entry for.
	known := map[string]bool{}
	for _, t := range byName {
		known[t.FullName()] = true
	}
	for full := range seen {
		if !known[full] {
			get(full, "")
		}
	}

	all := make([]Toon, 0, len(order))
	for _, key := range order {
		t := byName[key]
		if at := seen[t.FullName()]; at > 0 {
			t.LastSeen = at // the folder beats the newest event
		}
		if t.Recent == nil {
			t.Recent = []Event{}
		}
		all = append(all, *t)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].LastSeen > all[j].LastSeen })
	s := Summary{Others: []Toon{}}
	if len(all) > 0 {
		s.Last = &all[0]
		s.Others = all[1:]
	}
	return s
}

// lastLogins maps "Name Surname" to the newest modification time in that
// character's WTF folder, for the account that owns this DOINK.lua.
func lastLogins(svPath string) map[string]int64 {
	account := filepath.Dir(filepath.Dir(svPath)) // .../Account/<acct>
	out := map[string]int64{}
	if filepath.Base(filepath.Dir(svPath)) != "SavedVariables" || filepath.Base(filepath.Dir(account)) != "Account" {
		return out // not a game folder (a hand-set savedvariables_path)
	}
	realms, err := os.ReadDir(account)
	if err != nil {
		return out
	}
	for _, realm := range realms {
		if !realm.IsDir() || realm.Name() == "SavedVariables" {
			continue
		}
		chars, err := os.ReadDir(filepath.Join(account, realm.Name()))
		if err != nil {
			continue
		}
		for _, char := range chars {
			if !char.IsDir() {
				continue
			}
			var newest int64
			entries, _ := os.ReadDir(filepath.Join(account, realm.Name(), char.Name()))
			for _, entry := range entries {
				if info, err := entry.Info(); err == nil && info.ModTime().Unix() > newest {
					newest = info.ModTime().Unix()
				}
			}
			// Folder names are "Name-Surname" (Forever) or "Name".
			full := strings.Replace(char.Name(), "-", " ", 1)
			if newest > out[full] {
				out[full] = newest
			}
		}
	}
	return out
}

func str(data map[string]any, key string) string {
	s, _ := data[key].(string)
	return s
}

// Describe words an event for a short list line ("Reached level 26").
func Describe(e savedvars.Event) string {
	d := e.Data
	switch e.Type {
	case "level_up":
		if l, ok := d["level"].(float64); ok {
			return fmt.Sprintf("Reached level %d", int(l))
		}
		return "Levelled up"
	case "quest":
		if title := str(d, "title"); title != "" {
			return "Quest: " + title
		}
		return "Quest turned in"
	case "death":
		where := str(d, "zone")
		if sub := str(d, "subzone"); sub != "" {
			where = sub
		}
		text := "Died"
		if where != "" {
			text += " in " + where
		}
		if killer := str(d, "killer"); killer != "" {
			text += " (" + killer + ")"
		} else if env := str(d, "environment"); env != "" {
			text += " (" + strings.ToLower(env) + ")"
		}
		return text
	case "boss_kill":
		text := "Killed " + str(d, "name")
		if inst := str(d, "instance"); inst != "" {
			text += " (" + inst + ")"
		}
		return text
	case "loot":
		return "Looted " + str(d, "name")
	case "skill_up":
		if r, ok := d["rank"].(float64); ok {
			return fmt.Sprintf("%s %d", str(d, "skill"), int(r))
		}
		return str(d, "skill")
	case "mount":
		if name := str(d, "name"); name != "" {
			return "New mount: " + name
		}
		return "New mount"
	case "duel":
		if won, _ := d["won"].(bool); won {
			return "Won a duel vs " + str(d, "opponent")
		}
		return "Lost a duel vs " + str(d, "opponent")
	case "battleground":
		if won, _ := d["won"].(bool); won {
			return "Won " + str(d, "name")
		}
		return "Lost " + str(d, "name")
	}
	return e.Type
}
