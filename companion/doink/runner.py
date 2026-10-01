"""The watch-and-post loop: read SavedVariables, post new events, remember."""

import itertools
import logging
import threading
import time
from collections import defaultdict
from pathlib import Path

from .config import Config
from .discord import (MAX_EMBEDS_PER_MESSAGE, WebhookError, WebhookPool,
                      build_embed, is_webhook_url)
from .parser import char_key, full_name, legacy_char_key, parse_events, parse_webhooks
from .state import State
from .status import Status
from .watcher import Watcher

log = logging.getLogger("doink")

MAX_RETRY_DELAY = 60.0


def webhook_for(key: str, game_hooks: dict[str, str], config: Config) -> str:
    return game_hooks.get(key) or game_hooks.get("*") or config.webhook_url


def process(config: Config, path: Path, state: State, pool: WebhookPool,
            status: Status | None = None) -> bool:
    """Post every event newer than what we've seen. Returns False if anything
    is left to retry; state is saved after each message, so a retry resumes
    cleanly."""
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
            last = None

        if last is None:
            # First time we've seen this character: don't dump the whole
            # 500-entry ring buffer into Discord.
            new = events[-config.max_backlog:] if config.max_backlog > 0 else []
            if len(new) < len(events):
                log.info("%s: new character, skipping %d older events",
                         key, len(events) - len(new))
        else:
            new = [e for e in events if e["seq"] > last]

        url = webhook_for(key, game_hooks, config)
        if new and not config.dry_run:
            if not url:
                log.error("%s: no webhook. Set one in DOINK's settings, or in game: "
                          "/doink webhook <url>", key)
                status.failed(f"no webhook for {full_name(events[-1])}. "
                              "Open DOINK's settings to add one")
                ok = False
                continue  # state untouched: these post once a webhook exists
            if not is_webhook_url(url):
                log.error("%s: webhook isn't a Discord webhook URL", key)
                status.failed(f"bad webhook for {full_name(events[-1])}")
                ok = False
                continue

        for chunk in itertools.batched(new, MAX_EMBEDS_PER_MESSAGE):
            webhook = pool.get(url or "dry-run")
            try:
                webhook.send([build_embed(e) for e in chunk])
            except WebhookError as e:
                log.error("%s: post failed: %s", key, e)
                status.failed(f"posting to Discord failed: {e}")
                return False
            log.info("%s: %s %s", key, "printed" if config.dry_run else "posted",
                     ", ".join(f"#{e['seq']} {e['type']}" for e in chunk))
            for e in chunk:
                status.posted(full_name(e), e["type"])
            state.set_last_seen(key, chunk[-1]["seq"])

        if last is None or newest > last:
            state.set_last_seen(key, newest)  # also covers backlog we skipped
    return ok


def run(config: Config, once: bool = False, stop: threading.Event | None = None,
        status: Status | None = None) -> int:
    stop = stop or threading.Event()
    status = status or Status()
    state = State(config.state_path, persist=not config.dry_run)
    pool = WebhookPool(dry_run=config.dry_run)
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
