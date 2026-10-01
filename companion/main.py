"""DOINK companion: watches the SavedVariables file and posts new events to Discord."""

import argparse
import itertools
import logging
import sys
import time
from collections import defaultdict
from pathlib import Path

from doink.config import Config, load_config
from doink.discord import MAX_EMBEDS_PER_MESSAGE, Webhook, WebhookError, build_embed
from doink.parser import char_key, parse_events
from doink.state import State
from doink.watcher import Watcher

log = logging.getLogger("doink")

MAX_RETRY_DELAY = 60.0


def process(config: Config, state: State, webhook: Webhook) -> bool:
    """Post every event newer than what we've seen. Returns False if a post
    failed; state is saved after each message, so a retry resumes cleanly."""
    try:
        # latin-1 maps bytes 1:1; the parser decodes UTF-8 per string.
        text = config.savedvariables_path.read_bytes().decode("latin-1")
    except OSError as e:
        log.warning("can't read %s: %s", config.savedvariables_path, e)
        return False

    by_char = defaultdict(list)
    for event in parse_events(text):
        by_char[char_key(event)].append(event)

    for key, events in by_char.items():
        events.sort(key=lambda e: e["seq"])
        newest = events[-1]["seq"]
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

        to_post = [e for e in new if config.enabled(e["type"])]
        for chunk in itertools.batched(to_post, MAX_EMBEDS_PER_MESSAGE):
            try:
                webhook.send([build_embed(e) for e in chunk])
            except WebhookError as e:
                log.error("%s: post failed: %s", key, e)
                return False
            log.info("%s: posted %s", key,
                     ", ".join(f"#{e['seq']} {e['type']}" for e in chunk))
            state.set_last_seen(key, chunk[-1]["seq"])

        if last is None or newest > last:
            state.set_last_seen(key, newest)  # also skips past disabled types
    return True


def run(config: Config, once: bool) -> int:
    state = State(config.state_path, persist=not config.dry_run)
    webhook = Webhook(config.webhook_url, dry_run=config.dry_run)

    if once:
        return 0 if process(config, state, webhook) else 1

    log.info("watching %s%s", config.savedvariables_path,
             " (dry run)" if config.dry_run else "")
    watcher = Watcher(config.savedvariables_path)
    dirty = False
    failures = 0
    retry_at = 0.0
    while True:
        if watcher.poll():
            dirty = True
        if dirty and time.monotonic() >= retry_at:
            if process(config, state, webhook):
                dirty = False
                failures = 0
            else:
                failures += 1
                delay = min(MAX_RETRY_DELAY, config.poll_interval * 2 ** failures)
                log.info("retrying in %.0fs", delay)
                retry_at = time.monotonic() + delay
        time.sleep(config.poll_interval)


def main() -> int:
    here = Path(__file__).resolve().parent
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", type=Path, default=here / "config.toml")
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
