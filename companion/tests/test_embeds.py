import unittest

from doink.discord import build_embed, format_money


def event(event_type, data, **extra):
    return {"char": "Paul", "realm": "R", "class": "WARRIOR", "seq": 1,
            "ts": 1790870764, "type": event_type, "data": data, **extra}


class EmbedTest(unittest.TestCase):
    def test_loot(self):
        embed = build_embed(event("loot", {
            "item_id": 19019, "name": "Thunderfury, Blessed Blade of the Windseeker",
            "link": "|cnIQ5:|Hitem:19019::::::::|h[Thunderfury]|h|r",
            "quality": 5, "qty": 1, "vendor_value": 255355}))
        self.assertEqual(embed["title"], "Paul looted Thunderfury, Blessed Blade of the Windseeker")
        self.assertEqual(embed["url"], "https://www.wowhead.com/forever/item=19019")
        self.assertEqual(embed["color"], 0xFF8000)  # legendary, not class colour
        self.assertEqual(embed["fields"][0]["value"], "25g 53s 55c")

    def test_loot_stack_without_vendor_value(self):
        embed = build_embed(event("loot", {"item_id": 2589, "name": "Linen Cloth",
                                           "quality": 1, "qty": 3, "vendor_value": 0}))
        self.assertEqual(embed["title"], "Paul looted Linen Cloth ×3")
        self.assertNotIn("fields", embed)

    def test_death(self):
        embed = build_embed(event("death", {"zone": "Westfall", "subzone": "Moonbrook",
                                            "killer": "Defias Pillager"}))
        self.assertEqual(embed["title"], "Paul died to Defias Pillager")
        self.assertEqual(embed["description"], "in Moonbrook, Westfall")
        self.assertEqual(embed["color"], 0xC79C6E)

    def test_death_unknown_killer_no_subzone(self):
        embed = build_embed(event("death", {"zone": "Westfall", "subzone": None, "killer": None}))
        self.assertEqual(embed["title"], "Paul died")
        self.assertEqual(embed["description"], "in Westfall")

    def test_quest(self):
        embed = build_embed(event("quest", {"quest_id": 783, "title": "A Threat Within", "xp": 1200}))
        self.assertEqual(embed["title"], "Paul completed A Threat Within")
        self.assertEqual(embed["url"], "https://www.wowhead.com/forever/quest=783")
        self.assertEqual(embed["fields"][0]["value"], "1,200")

    def test_boss_kill(self):
        embed = build_embed(event("boss_kill", {"encounter_id": 672, "name": "Ragnaros",
                                                "instance": "Molten Core", "difficulty": 9,
                                                "success": True, "group_size": 40}))
        self.assertEqual(embed["title"], "Paul defeated Ragnaros")
        self.assertEqual([f["value"] for f in embed["fields"]], ["Molten Core", "40"])

    def test_skill_up(self):
        embed = build_embed(event("skill_up", {"skill": "Blacksmithing", "rank": 150,
                                               "max_rank": 225}))
        self.assertEqual(embed["title"], "Paul reached 150 Blacksmithing")
        self.assertEqual(embed["description"], "150 / 225")

    def test_surname_in_title_and_footer(self):
        embed = build_embed(event("level_up", {"level": 10}, surname="Hebbs"))
        self.assertEqual(embed["title"], "Paul Hebbs reached level 10")
        self.assertEqual(embed["footer"]["text"], "Paul Hebbs-R")

    def test_test_events_are_labelled(self):
        embed = build_embed(event("quest", {"quest_id": 783, "title": "A Threat Within"}, test=True))
        self.assertEqual(embed["title"], "[TEST] Paul completed A Threat Within")

    def test_format_money(self):
        self.assertEqual(format_money(0), "0c")
        self.assertEqual(format_money(5), "5c")
        self.assertEqual(format_money(10000), "1g")
        self.assertEqual(format_money(10203), "1g 2s 3c")
        self.assertEqual(format_money(150), "1s 50c")


if __name__ == "__main__":
    unittest.main()
