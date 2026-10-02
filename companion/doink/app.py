"""The companion as the settings window sees it: owns the config, the shared
posting state and the worker threads, and applies changes without a restart."""

import logging
import sys
import threading
from pathlib import Path

from . import autostart
from .config import Config, load_config, native_path, save_settings
from .discord import Webhook, WebhookError, WebhookPool, connection_test_embed, is_webhook_url
from .discover import discover_in_wow_dir
from .parser import parse_webhooks
from .runner import RealtimeWorker, run
from .state import State
from .status import Status
from .updates import RELEASES_URL, UpdateChecker, UpdateError, UpdateInfo, Updater

log = logging.getLogger("doink")


class SettingsError(ValueError):
    """A setting was rejected; the message is shown to the user as-is."""


class App:
    def __init__(self, config_path: Path, status: Status, dry_run: bool = False):
        self.config_path = config_path
        self.status = status
        self.dry_run = dry_run
        self.config: Config = load_config(config_path, dry_run, require_paths=False)
        # One State and one WebhookPool, shared by both workers, so the file
        # path and the realtime path can never double-post.
        self.state = State(self.config.state_path, persist=not self.config.dry_run)
        self.pool = WebhookPool(dry_run=self.config.dry_run)
        self._stop: threading.Event | None = None
        self._worker: threading.Thread | None = None
        self._rt_stop: threading.Event | None = None
        self._rt_worker: RealtimeWorker | None = None
        self._updates = UpdateChecker(self.status.update_checked)
        self._updates_started = False
        self.status.notify_updates = self.config.update_notify

    # ---------------------------------------------------------- workers

    def start(self) -> None:
        if self.config.update_check and not self._updates_started:
            self._updates.start()  # once per process; restart() must not spawn another
            self._updates_started = True
        if self.config.savedvariables_paths:
            self._stop = threading.Event()
            self._worker = threading.Thread(target=self._work, args=(self.config, self._stop),
                                            name="watcher", daemon=True)
            self._worker.start()
        else:
            self.status.watching(0)
            self.status.failed("World of Warcraft not found. Open settings to choose its folder")
        if self.config.realtime:
            self._start_realtime()

    def stop(self) -> None:
        self._stop_realtime()
        if self._stop:
            self._stop.set()
            self._worker.join(timeout=5)
            self._stop = self._worker = None

    def shutdown(self) -> None:
        """Process exit: everything, including the daily update check."""
        self.stop()
        self._updates.stop()

    # ---------------------------------------------------------- updates

    def check_updates(self) -> UpdateInfo:
        """A check right now, regardless of update_check. Raises
        SettingsError if GitHub couldn't be reached."""
        info = self._updates.check_now()
        if info is None:
            raise SettingsError("Couldn't reach GitHub to check. Try again later, or see "
                                + RELEASES_URL)
        return info

    @staticmethod
    def update_installable() -> bool:
        return sys.platform == "win32" and bool(getattr(sys, "frozen", False))

    def install_update(self, info: UpdateInfo, progress=lambda text: None) -> None:
        """Download, verify and stage ``info``, then launch the swap script.
        On return the caller must quit so the script can replace the files.
        Raises SettingsError with a message for the user."""
        if not self.update_installable():
            raise SettingsError("Installing updates only works for the Windows doink.exe build.")
        updater = Updater(self.config_path.parent)
        try:
            companion = updater.download(info, progress)
            progress("Restarting DOINK…")
            self.stop()  # workers down first: no half-written state.json during the swap
            updater.apply(companion)
        except UpdateError as e:
            raise SettingsError(str(e)) from None
        except OSError as e:
            raise SettingsError(f"Update failed: {e}") from None

    def restart(self) -> None:
        self.stop()
        self.config = load_config(self.config_path, self.dry_run, require_paths=False)
        self.start()

    def _work(self, config: Config, stop: threading.Event) -> None:
        try:
            run(config, stop=stop, status=self.status, state=self.state, pool=self.pool)
        except Exception:
            log.exception("watcher crashed")
            self.status.failed("stopped after an error; see the log")

    def _start_realtime(self) -> None:
        if self._rt_worker and self._rt_worker.is_alive():
            return
        self._rt_stop = threading.Event()
        self._rt_worker = RealtimeWorker(self.config, self.state, self.pool, self.status,
                                         self._rt_stop, self.game_webhooks)
        self._rt_worker.start()

    def _stop_realtime(self) -> None:
        if self._rt_stop:
            self._rt_stop.set()
            self._rt_worker.join(timeout=5)
            self._rt_stop = self._rt_worker = None
        self.status.realtime_update(enabled=False, reason="off")

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
        self.config.webhook_url = url  # the workers read it on every post
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

    # ---------------------------------------------------------- realtime

    @staticmethod
    def realtime_available() -> bool:
        return sys.platform == "win32"

    def set_realtime(self, on: bool) -> None:
        save_settings(self.config_path, {"realtime": on})
        self.config.realtime = on
        log.info("realtime posting (experimental): %s", "on" if on else "off")
        if on:
            self._start_realtime()
        else:
            self._stop_realtime()

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
