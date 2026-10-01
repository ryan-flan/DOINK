import json
import tempfile
import unittest
from pathlib import Path

from doink.state import MISSING_MAX, State


class StateTest(unittest.TestCase):
    def setUp(self):
        self.path = Path(tempfile.mkdtemp()) / "state.json"

    def test_unknown_character_is_pending(self):
        s = State(self.path)
        self.assertTrue(s.is_pending("Paul-R", 1))
        self.assertIsNone(s.last_seen("Paul-R"))

    def test_gaps_are_tracked_and_filled(self):
        s = State(self.path)
        s.mark_posted("Paul-R", 5)
        s.mark_posted("Paul-R", 8)
        self.assertEqual(s.last_seen("Paul-R"), 8)
        self.assertEqual(s.missing("Paul-R"), {6, 7})
        self.assertTrue(s.is_pending("Paul-R", 7))
        self.assertFalse(s.is_pending("Paul-R", 5))
        self.assertFalse(s.is_pending("Paul-R", 8))
        self.assertTrue(s.is_pending("Paul-R", 9))
        s.mark_posted("Paul-R", 6)
        self.assertEqual(s.missing("Paul-R"), {7})
        self.assertEqual(s.last_seen("Paul-R"), 8)

    def test_first_sight_backlog_becomes_gaps(self):
        s = State(self.path)
        s.mark_posted("Paul-R", 50, first_sight_backlog=10)
        self.assertEqual(s.missing("Paul-R"), set(range(41, 50)))
        s = State(self.path)
        s.mark_posted("New-R", 3, first_sight_backlog=10)
        self.assertEqual(s.missing("New-R"), {1, 2}, "never below seq 1")

    def test_baseline_and_prune(self):
        s = State(self.path)
        s.mark_posted("Paul-R", 2)
        s.mark_posted("Paul-R", 9)  # gaps 3..8
        s.prune_missing("Paul-R", present={4, 5, 9}, newest=6)
        self.assertEqual(s.missing("Paul-R"), {4, 5, 7, 8}, "absent ones at or below newest go")
        s.baseline("Paul-R", 4)
        self.assertEqual(s.last_seen("Paul-R"), 9, "baseline never lowers last_seen")
        self.assertEqual(s.missing("Paul-R"), set())

    def test_missing_is_capped(self):
        s = State(self.path)
        s.mark_posted("Paul-R", 1)
        s.mark_posted("Paul-R", 2000)
        self.assertEqual(len(s.missing("Paul-R")), MISSING_MAX)
        self.assertIn(1999, s.missing("Paul-R"))
        self.assertNotIn(2, s.missing("Paul-R"))

    def test_persists_and_reads_pre_060_format(self):
        self.path.write_text(json.dumps({"Paul-R": {"last_seen": 10}}), encoding="utf-8")
        s = State(self.path)
        self.assertEqual(s.missing("Paul-R"), set())
        s.mark_posted("Paul-R", 12)
        again = State(self.path)
        self.assertEqual(again.last_seen("Paul-R"), 12)
        self.assertEqual(again.missing("Paul-R"), {11})
        again.forget("Paul-R")
        self.assertIsNone(State(self.path).last_seen("Paul-R"))


if __name__ == "__main__":
    unittest.main()
