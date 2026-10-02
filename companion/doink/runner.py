"""The watch-and-post loop, and the realtime reader that feeds the same path."""

import itertools
import logging
import sys
import threading
import time
from collections import defaultdict
from collections.abc import Callable
from pathlib import Path

from .config import Config
from .discord import (MAX_EMBEDS_PER_MESSAGE, WebhookError, WebhookPool,
                      build_embed, is_webhook_url)
from .parser import (char_key, decode_message, full_name, legacy_char_key,
                     parse_events, parse_webhooks)
from .state import State
from .status import Status
from .watcher import Watcher

log = logging.getLogger("doink")

MAX_RETRY_DELAY = 60.0
REALTIME_FOREGROUND_INTERVAL = 1 / 8  # two captures per 250 ms chunk
REALTIME_IDLE_INTERVAL = 1.0          # WoW not running, minimized or behind another window


def webhook_for(key: str, game_hooks: dict[str, str], config: Config) -> str:
    return game_hooks.get(key) or game_hooks.get("*") or config.webhook_url


def post_events(config: Config, key: str, events: list[dict], state: State,
                pool: WebhookPool, status: Status, game_hooks: dict[str, str],
                first_sight_backlog: int = 0, realtime: bool = False) -> bool:
    """Post ``events`` (one character, ascending seq) and record each one.
    Returns False if they couldn't be posted; state is saved after each
    message, so a retry resumes cleanly."""
    url = webhook_for(key, game_hooks, config)
    if events and not config.dry_run:
        if not url:
            log.error("%s: no webhook. Set one in DOINK's settings, or in game: "
                      "/doink webhook <url>", key)
            status.failed(f"no webhook for {full_name(events[-1])}. "
                          "Open DOINK's settings to add one")
            return False  # state untouched: these post once a webhook exists
        if not is_webhook_url(url):
            log.error("%s: webhook isn't a Discord webhook URL", key)
            status.failed(f"bad webhook for {full_name(events[-1])}")
            return False

    for chunk in itertools.batched(events, MAX_EMBEDS_PER_MESSAGE):
        webhook = pool.get(url or "dry-run")
        try:
            webhook.send([build_embed(e) for e in chunk])
        except WebhookError as e:
            log.error("%s: post failed: %s", key, e)
            status.failed(f"posting to Discord failed: {e}")
            return False
        log.info("%s: %s %s%s", key, "printed" if config.dry_run else "posted",
                 ", ".join(f"#{e['seq']} {e['type']}" for e in chunk),
                 " (realtime)" if realtime else "")
        for e in chunk:
            status.posted(full_name(e), e["type"], realtime=realtime)
            state.mark_posted(key, e["seq"], first_sight_backlog)
    return True


def process(config: Config, path: Path, state: State, pool: WebhookPool,
            status: Status | None = None) -> bool:
    """Post every event in the SavedVariables file that hasn't been posted.
    Returns False if anything is left to retry."""
    status = status or Status()
    try:
        # latin-1 maps bytes 1:1; the parser decodes UTF-8 per string.
        text = path.read_bytes().decode("latin-1")
    except OSError as e:
        log.warning("can't read %s: %s", path, e)
        return False

    game_hooks = parse_webhooks(text)
    by_char = defaultdict(list)
    for event in parse_events(text):
        by_char[char_key(event)].append(event)

    ok = True
    for key, events in by_char.items():
        events.sort(key=lambda e: e["seq"])
        newest = events[-1]["seq"]
        legacy = legacy_char_key(events[-1])
        if legacy != key and state.rename(legacy, key):
            log.info("%s: carried over posting history from %s", key, legacy)
        last = state.last_seen(key)

        if last is not None and newest < last:
            log.warning("%s: newest seq %d is below last seen %d. "
                        "SavedVariables reset? Starting over.", key, newest, last)
            state.forget(key)
            last = None

        if last is None:
            # First time we've seen this character: don't dump the whole
            # 500-entry ring buffer into Discord.
            new = events[-config.max_backlog:] if config.max_backlog > 0 else []
            if len(new) < len(events):
                log.info("%s: new character, skipping %d older events",
                         key, len(events) - len(new))
        else:
            new = [e for e in events if state.is_pending(key, e["seq"])]

        if not post_events(config, key, new, state, pool, status, game_hooks):
            ok = False
            continue
        if last is None:
            state.baseline(key, newest)  # older ones were skipped on purpose
        else:
            state.prune_missing(key, {e["seq"] for e in events}, newest)
    return ok


