import unittest

from doink.parser import char_key, parse_events

# As written by the Forever beta client (1.60.1.70124); character renamed.
REAL_FILE = r'''
DOINKDB = {
["version"] = 1,
["chars"] = {
["Flano-Classic Beta PvE 2"] = {
["config"] = {
},
["seq"] = 1,
["events"] = {
"{\"char\":\"Flano\",\"class\":\"WARRIOR\",\"data\":{\"level\":9},\"level\":8,\"realm\":\"Classic Beta PvE 2\",\"seq\":1,\"test\":true,\"ts\":1790870764,\"type\":\"level_up\"}",
},
},
},
}
'''


def event_line(seq, char="Flano", extra=""):
    return (r'"{\"char\":\"%s\",\"realm\":\"R\",\"seq\":%d,\"type\":\"level_up\"%s}",'
            % (char, seq, extra))


def wrap(*lines):
    return '["events"] = {\n' + "\n".join(lines) + "\n},"


class ParserTest(unittest.TestCase):
    def test_real_file(self):
        events = parse_events(REAL_FILE)
        self.assertEqual(len(events), 1)
        event = events[0]
        self.assertEqual(event["seq"], 1)
        self.assertEqual(event["data"], {"level": 9})
        self.assertIs(event["test"], True)
        self.assertEqual(char_key(event), "Flano-Classic Beta PvE 2")

    def test_multiple_chars(self):
        text = wrap(event_line(1), event_line(2)) + wrap(event_line(7, "Alt"))
        self.assertEqual([(e["char"], e["seq"]) for e in parse_events(text)],
                         [("Flano", 1), ("Flano", 2), ("Alt", 7)])

    def test_braces_and_escapes_inside_strings(self):
        # A "}" inside a JSON string must not end the block; \\ and \" nest.
        extra = r',\"data\":{\"name\":\"a } b \\\\ \\\"q\\\"\"}'
        events = parse_events(wrap(event_line(1, extra=extra), event_line(2)))
        self.assertEqual(events[0]["data"]["name"], 'a } b \\ "q"')
        self.assertEqual(len(events), 2)

    def test_item_link_pipes(self):
        link = r'|cff0070dd|Hitem:2140::::::::9:::::::|h[Carving Knife]|h|r'
        extra = r',\"data\":{\"link\":\"%s\"}' % link
        events = parse_events(wrap(event_line(1, extra=extra)))
        self.assertEqual(events[0]["data"]["link"], link)

    def test_utf8_names(self):
        raw = wrap(event_line(1, char="Pàul")).encode("utf-8").decode("latin-1")
        self.assertEqual(parse_events(raw)[0]["char"], "Pàul")

    def test_decimal_byte_escapes(self):
        # "à" is UTF-8 bytes 195 160; WoW may write them as \195\160.
        text = wrap(event_line(1, char=r"P\195\160ul"))
        self.assertEqual(parse_events(text)[0]["char"], "Pàul")

    def test_comments(self):
        text = wrap(event_line(1) + " -- [1]", event_line(2) + " -- [2]")
        self.assertEqual([e["seq"] for e in parse_events(text)], [1, 2])

    def test_bad_entries_are_skipped(self):
        text = wrap(
            event_line(1),
            '"not json",',
            r'"{\"seq\":2}",',                   # missing fields
            r'"{\"char\":\"P\",\"realm\":\"R\",\"seq\":\"3\",\"type\":\"x\"}",',  # string seq
            event_line(4),
        )
        with self.assertLogs("doink.parser", "WARNING") as logs:
            events = parse_events(text)
        self.assertEqual([e["seq"] for e in events], [1, 4])
        self.assertEqual(len(logs.records), 3)

    def test_empty_and_missing(self):
        self.assertEqual(parse_events('["events"] = {\n},'), [])
        self.assertEqual(parse_events("DOINKDB = nil"), [])


if __name__ == "__main__":
    unittest.main()
