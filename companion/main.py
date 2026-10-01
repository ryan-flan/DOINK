"""DOINK companion: watches the SavedVariables file and posts new events to Discord."""

import argparse
import itertools
import logging
import sys
import time
from collections import defaultdict
from pathlib import Path

from doink.config import Config, load_config
from doink.discord import (MAX_EMBEDS_PER_MESSAGE, WebhookError, WebhookPool,
                           build_embed, is_webhook_url)
from doink.parser import char_key, legacy_char_key, parse_events, parse_webhooks
from doink.state import State
from doink.watcher import Watcher

log = logging.getLogger("doink")

MAX_RETRY_DELAY = 60.0


def app_dir() -> Path:
    """Where config.toml and state.json live: next to the .exe when frozen
    by PyInstaller (``__file__`` is then a temp dir), else next to main.py."""
    if getattr(sys, "frozen", False):
        return Path(sys.executable).resolve().parent
    return Path(__file__).resolve().parent


def webhook_for(key: str, game_hooks: dict[str, str], config: Config) -> str:
    return game_hooks.get(key) or game_hooks.get("*") or config.webhook_url


def process(config: Config, path: Path, state: State, pool: WebhookPool) -> bool:
    """Post every event newer than what we've seen. Returns False if anything
    is left to retry; state is saved after each message, so a retry resumes
    cleanly."""
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
                log.error("%s: no webhook. In game: /doink webhook <url>, then /reload "
                          "(or set webhook_url in config.toml)", key)
                ok = False
                continue  # state untouched: these post once a webhook exists
            if not is_webhook_url(url):
                log.error("%s: webhook isn't a Discord webhook URL", key)
                ok = False
                continue

        for chunk in itertools.batched(new, MAX_EMBEDS_PER_MESSAGE):
            webhook = pool.get(url or "dry-run")
            try:
                webhook.send([build_embed(e) for e in chunk])
            except WebhookError as e:
                log.error("%s: post failed: %s", key, e)
                return False
            log.info("%s: %s %s", key, "printed" if config.dry_run else "posted",
                     ", ".join(f"#{e['seq']} {e['type']}" for e in chunk))
            state.set_last_seen(key, chunk[-1]["seq"])

        if last is None or newest > last:
            state.set_last_seen(key, newest)  # also covers backlog we skipped
    return ok


def run(config: Config, once: bool) -> int:
    state = State(config.state_path, persist=not config.dry_run)
    pool = WebhookPool(dry_run=config.dry_run)
    paths = config.savedvariables_paths

    if once:
        results = [process(config, p, state, pool) for p in paths]
        return 0 if all(results) else 1

    for path in paths:
        log.info("watching %s%s", path, " (dry run)" if config.dry_run else "")
    watchers = [Watcher(p) for p in paths]
    dirty: set[Path] = set()
    failures = 0
    retry_at = 0.0
    while True:
        for watcher in watchers:
            if watcher.poll():
                dirty.add(watcher.path)
        if dirty and time.monotonic() >= retry_at:
            dirty = {p for p in dirty if not process(config, p, state, pool)}
            if dirty:
                failures += 1
                delay = min(MAX_RETRY_DELAY, config.poll_interval * 2 ** failures)
                log.info("retrying in %.0fs", delay)
                retry_at = time.monotonic() + delay
            else:
                failures = 0
        time.sleep(config.poll_interval)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", type=Path, default=app_dir() / "config.toml")
    parser.add_argument("--dry-run", action="store_true",
                        help="print embeds instead of posting; don't save state")
    parser.add_argument("--once", action="store_true",
                        help="process the file once and exit")
    parser.add_argument("-v", "--verbose", action="store_true")
    args = parser.parse_args()

    logging.basicConfig(
        level=logging.DEBUG if args.verbose else logging.INFO,
        format="%(asctime)s %(levelname)-7s %(message)s",
        datefmt="%H:%M:%S",
    )
    config = load_config(args.config, dry_run=args.dry_run)
    try:
        return run(config, once=args.once)
    except KeyboardInterrupt:
        log.info("stopped")
        return 0


if __name__ == "__main__":
    sys.exit(main())
