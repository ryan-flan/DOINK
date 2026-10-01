"""Discord embed builders and webhook transport."""

import json
import logging
import time
import urllib.error
import urllib.request
from datetime import UTC, datetime

from . import __version__

log = logging.getLogger(__name__)

MAX_EMBEDS_PER_MESSAGE = 10  # Discord's limit
MIN_POST_INTERVAL = 2.0      # seconds; stays under the ~30/min webhook limit
USER_AGENT = f"DOINK-companion/{__version__}"

# RAID_CLASS_COLORS as of Classic Era.
CLASS_COLOURS = {
    "WARRIOR": 0xC79C6E,
    "PALADIN": 0xF58CBA,
    "HUNTER": 0xABD473,
    "ROGUE": 0xFFF569,
    "PRIEST": 0xFFFFFF,
    "SHAMAN": 0x0070DE,
    "MAGE": 0x69CCF0,
    "WARLOCK": 0x9482C9,
    "DRUID": 0xFF7D0A,
}
DEFAULT_COLOUR = 0x99AAB5


# ------------------------------------------------------------------ embeds

def _level_up(event: dict, data: dict) -> dict:
    return {"title": f"{event['char']} reached level {data.get('level', '?')}"}


def _generic(event: dict, data: dict) -> dict:
    # Fallback for types without a builder yet (M3), so nothing is lost.
    body = json.dumps(data, indent=2, ensure_ascii=False)[:3900]
    return {
        "title": f"{event['char']}: {event['type']}",
        "description": f"```json\n{body}\n```",
    }


BUILDERS = {
    "level_up": _level_up,
}


def build_embed(event: dict) -> dict:
    data = event.get("data") or {}
    embed = BUILDERS.get(event["type"], _generic)(event, data)

    embed.setdefault("color", CLASS_COLOURS.get(event.get("class"), DEFAULT_COLOUR))
    footer = f"{event['char']}-{event['realm']}"
    if event.get("test"):
        embed["title"] = "[TEST] " + embed["title"]
        footer += " · test event"
    embed["footer"] = {"text": footer}
    if isinstance(event.get("ts"), int):
        embed["timestamp"] = datetime.fromtimestamp(event["ts"], UTC).isoformat()
    return embed


# ------------------------------------------------------------------ transport

class WebhookError(Exception):
    pass


class Webhook:
    def __init__(self, url: str, dry_run: bool = False):
        separator = "&" if "?" in url else "?"
        self.url = f"{url}{separator}wait=true"  # wait=true: get real errors back
        self.dry_run = dry_run
        self._next_post = 0.0

    def send(self, embeds: list[dict]) -> None:
        payload = {"embeds": embeds}
        if self.dry_run:
            print(json.dumps(payload, indent=2, ensure_ascii=False), flush=True)
            return

        delay = self._next_post - time.monotonic()
        if delay > 0:
            time.sleep(delay)

        body = json.dumps(payload).encode("utf-8")
        for _ in range(5):
            request = urllib.request.Request(
                self.url,
                data=body,
                method="POST",
                headers={"Content-Type": "application/json", "User-Agent": USER_AGENT},
            )
            try:
                with urllib.request.urlopen(request, timeout=15) as response:
                    self._schedule_next(response.headers)
                    return
            except urllib.error.HTTPError as e:
                detail = e.read()
                if e.code == 429:
                    retry_after = float(json.loads(detail).get("retry_after", 1))
                    log.info("rate limited; retrying in %.1fs", retry_after)
                    time.sleep(retry_after)
                    continue
                raise WebhookError(f"HTTP {e.code}: {detail[:200]!r}") from None
            except urllib.error.URLError as e:
                raise WebhookError(f"network error: {e.reason}") from None
        raise WebhookError("still rate limited after 5 attempts")

    def _schedule_next(self, headers) -> None:
        wait = MIN_POST_INTERVAL
        if headers.get("X-RateLimit-Remaining") == "0":
            wait = max(wait, float(headers.get("X-RateLimit-Reset-After", 0)))
        self._next_post = time.monotonic() + wait
