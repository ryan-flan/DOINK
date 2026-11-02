// Package wowhead looks up item details on Wowhead for richer loot embeds.
package wowhead

// Item is what Wowhead knows about one item.
type Item struct {
	ID      int
	Name    string
	Quality *int     // nil when Wowhead gave none
	IconURL string   // "" when none
	Lines   []string // tooltip text, name excluded
}
