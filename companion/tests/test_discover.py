import tempfile
import unittest
from pathlib import Path

from doink.discover import discover_savedvariables
from doink.parser import parse_webhooks


class DiscoverTest(unittest.TestCase):
    def test_finds_accounts_in_flavors_with_the_addon(self):
        root = Path(tempfile.mkdtemp())
        wow = root / "Program Files (x86)" / "World of Warcraft"
        for flavor, has_addon in (("_classic_beta_", True), ("_retail_", False)):
            (wow / flavor / "WTF" / "Account" / "123#1" / "SavedVariables").mkdir(parents=True)
            if has_addon:
                (wow / flavor / "Interface" / "AddOns" / "DOINK").mkdir(parents=True)
        (wow / "_classic_beta_" / "WTF" / "Account" / "456#2" / "SavedVariables").mkdir(parents=True)

        found = discover_savedvariables([root])
        self.assertEqual(found, [
            wow / "_classic_beta_" / "WTF" / "Account" / "123#1" / "SavedVariables" / "DOINK.lua",
            wow / "_classic_beta_" / "WTF" / "Account" / "456#2" / "SavedVariables" / "DOINK.lua",
        ])

    def test_nothing_installed(self):
        self.assertEqual(discover_savedvariables([Path(tempfile.mkdtemp())]), [])


class ParseWebhooksTest(unittest.TestCase):
    def test_flat_table_as_wow_writes_it(self):
        text = '''DOINKDB = {
["webhooks"] = {
["*"] = "https://discord.com/api/webhooks/1/a",
["Pàul-Classic Beta PvE 2"] = "https://discord.com/api/webhooks/2/b", -- [1]
},
["chars"] = {
},
}'''.encode("utf-8").decode("latin-1")
        self.assertEqual(parse_webhooks(text), {
            "*": "https://discord.com/api/webhooks/1/a",
            "Pàul-Classic Beta PvE 2": "https://discord.com/api/webhooks/2/b",
        })

    def test_missing_or_empty(self):
        self.assertEqual(parse_webhooks("DOINKDB = {}"), {})
        self.assertEqual(parse_webhooks('["webhooks"] = {\n},'), {})

    def test_malformed_keeps_what_parsed(self):
        text = '["webhooks"] = {\n["*"] = "u1",\n["x"] = 42,\n}'
        with self.assertLogs("doink.parser", "WARNING"):
            self.assertEqual(parse_webhooks(text), {"*": "u1"})


if __name__ == "__main__":
    unittest.main()
