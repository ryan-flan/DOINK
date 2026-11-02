// Package wowhead looks up item details on Wowhead for richer loot embeds.
//
// Wowhead serves a small JSON tooltip per item for the Forever data set:
// {base}/forever/tooltip/item/<id>?locale=0 returns the name, quality, icon
// name and the tooltip as HTML (unknown ids answer 404). Only the item id
// ever leaves the machine. Results are cached in memory for the session.
package wowhead

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Item is what Wowhead knows about one item.
type Item struct {
	ID      int
	Name    string
	Quality *int     // nil when Wowhead gave none
	IconURL string   // "" when none
	Lines   []string // tooltip text, name excluded
}

// MaxLines caps the tooltip lines kept for an embed.
const MaxLines = 12

// DefaultBase is where Fetch asks by default.
const DefaultBase = "https://nether.wowhead.com"

var (
	money    = regexp.MustCompile(`<span class="money(gold|silver|copper)">(\d+)</span>`)
	breaks   = regexp.MustCompile(`(?i)<br\s*/?>|</?(?:td|tr|div|table)\b[^>]*>`)
	tags     = regexp.MustCompile(`(?s)<!--.*?-->|<[^>]+>`)
	iconName = regexp.MustCompile(`^[a-z0-9_]+$`)
)

// TooltipLines returns the tooltip's text lines, in order, without the item
// name, sell price or stack size: "Item Level 27", "Binds when picked up",
// "Back", "22 Armor", ...
func TooltipLines(tooltipHTML string) []string {
	text := money.ReplaceAllStringFunc(tooltipHTML, func(m string) string {
		parts := money.FindStringSubmatch(m)
		return parts[2] + parts[1][:1] + " "
	})
	text = breaks.ReplaceAllString(text, "\n")
	text = html.UnescapeString(tags.ReplaceAllString(text, ""))
	var lines []string
	seen := map[string]bool{}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.Join(strings.Fields(raw), " ")
		if line == "" || seen[line] || strings.HasPrefix(line, "Sell Price") || strings.HasPrefix(line, "Max Stack") {
			continue
		}
		seen[line] = true
		lines = append(lines, line)
	}
	if len(lines) <= 1 {
		return []string{} // the first line is the name
	}
	lines = lines[1:]
	if len(lines) > MaxLines {
		lines = lines[:MaxLines]
	}
	return lines
}

// ParseItem reads Wowhead's tooltip JSON; nil if it isn't usable.
func ParseItem(id int, payload []byte) *Item {
	var data map[string]json.RawMessage
	if json.Unmarshal(payload, &data) != nil || data == nil {
		return nil
	}
	var name string
	if json.Unmarshal(data["name"], &name) != nil || name == "" {
		return nil
	}
	item := &Item{ID: id, Name: name}
	var quality json.Number
	raw := strings.TrimSpace(string(data["quality"]))
	isNumber := raw != "" && (raw[0] == '-' || raw[0] >= '0' && raw[0] <= '9')
	if isNumber && json.Unmarshal(data["quality"], &quality) == nil {
		if q, err := quality.Int64(); err == nil {
			qi := int(q)
			item.Quality = &qi
		}
	}
	var icon string
	if json.Unmarshal(data["icon"], &icon) == nil && iconName.MatchString(icon) {
		item.IconURL = "https://wow.zamimg.com/images/wow/icons/large/" + icon + ".jpg"
	}
	var tooltip string
	_ = json.Unmarshal(data["tooltip"], &tooltip)
	item.Lines = TooltipLines(tooltip)
	return item
}

// Fetch makes one GET to Wowhead; nil on any failure (never panics). A nil
// client gets one with a 5 s timeout.
func Fetch(client *http.Client, base string, version string, id int) *Item {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	if base == "" {
		base = DefaultBase
	}
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/forever/tooltip/item/%d?locale=0", base, id), nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "DOINK-companion/"+version+" (+https://github.com/ryan-flan/DOINK)")
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil
	}
	return ParseItem(id, payload)
}

// Cache remembers looked-up items for the session, misses included, so one
// bad network moment or unknown item never costs more than one request.
type Cache struct {
	mu    sync.Mutex
	items map[int]*Item
	fetch func(int) *Item
}

// NewCache returns a Cache that asks fetch on a miss.
func NewCache(fetch func(int) *Item) *Cache {
	return &Cache{items: map[int]*Item{}, fetch: fetch}
}

// Lookup returns the item, fetching it once per session. id <= 0 → nil.
func (c *Cache) Lookup(id int) *Item {
	if id <= 0 {
		return nil
	}
	c.mu.Lock()
	item, ok := c.items[id]
	c.mu.Unlock()
	if ok {
		return item
	}
	item = c.fetch(id)
	c.mu.Lock()
	c.items[id] = item
	c.mu.Unlock()
	return item
}
