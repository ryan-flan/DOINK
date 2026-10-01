import unittest

from doink.status import TOOLTIP_MAX, Status


class StatusTest(unittest.TestCase):
    def setUp(self):
        self.calls = []
        self.status = Status(on_change=self.calls.append)

    def test_summary_progression(self):
        self.status.watching(1)
        self.assertEqual(self.status.summary(), "DOINK: watching 1 file, nothing posted yet")
        self.status.posted("Paul Hebbs", "level_up, quest")
        self.assertRegex(self.status.summary(),
                         r"^DOINK: last post \d\d:\d\d, Paul Hebbs: level_up, quest$")
        self.status.failed("no webhook for Paul Hebbs")
        self.assertEqual(self.status.summary(), "DOINK: no webhook for Paul Hebbs")

    def test_new_errors_notify_once(self):
        self.status.failed("boom")
        self.status.failed("boom")  # retry of the same problem
        self.status.failed("different")
        self.assertEqual([c for c in self.calls if c], ["boom", "different"])

    def test_ok_and_posting_clear_errors(self):
        self.status.failed("boom")
        self.status.ok()
        self.assertNotIn("boom", self.status.summary())
        self.status.failed("boom")  # same text again is new after clearing
        self.assertEqual([c for c in self.calls if c], ["boom", "boom"])
        self.status.posted("Paul", "loot")
        self.assertNotIn("boom", self.status.summary())

    def test_tooltip_length_limit(self):
        self.status.failed("x" * 300)
        self.assertEqual(len(self.status.summary()), TOOLTIP_MAX)


if __name__ == "__main__":
    unittest.main()