def run(config: Config, once: bool = False, stop: threading.Event | None = None,
        status: Status | None = None, state: State | None = None,
        pool: WebhookPool | None = None) -> int:
    stop = stop or threading.Event()
    status = status or Status()
    state = state or State(config.state_path, persist=not config.dry_run)
    pool = pool or WebhookPool(dry_run=config.dry_run)
    paths = config.savedvariables_paths

    if once:
        results = [process(config, p, state, pool, status) for p in paths]
        return 0 if all(results) else 1

    for path in paths:
        log.info("watching %s%s", path, " (dry run)" if config.dry_run else "")
    status.watching(len(paths))
    watchers = [Watcher(p) for p in paths]
    dirty: set[Path] = set()
    failures = 0
    retry_at = 0.0
    while not stop.is_set():
        for watcher in watchers:
            if watcher.poll():
                dirty.add(watcher.path)
        if dirty and time.monotonic() >= retry_at:
            dirty = {p for p in dirty if not process(config, p, state, pool, status)}
            if dirty:
                failures += 1
                delay = min(MAX_RETRY_DELAY, config.poll_interval * 2 ** failures)
                log.info("retrying in %.0fs", delay)
                retry_at = time.monotonic() + delay
            else:
                failures = 0
                status.ok()
        stop.wait(config.poll_interval)
    return 0


class RealtimeWorker(threading.Thread):
    """Reads the addon's pixel strip and posts what it finds (experimental).

    Shares ``state`` and ``pool`` with the file watcher so the two paths
    never double-post. Stops itself if the meter says reading has become
    expensive; the watcher is unaffected."""

    def __init__(self, config: Config, state: State, pool: WebhookPool, status: Status,
                 stop: threading.Event, game_hooks: Callable[[], dict[str, str]]):
        super().__init__(name="realtime", daemon=True)
        self.config = config
        self.state = state
        self.pool = pool
        self.status = status
        self.stop_event = stop
        self.game_hooks = game_hooks

    def run(self) -> None:
        if sys.platform != "win32":
            log.info("realtime reading needs Windows; skipped")
            self.status.realtime_update(enabled=False, reason="needs Windows")
            return
        from . import pixel

        try:
            reader = pixel.Reader()
        except OSError as e:
            log.error("realtime: can't start the screen reader: %s", e)
            self.status.realtime_update(enabled=False, reason="couldn't start the screen reader")
            return
        self.status.realtime_update(enabled=True, reason=None)
        log.info("realtime: reading the WoW window's top and bottom bands (experimental)")
        last_report = time.monotonic()
        last_window = None
        try:
            while not self.stop_event.is_set():
                started = time.perf_counter()
                for raw in reader.poll():
                    self.handle_message(raw)
                snapshot = reader.meter.snapshot()
                self.status.realtime_update(window=reader.window, strip=reader.strip,
                                            strip_seen=reader.last_seen, meter=snapshot)
                if reader.window != last_window:
                    log.info("realtime: WoW window %s", reader.window)
                    last_window = reader.window
                while reader.trace:
                    at, reason = reader.trace.popleft()
                    log.debug("realtime: strip candidate rejected: %s", reason)
                if reader.window == "foreground" and time.monotonic() - last_report >= 60:
                    # A cost line a minute while reading, for the README numbers
                    # and for anyone's bug report.
                    log.info("realtime: %s captures/s, %s ms avg, %s%% CPU, %s MB, %s GDI handles",
                             snapshot["rate_hz"], snapshot["avg_ms"], snapshot["cpu_pct"],
                             None if snapshot["ws_mb"] is None else round(snapshot["ws_mb"]),
                             snapshot["gdi"])
                    last_report = time.monotonic()
                reason = reader.meter.should_stop()
                if reason:
                    log.warning("realtime: stopping: %s", reason)
                    self.status.realtime_update(enabled=False, reason=reason)
                    return
                interval = (REALTIME_FOREGROUND_INTERVAL if reader.window == "foreground"
                            else REALTIME_IDLE_INTERVAL)
                self.stop_event.wait(max(0.0, interval - (time.perf_counter() - started)))
        except Exception:
            log.exception("realtime reader crashed")
            self.status.realtime_update(enabled=False, reason="stopped after an error; see the log")
        finally:
            reader.close()
            if self.stop_event.is_set():  # asked to stop; other exits set their own reason
                self.status.realtime_update(enabled=False, reason="off")

    # Not "_handle": threading.Thread owns an attribute of that name on 3.13+.
    def handle_message(self, raw: bytes) -> None:
        message = decode_message(raw)
        if message is None:
            log.warning("realtime: unreadable message: %.120r", raw)
            return
        if message.get("type") == "hello":
            log.info("realtime: hello from %s (addon %s%s)", full_name(message),
                     message.get("addon"), ", test pattern" if message.get("test") else "")
            self.status.realtime_hello(full_name(message), str(message.get("addon", "?")),
                                       bool(message.get("test")), message.get("position"))
            return
        key = char_key(message)
        if not self.state.is_pending(key, message["seq"]):
            return  # already posted (a repeat, or the file beat us to it)
        post_events(self.config, key, [message], self.state, self.pool, self.status,
                    self.game_hooks(), first_sight_backlog=self.config.max_backlog,
                    realtime=True)
