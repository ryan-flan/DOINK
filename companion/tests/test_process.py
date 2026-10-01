import tempfile
import unittest
from pathlib import Path

from doink.config import Config
from doink.discord import WebhookError, build_embed
from doink.state import State
from main import process


class FakeWebhook:
    def __init__(self):
        self.sent = []      # one list of embeds per message
        self.fail = False

    def send(self, embeds):
        if self.fail:
            raise WebhookError("boom")
        self.sent.append(embeds)


def events_file(*seqs, char="Paul", event_type="level_up"):
    lines = [
        r'"{\"char\":\"%s\",\"class\":\"MAGE\",\"data\":{\"level\":%d},\"realm\":\"R\",'
        r'\"seq\":%d,\"ts\":1790870764,\"type\":\"%s\"}",' % (char, s, s, event_type)
        for s in seqs
    ]
    return '["events"] = {\n' + "\n".join(lines) + "\n},"


class ProcessTest(unittest.TestCase):
    def setUp(self):
        tmp = Path(tempfile.mkdtemp())
        self.sv = tmp / "DOINK.lua"
        self.config = Config(savedvariables_path=self.sv, webhook_url="",
                             state_path=tmp / "state.json", max_backlog=10)
        self.webhook = FakeWebhook()

    def run_with(self, text, state=None):
        self.sv.write_text(text, encoding="utf-8")
        state = state or State(self.config.state_path)
        return process(self.config, state, self.webhook), state

    def test_posts_only_new_and_survives_restart(self):
        self.run_with(events_file(1, 2))
        self.assertEqual(sum(len(m) for m in self.webhook.sent), 2)

        # Restart: fresh State loaded from disk, same file + one new event.
        self.webhook.sent.clear()
        ok, state = self.run_with(events_file(1, 2, 3))
        self.assertTrue(ok)
        self.assertEqual(len(self.webhook.sent), 1)
        self.assertEqual(self.webhook.sent[0][0]["title"], "Paul reached level 3")
        self.assertEqual(state.last_seen("Paul-R"), 3)

    def test_batches_ten_per_message(self):
        self.config.max_backlog = 100
        self.run_with(events_file(*range(1, 24)))
        self.assertEqual([len(m) for m in self.webhook.sent], [10, 10, 3])

    def test_first_sight_backlog_cap(self):
        self.config.max_backlog = 2
        _, state = self.run_with(events_file(*range(1, 50)))
        self.assertEqual([e["title"] for e in self.webhook.sent[0]],
                         ["Paul reached level 48", "Paul reached level 49"])
        self.assertEqual(state.last_seen("Paul-R"), 49)

    def test_backlog_zero_just_baselines(self):
        self.config.max_backlog = 0
        _, state = self.run_with(events_file(1, 2, 3))
        self.assertEqual(self.webhook.sent, [])
        self.assertEqual(state.last_seen("Paul-R"), 3)

    def test_disabled_type_is_skipped_but_advances(self):
        self.config.notifiers = {"level_up": False}
        _, state = self.run_with(events_file(1, 2))
        self.assertEqual(self.webhook.sent, [])
        self.assertEqual(state.last_seen("Paul-R"), 2)

    def test_failure_keeps_state_for_retry(self):
        state = State(self.config.state_path)
        self.run_with(events_file(1), state)
        self.webhook.fail = True
        ok, _ = self.run_with(events_file(1, 2), state)
        self.assertFalse(ok)
        self.assertEqual(state.last_seen("Paul-R"), 1)

        self.webhook.fail = False
        ok, _ = self.run_with(events_file(1, 2), state)
        self.assertTrue(ok)
        self.assertEqual(self.webhook.sent[-1][0]["title"], "Paul reached level 2")

    def test_seq_reset_starts_over(self):
        state = State(self.config.state_path)
        self.run_with(events_file(40, 41), state)
        self.webhook.sent.clear()
        self.run_with(events_file(1), state)  # SavedVariables wiped
        self.assertEqual(self.webhook.sent[0][0]["title"], "Paul reached level 1")
        self.assertEqual(state.last_seen("Paul-R"), 1)


class EmbedTest(unittest.TestCase):
    def test_level_up_embed(self):
        embed = build_embed({"char": "Paul", "realm": "Classic Beta PvE 2", "class": "WARRIOR",
                             "seq": 1, "ts": 1790870764, "type": "level_up",
                             "data": {"level": 9}, "test": True})
        self.assertEqual(embed["title"], "[TEST] Paul reached level 9")
        self.assertEqual(embed["color"], 0xC79C6E)
        self.assertEqual(embed["footer"]["text"], "Paul-Classic Beta PvE 2 · test event")
        self.assertTrue(embed["timestamp"].endswith("+00:00"))

    def test_unknown_type_falls_back(self):
        embed = build_embed({"char": "Paul", "realm": "R", "seq": 1, "type": "loot",
                             "data": {"name": "Carving Knife"}})
        self.assertEqual(embed["title"], "Paul: loot")
        self.assertIn("Carving Knife", embed["description"])


if __name__ == "__main__":
    unittest.main()
