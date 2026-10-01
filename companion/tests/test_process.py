import json
import tempfile
import threading
import unittest
from pathlib import Path

from doink.config import Config
from doink.discord import WebhookError, build_embed
from doink.runner import RealtimeWorker, post_events, process
from doink.state import State
from doink.status import Status


CONFIG_HOOK = "https://discord.com/api/webhooks/1/config"
GAME_HOOK = "https://discord.com/api/webhooks/2/account"
CHAR_HOOK = "https://discord.com/api/webhooks/3/alt"


class FakeWebhook:
    """Stands in for both WebhookPool and Webhook; records which URL got what."""

    def __init__(self):
        self.sent = []      # one list of embeds per message
        self.urls = []      # URL each message went to
        self.fail = False
        self._url = None

    def get(self, url):
        self._url = url
        return self

    def send(self, embeds):
        if self.fail:
            raise WebhookError("boom")
        self.sent.append(embeds)
        self.urls.append(self._url)


def webhooks_block(**hooks):
    entries = "".join(f'["{k}"] = "{v}",\n' for k, v in hooks.items())
    return '["webhooks"] = {\n' + entries + "},\n"


def events_file(*seqs, char="Paul", event_type="level_up", surname=None):
    extra = r'\"surname\":\"%s\",' % surname if surname else ""
    lines = [
        r'"{%s\"char\":\"%s\",\"class\":\"MAGE\",\"data\":{\"level\":%d},\"realm\":\"R\",'
        r'\"seq\":%d,\"ts\":1790870764,\"type\":\"%s\"}",' % (extra, char, s, s, event_type)
        for s in seqs
    ]
    return '["events"] = {\n' + "\n".join(lines) + "\n},"


