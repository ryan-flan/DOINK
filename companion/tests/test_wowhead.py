import io
import json
import unittest
import urllib.error
from pathlib import Path

from doink import wowhead
from doink.discord import build_embed
from doink.wowhead import ItemCache, ItemInfo, fetch_item, parse_item, tooltip_lines

FIXTURE = Path(__file__).with_name("fixtures") / "wowhead_item_6314.json"


class FakeResponse(io.BytesIO):
    def __enter__(self):
        return self

    def __exit__(self, *exc):
        self.close()


class TooltipTest(unittest.TestCase):
    def test_wolfmaster_cape(self):
        info = parse_item(6314, FIXTURE.read_bytes())
        self.assertEqual(info.name, "Wolfmaster Cape")
        self.assertEqual(info.quality, 3)
        self.assertEqual(info.icon_url, "https://wow.zamimg.com/images/wow/icons/large/inv_misc_cape_10.jpg")
        self.assertEqual(info.lines, [
            "Item Level 27", "Binds when picked up", "Back", "22 Armor", "+3 Stamina",
            "Requires Level 22", "Equip: +10 Attack Power.",
            "Equip: +9 Attack Power against Beasts.",
        ], "name, sell price and HTML comments stripped; order kept")

    def test_money_and_stack_lines_are_dropped(self):
        html = ('<table><tr><td><b class="q1">Chunk of Boar Meat</b><span class="q whtt-extra">'
                '<br>Item Level 5</span></td></tr></table><table><tr><td><span class="toycolor">'
                'Crafting Reagent</span><br><div class="whtt-maxstack">Max Stack: 20</div>'
                '<div class="whtt-sellprice">Sell Price: <span class="moneycopper">4</span></div></td></tr></table>')
        self.assertEqual(tooltip_lines(html), ["Item Level 5", "Crafting Reagent"])

    def test_entities_and_line_cap(self):
        html = "<b>X</b><br>Use: &quot;Zap&quot; &amp; run" + "<br>line" * 30
        lines = tooltip_lines(html)
        self.assertEqual(lines[0], 'Use: "Zap" & run')
        self.assertLessEqual(len(lines), wowhead.MAX_LINES)

    def test_bad_payloads(self):
        self.assertIsNone(parse_item(1, b"not json"))
        self.assertIsNone(parse_item(1, b'{"error":"ID is out of range"}'))
        info = parse_item(1, json.dumps({"name": "X", "icon": "../evil", "quality": "3"}).encode())
        self.assertIsNone(info.icon_url, "icon names are validated before building a URL")
        self.assertIsNone(info.quality)


class FetchTest(unittest.TestCase):
    def test_success_404_and_network_error(self):
        seen = []

        def opener(request, timeout):
            seen.append((request.full_url, request.get_header("User-agent"), timeout))
            return FakeResponse(FIXTURE.read_bytes())

        info = fetch_item(6314, opener=opener)
        self.assertEqual(info.name, "Wolfmaster Cape")
        self.assertEqual(seen[0][0], "https://nether.wowhead.com/forever/tooltip/item/6314?locale=0")
        self.assertIn("DOINK-companion", seen[0][1])
        self.assertEqual(seen[0][2], wowhead.TIMEOUT)

        def missing(request, timeout):
            raise urllib.error.HTTPError(request.full_url, 404, "Not Found", {}, None)

        self.assertIsNone(fetch_item(99999999, opener=missing))

        def down(request, timeout):
            raise urllib.error.URLError("no route")

        self.assertIsNone(fetch_item(6314, opener=down))

    def test_cache_remembers_hits_and_misses(self):
        calls = []

        def fetch(item_id):
            calls.append(item_id)
            return ItemInfo(item_id, "X", 2, None) if item_id == 1 else None

        cache = ItemCache(fetch=fetch)
        self.assertEqual(cache.lookup(1).name, "X")
        self.assertIsNone(cache.lookup(2))
        cache.lookup(1)
        cache.lookup(2)
        self.assertEqual(calls, [1, 2], "one request per item, misses included")
        self.assertIsNone(cache.lookup(None))
        self.assertIsNone(cache.lookup("6314"))
        self.assertEqual(calls, [1, 2])


class LootEmbedTest(unittest.TestCase):
    def test_item_details_enrich_the_embed(self):
        info = parse_item(6314, FIXTURE.read_bytes())
        event = {"char": "Flano", "realm": "R", "class": "HUNTER", "seq": 1, "type": "loot",
                 "data": {"item_id": 6314, "name": "Wolfmaster Cape", "quality": 3, "qty": 1,
                          "vendor_value": 1215}}
        embed = build_embed(event, info)
        self.assertEqual(embed["title"], "Flano looted Wolfmaster Cape")
        self.assertEqual(embed["url"], "https://www.wowhead.com/forever/item=6314")
        self.assertEqual(embed["thumbnail"]["url"], info.icon_url)
        self.assertTrue(embed["description"].startswith("Item Level 27\nBinds when picked up\nBack\n"))
        self.assertEqual(embed["fields"][0]["value"], "12s 15c")
        self.assertEqual(embed["color"], 0x0070DD)

        plain = build_embed(event, None)
        self.assertNotIn("thumbnail", plain)
        self.assertNotIn("description", plain)

    def test_item_fills_gaps_in_the_event(self):
        info = ItemInfo(6314, "Wolfmaster Cape", 3, "https://x/icon.jpg", ["Back"])
        event = {"char": "Flano", "realm": "R", "seq": 1, "type": "loot",
                 "data": {"item_id": 6314, "name": None, "quality": None}}
        embed = build_embed(event, info)
        self.assertEqual(embed["title"], "Flano looted Wolfmaster Cape")
        self.assertEqual(embed["color"], 0x0070DD, "quality from Wowhead when the game had none")


if __name__ == "__main__":
    unittest.main()
