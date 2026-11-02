package wowhead_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ryan-flan/DOINK/companion/internal/discord"
	"github.com/ryan-flan/DOINK/companion/internal/savedvars"
	"github.com/ryan-flan/DOINK/companion/internal/wowhead"
)

func fixture(t *testing.T) []byte {
	raw, err := os.ReadFile("testdata/item_6314.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseFixture(t *testing.T) {
	item := wowhead.ParseItem(6314, fixture(t))
	if item == nil {
		t.Fatal("nil item")
	}
	want := []string{"Item Level 27", "Binds when picked up", "Back", "22 Armor", "+3 Stamina",
		"Requires Level 22", "Equip: +10 Attack Power.", "Equip: +9 Attack Power against Beasts."}
	if !reflect.DeepEqual(item.Lines, want) {
		t.Errorf("lines %q", item.Lines)
	}
	if item.Name != "Wolfmaster Cape" || item.ID != 6314 || item.Quality == nil || *item.Quality != 3 {
		t.Errorf("item %+v", item)
	}
	if item.IconURL != "https://wow.zamimg.com/images/wow/icons/large/inv_misc_cape_10.jpg" {
		t.Errorf("icon %q", item.IconURL)
	}
}

func TestBoarMeat(t *testing.T) {
	html := `<table><tr><td><b class="q1">Chunk of Boar Meat</b><span class="q whtt-extra">` +
		`<br>Item Level 5</span></td></tr></table><table><tr><td><span class="toycolor">` +
		`Crafting Reagent</span><br><div class="whtt-maxstack">Max Stack: 20</div>` +
		`<div class="whtt-sellprice">Sell Price: <span class="moneycopper">4</span></div></td></tr></table>`
	if got := wowhead.TooltipLines(html); !reflect.DeepEqual(got, []string{"Item Level 5", "Crafting Reagent"}) {
		t.Errorf("got %q", got)
	}
}

func TestEntitiesAndLineCap(t *testing.T) {
	html := "<b>X</b><br>Use: &quot;Zap&quot; &amp; run"
	for i := 0; i < 30; i++ {
		html += "<BR/>line " + strings.Repeat("i", i)
	}
	lines := wowhead.TooltipLines(html)
	if lines[0] != `Use: "Zap" & run` {
		t.Errorf("first %q", lines[0])
	}
	if len(lines) != wowhead.MaxLines {
		t.Errorf("len %d", len(lines))
	}
	if got := wowhead.TooltipLines("<b>Only a name</b>"); len(got) != 0 {
		t.Errorf("got %q", got)
	}
}

func TestBadPayloads(t *testing.T) {
	if wowhead.ParseItem(1, []byte("not json")) != nil {
		t.Error("not json parsed")
	}
	if wowhead.ParseItem(1, []byte(`{"error":"ID is out of range"}`)) != nil {
		t.Error("error payload parsed")
	}
	item := wowhead.ParseItem(1, []byte(`{"name":"X","icon":"../evil","quality":"3"}`))
	if item == nil || item.IconURL != "" || item.Quality != nil {
		t.Errorf("item %+v", item)
	}
	if item := wowhead.ParseItem(1, []byte(`{"name":"X","quality":2.5}`)); item.Quality != nil {
		t.Error("fractional quality kept")
	}
}

func TestFetch(t *testing.T) {
	payload := fixture(t)
	var path, query, ua string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, query, ua = r.URL.Path, r.URL.RawQuery, r.Header.Get("User-Agent")
		if path != "/forever/tooltip/item/6314" {
			w.WriteHeader(404)
			return
		}
		w.Write(payload)
	}))
	item := wowhead.Fetch(srv.Client(), srv.URL, "1.2.3", 6314)
	if item == nil || item.Name != "Wolfmaster Cape" {
		t.Fatalf("item %+v", item)
	}
	if query != "locale=0" || ua != "DOINK-companion/1.2.3 (+https://github.com/ryan-flan/DOINK)" {
		t.Errorf("query %q ua %q", query, ua)
	}
	if wowhead.Fetch(srv.Client(), srv.URL, "1", 999999) != nil {
		t.Error("404 gave an item")
	}
	srv.Close()
	if wowhead.Fetch(nil, srv.URL, "1", 6314) != nil {
		t.Error("connection refused gave an item")
	}
}

func TestCache(t *testing.T) {
	var calls []int
	cache := wowhead.NewCache(func(id int) *wowhead.Item {
		calls = append(calls, id)
		if id == 1 {
			return &wowhead.Item{ID: 1, Name: "X"}
		}
		return nil
	})
	if cache.Lookup(1).Name != "X" || cache.Lookup(2) != nil {
		t.Error("first lookups")
	}
	cache.Lookup(1)
	cache.Lookup(2)
	if cache.Lookup(0) != nil || cache.Lookup(-5) != nil {
		t.Error("non-positive ids")
	}
	if !reflect.DeepEqual(calls, []int{1, 2}) {
		t.Errorf("calls %v: one request per item, misses included", calls)
	}
}

func lootEvent(d map[string]any) savedvars.Event {
	return savedvars.Event{Char: "Flano", Realm: "R", Class: "HUNTER", Seq: 1, Type: "loot", Data: d}
}

func TestLootEmbedEnrichment(t *testing.T) {
	item := wowhead.ParseItem(6314, fixture(t))
	ev := lootEvent(map[string]any{"item_id": 6314.0, "name": "Wolfmaster Cape", "quality": 3.0,
		"qty": 1.0, "vendor_value": 1215.0})
	e := discord.BuildEmbed(ev, item)
	if e["title"] != "Flano looted Wolfmaster Cape" || e["url"] != "https://www.wowhead.com/forever/item=6314" {
		t.Errorf("title %q url %q", e["title"], e["url"])
	}
	if !reflect.DeepEqual(e["thumbnail"], map[string]any{"url": item.IconURL}) {
		t.Errorf("thumbnail %v", e["thumbnail"])
	}
	if d, _ := e["description"].(string); !strings.HasPrefix(d, "Item Level 27\nBinds when picked up\nBack\n") {
		t.Errorf("description %q", d)
	}
	if fs := e["fields"].([]map[string]any); fs[0]["value"] != "12s 15c" {
		t.Errorf("fields %v", fs)
	}
	if e["color"] != 0x0070DD {
		t.Errorf("color %x", e["color"])
	}
	plain := discord.BuildEmbed(ev, nil)
	if _, ok := plain["thumbnail"]; ok {
		t.Error("plain has a thumbnail")
	}
	if _, ok := plain["description"]; ok {
		t.Error("plain has a description")
	}
}

func TestItemFillsGapsInTheEvent(t *testing.T) {
	three := 3
	item := &wowhead.Item{ID: 6314, Name: "Wolfmaster Cape", Quality: &three, IconURL: "https://x/icon.jpg", Lines: []string{"Back"}}
	e := discord.BuildEmbed(lootEvent(map[string]any{"item_id": 6314.0, "name": nil, "quality": nil}), item)
	if e["title"] != "Flano looted Wolfmaster Cape" || e["color"] != 0x0070DD {
		t.Errorf("title %q color %x", e["title"], e["color"])
	}
}