class ProcessTest(unittest.TestCase):
    def setUp(self):
        tmp = Path(tempfile.mkdtemp())
        self.sv = tmp / "DOINK.lua"
        self.config = Config(savedvariables_paths=[self.sv], webhook_url=CONFIG_HOOK,
                             state_path=tmp / "state.json", max_backlog=10)
        self.webhook = FakeWebhook()

    def run_with(self, text, state=None):
        self.sv.write_text(text, encoding="utf-8")
        state = state or State(self.config.state_path)
        return process(self.config, self.sv, state, self.webhook), state

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

    def test_webhook_precedence_char_then_account_then_config(self):
        text = (webhooks_block(**{"*": GAME_HOOK, "Alt-R": CHAR_HOOK})
                + events_file(1) + events_file(1, char="Alt") + events_file(1, char="Bob"))
        self.run_with(text)
        self.assertEqual(sorted(self.webhook.urls), sorted([GAME_HOOK, CHAR_HOOK, GAME_HOOK]))

        self.webhook.urls.clear()
        self.run_with(events_file(1, 2))  # no in-game webhooks at all
        self.assertEqual(self.webhook.urls, [CONFIG_HOOK])

    def test_no_webhook_anywhere_keeps_events_for_later(self):
        self.config.webhook_url = ""
        with self.assertLogs("doink", "ERROR"):
            ok, state = self.run_with(events_file(1, 2))
        self.assertFalse(ok)
        self.assertEqual(self.webhook.sent, [])
        self.assertIsNone(state.last_seen("Paul-R"))

        # Set in game -> next reload posts them.
        ok, _ = self.run_with(webhooks_block(**{"*": GAME_HOOK}) + events_file(1, 2))
        self.assertTrue(ok)
        self.assertEqual(len(self.webhook.sent[0]), 2)

    def test_bad_game_webhook_is_refused(self):
        with self.assertLogs("doink", "ERROR"):
            ok, _ = self.run_with(webhooks_block(**{"*": "https://evil.example/x"}) + events_file(1))
        self.assertFalse(ok)
        self.assertEqual(self.webhook.sent, [])

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

    def test_surname_upgrade_carries_history_without_reposting(self):
        # v0.2.0: posted #1-2 as "Paul-R".
        state = State(self.config.state_path)
        self.run_with(events_file(1, 2), state)
        self.webhook.sent.clear()

        # v0.3.0 addon migrated the entry and stamped the surname into it;
        # one new event since. Only #3 may post.
        restarted = State(self.config.state_path)
        with self.assertLogs("doink", "INFO") as logs:
            ok, _ = self.run_with(events_file(1, 2, 3, surname="Hebbs"), restarted)
        self.assertTrue(ok)
        self.assertEqual([e["title"] for m in self.webhook.sent for e in m],
                         ["Paul Hebbs reached level 3"])
        self.assertEqual(restarted.last_seen("Paul Hebbs-R"), 3)
        self.assertIsNone(restarted.last_seen("Paul-R"))
        self.assertTrue(any("carried over" in line for line in logs.output))

    def test_seq_reset_starts_over(self):
        state = State(self.config.state_path)
        self.run_with(events_file(40, 41), state)
        self.webhook.sent.clear()
        self.run_with(events_file(1), state)  # SavedVariables wiped
        self.assertEqual(self.webhook.sent[0][0]["title"], "Paul reached level 1")
        self.assertEqual(state.last_seen("Paul-R"), 1)

    def event(self, seq, char="Paul"):
        return {"char": char, "class": "MAGE", "data": {"level": seq}, "realm": "R",
                "seq": seq, "ts": 1790870764, "type": "level_up"}

    def test_realtime_post_then_file_fills_the_gaps_only(self):
        state = State(self.config.state_path)
        status = Status()
        # The reader catches #3 of a character it has never seen.
        ok = post_events(self.config, "Paul-R", [self.event(3)], state, self.webhook, status,
                         {}, first_sight_backlog=10, realtime=True)
        self.assertTrue(ok)
        self.assertEqual(status.recent()[0][3], True, "marked as a realtime post")
        self.assertEqual(state.missing("Paul-R"), {1, 2})

        # The file arrives later with #1-4: only 1, 2 and 4 may post.
        self.webhook.sent.clear()
        ok, _ = self.run_with(events_file(1, 2, 3, 4), state)
        self.assertTrue(ok)
        self.assertEqual([e["title"] for m in self.webhook.sent for e in m],
                         ["Paul reached level 1", "Paul reached level 2", "Paul reached level 4"])
        self.assertEqual(state.missing("Paul-R"), set())
        self.assertEqual(state.last_seen("Paul-R"), 4)

    def test_realtime_worker_dedupes_and_skips_hello(self):
        state = State(self.config.state_path)
        self.run_with(events_file(1, 2), state)
        self.webhook.sent.clear()
        status = Status()
        worker = RealtimeWorker(self.config, state, self.webhook, status, threading.Event(),
                                game_hooks=lambda: {})
        worker.handle_message(json.dumps(self.event(2)).encode())  # already posted from the file
        self.assertEqual(self.webhook.sent, [])
        worker.handle_message(b'{"type":"hello","char":"Paul","realm":"R","addon":"0.6.0","test":true}')
        self.assertEqual(self.webhook.sent, [])
        self.assertEqual(status.realtime()["hello"]["who"], "Paul")
        self.assertTrue(status.realtime()["hello"]["test"])
        worker.handle_message(json.dumps(self.event(3)).encode())
        self.assertEqual(self.webhook.sent[0][0]["title"], "Paul reached level 3")
        worker.handle_message(json.dumps(self.event(3)).encode())  # the strip repeats it
        self.assertEqual(len(self.webhook.sent), 1)
        self.assertEqual(state.last_seen("Paul-R"), 3)


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
        embed = build_embed({"char": "Paul", "realm": "R", "seq": 1, "type": "achievement",
                             "data": {"name": "Explore Elwynn Forest"}})
        self.assertEqual(embed["title"], "Paul: achievement")
        self.assertIn("Explore Elwynn Forest", embed["description"])


if __name__ == "__main__":
    unittest.main()
