import tempfile
import tomllib
import unittest
from pathlib import Path

from doink.app import App, SettingsError
from doink.config import load_config, save_settings
from doink.status import Status

HOOK = "https://discord.com/api/webhooks/123/abc-DEF_9"


def make_wow(root: Path, with_addon: bool = True) -> Path:
    """A fake WoW install with one account; returns the install folder."""
    wow = root / "World of Warcraft"
    sv = wow / "_classic_beta_" / "WTF" / "Account" / "1#1" / "SavedVariables"
    sv.mkdir(parents=True)
    if with_addon:
        (wow / "_classic_beta_" / "Interface" / "AddOns" / "DOINK").mkdir(parents=True)
    return wow


class SaveSettingsTest(unittest.TestCase):
    def setUp(self):
        self.path = Path(tempfile.mkdtemp()) / "config.toml"

    def test_round_trip_tricky_strings_and_types(self):
        save_settings(self.path, {
            "wow_dir": r'D:\Games\World of Warcraft',
            "webhook_url": HOOK,
            "dry_run": True,
            "max_backlog": 3,
            "poll_interval": 2.5,
            "odd": 'quote " and back\\slash',
        })
        raw = tomllib.loads(self.path.read_text(encoding="utf-8"))
        self.assertEqual(raw["wow_dir"], r'D:\Games\World of Warcraft')
        self.assertEqual(raw["odd"], 'quote " and back\\slash')
        self.assertEqual((raw["dry_run"], raw["max_backlog"], raw["poll_interval"]),
                         (True, 3, 2.5))

    def test_merges_removes_and_drops_obsolete_tables(self):
        self.path.write_text('webhook_url = "x"\nmax_backlog = 5\n[notifiers]\nloot = true\n',
                             encoding="utf-8")
        save_settings(self.path, {"webhook_url": "", "dry_run": False})
        raw = tomllib.loads(self.path.read_text(encoding="utf-8"))
        self.assertEqual(raw, {"max_backlog": 5, "dry_run": False})


class AppTest(unittest.TestCase):
    def setUp(self):
        self.dir = Path(tempfile.mkdtemp())
        self.wow = make_wow(self.dir)
        self.config_path = self.dir / "config.toml"
        save_settings(self.config_path, {"wow_dir": str(self.wow)})
        self.status = Status()
        self.app = App(self.config_path, self.status)

    def tearDown(self):
        self.app.stop()

    def test_set_webhook_saves_and_applies_live(self):
        self.app.set_webhook(f"  {HOOK}  ")
        self.assertEqual(self.app.config.webhook_url, HOOK)
        self.assertEqual(load_config(self.config_path).webhook_url, HOOK)
        self.app.set_webhook("")
        self.assertEqual(self.app.config.webhook_url, "")
        self.assertNotIn("webhook_url", tomllib.loads(self.config_path.read_text("utf-8")))

    def test_set_webhook_rejects_non_discord_urls(self):
        with self.assertRaises(SettingsError):
            self.app.set_webhook("https://example.com/api/webhooks/1/x")
        self.assertEqual(self.app.config.webhook_url, "")

    def test_send_test_validates_before_any_network(self):
        with self.assertRaises(SettingsError):
            self.app.send_test("not a url")

    def test_needs_setup_until_a_webhook_exists_anywhere(self):
        self.assertTrue(self.app.needs_setup())
        sv = self.app.watched[0]
        sv.write_text('DOINKDB = {\n["webhooks"] = {\n["*"] = "%s",\n},\n}' % HOOK,
                      encoding="utf-8")
        self.assertEqual(self.app.game_webhooks(), {"*": HOOK})
        self.assertFalse(self.app.needs_setup())

    def test_set_wow_dir(self):
        other = make_wow(self.dir / "elsewhere")
        self.assertEqual(self.app.set_wow_dir(str(other)), 1)
        self.assertTrue(str(self.app.watched[0]).startswith(str(other)))
        self.assertEqual(load_config(self.config_path).savedvariables_paths, self.app.watched)

    def test_set_wow_dir_without_addon_is_refused(self):
        bare = make_wow(self.dir / "bare", with_addon=False)
        before = self.app.watched
        with self.assertRaises(SettingsError):
            self.app.set_wow_dir(str(bare))
        self.assertEqual(self.app.watched, before)

    def test_no_wow_found_reports_instead_of_exiting(self):
        save_settings(self.config_path, {"wow_dir": str(self.dir / "nowhere")})
        app = App(self.config_path, Status())
        self.assertEqual(app.watched, [])
        app.start()
        self.assertIn("World of Warcraft not found", app.status.summary())
        self.assertTrue(app.needs_setup())

    def test_start_and_restart_watcher(self):
        self.app.start()
        self.assertIn("watching 1 file", self.status.summary())
        self.app.restart()
        self.assertIn("watching 1 file", self.status.summary())


if __name__ == "__main__":
    unittest.main()
