"""The companion as the settings window sees it: owns the config and the
watcher thread, and applies changes without a restart."""

import logging
import threading
from pathlib import Path

from . import autostart
from .config import Config, load_config, native_path, save_settings
from .discord import Webhook, WebhookError, connection_test_embed, is_webhook_url
from .discover import discover_in_wow_dir
from .parser import parse_webhooks
from .runner import run
from .status import Status

log = logging.getLogger("doink")


class SettingsError(ValueError):
    """A setting was rejected; the message is shown to the user as-is."""


class App:
    def __init__(self, config_path: Path, status: Status, dry_run: bool = False):
        self.config_path = config_path
        self.status = status
        self.dry_run = dry_run
        self.config: Config = load_config(config_path, dry_run, require_paths=False)
        self._stop: threading.Event | None = None
        self._worker: threading.Thread | None = None

    # ---------------------------------------------------------- watcher

    def start(self) -> None:
        if not self.config.savedvariables_paths:
            self.status.watching(0)
            self.status.failed("World of Warcraft not found. Open settings to choose its folder")
            return
        self._stop = threading.Event()
        self._worker = threading.Thread(target=self._work, args=(self.config, self._stop),
                                        name="watcher", daemon=True)
        self._worker.start()

    def stop(self) -> None:
        if self._stop:
            self._stop.set()
            self._worker.join(timeout=5)
            self._stop = self._worker = None

    def restart(self) -> None:
        self.stop()
        self.config = load_config(self.config_path, self.dry_run, require_paths=False)
        self.start()

    def _work(self, config: Config, stop: threading.Event) -> None:
        try:
            run(config, stop=stop, status=self.status)
        except Exception:
            log.exception("watcher crashed")
            self.status.failed("stopped after an error; see the log")

    # ---------------------------------------------------------- settings

    @property
    def watched(self) -> list[Path]:
        return list(self.config.savedvariables_paths)

    def game_webhooks(self) -> dict[str, str]:
        """Webhooks set in game (/doink webhook), from every watched file."""
        hooks = {}
        for path in self.config.savedvariables_paths:
            try:
                hooks.update(parse_webhooks(path.read_bytes().decode("latin-1")))
            except OSError:
                pass
        return hooks

    def needs_setup(self) -> bool:
        """First run: nothing to watch, or nowhere to post."""
        return not self.watched or not (self.config.webhook_url or self.game_webhooks())

    def set_webhook(self, url: str) -> None:
        url = url.strip()
        if url and not is_webhook_url(url):
            raise SettingsError("That isn't a Discord webhook URL. It should start with "
                                "https://discord.com/api/webhooks/")
        save_settings(self.config_path, {"webhook_url": url})
        self.config.webhook_url = url  # the watcher reads it on every post
        log.info("webhook %s in settings", "saved" if url else "cleared")
        if url:
            self.status.ok()

    def send_test(self, url: str) -> None:
        url = url.strip()
        if not is_webhook_url(url):
            raise SettingsError("Paste a Discord webhook URL first.")
        try:
            Webhook(url).send([connection_test_embed()])
        except WebhookError as e:
            raise SettingsError(f"Discord didn't accept it: {e}") from None

    def set_wow_dir(self, folder: str) -> int:
        """Use this WoW install folder. Returns how many files will be watched."""
        found = discover_in_wow_dir(native_path(folder))
        if not found:
            raise SettingsError(
                "No DOINK addon found there. Pick the folder that contains "
                "_classic_beta_ (or similar), and make sure Interface\\AddOns\\DOINK "
                "is installed in it.")
        save_settings(self.config_path, {"wow_dir": folder, "savedvariables_path": None})
        self.restart()
        return len(found)

    # ---------------------------------------------------------- autostart

    @staticmethod
    def autostart_available() -> bool:
        return autostart.available()

    @staticmethod
    def autostart_enabled() -> bool:
        return autostart.available() and autostart.is_enabled()

    @staticmethod
    def set_autostart(on: bool) -> None:
        autostart.set_enabled(on)
        log.info("start with Windows: %s", "on" if on else "off")
