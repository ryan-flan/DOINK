"""Discord embed builders and webhook transport."""

import json
import logging
import re
import time
import urllib.error
import urllib.request
from datetime import UTC, datetime

from . import __version__
from .parser import char_key, full_name

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

# ITEM_QUALITY_COLORS: poor, common, uncommon, rare, epic, legendary, artifact.
QUALITY_COLOURS = [0x9D9D9D, 0xFFFFFF, 0x1EFF00, 0x0070DD, 0xA335EE, 0xFF8000, 0xE6CC80]

# Verified against wowhead.com/forever/item=769 and /quest=783.
WOWHEAD = "https://www.wowhead.com/forever"


def format_money(copper: int) -> str:
    gold, rest = divmod(copper, 10000)
    silver, copper = divmod(rest, 100)
    parts = [f"{gold}g"] if gold else []
    if silver:
        parts.append(f"{silver}s")
    if copper or not parts:
        parts.append(f"{copper}c")
    return " ".join(parts)


def _field(name: str, value) -> dict:
    return {"name": name, "value": str(value), "inline": True}


# ------------------------------------------------------------------ embeds

def _level_up(event: dict, data: dict) -> dict:
    return {"title": f"{full_name(event)} reached level {data.get('level', '?')}"}


def _loot(event: dict, data: dict) -> dict:
    name = data.get("name") or "an item"
    qty = data.get("qty") or 1
    embed = {"title": f"{full_name(event)} looted {name}" + (f" ×{qty}" if qty > 1 else "")}
    if isinstance(data.get("item_id"), int):
        embed["url"] = f"{WOWHEAD}/item={data['item_id']}"
    quality = data.get("quality")
    if isinstance(quality, int) and 0 <= quality < len(QUALITY_COLOURS):
        embed["color"] = QUALITY_COLOURS[quality]
    if data.get("vendor_value"):
        embed["fields"] = [_field("Vendor value", format_money(data["vendor_value"]))]
    return embed


def _death(event: dict, data: dict) -> dict:
    killer = data.get("killer")
    where = data.get("zone") or "somewhere"
    if data.get("subzone"):
        where = f"{data['subzone']}, {where}"
    return {
        "title": f"{full_name(event)} died" + (f" to {killer}" if killer else ""),
        "description": f"in {where}",
    }


def _quest(event: dict, data: dict) -> dict:
    embed = {"title": f"{full_name(event)} completed {data.get('title') or 'a quest'}"}
    if isinstance(data.get("quest_id"), int):
        embed["url"] = f"{WOWHEAD}/quest={data['quest_id']}"
    if data.get("xp"):
        embed["fields"] = [_field("XP", f"{data['xp']:,}")]
    return embed


def _boss_kill(event: dict, data: dict) -> dict:
    embed = {"title": f"{full_name(event)} defeated {data.get('name') or 'a boss'}"}
    fields = []
    if data.get("instance"):
        fields.append(_field("Instance", data["instance"]))
    if data.get("group_size"):
        fields.append(_field("Group size", data["group_size"]))
    if fields:
        embed["fields"] = fields
    return embed


def _skill_up(event: dict, data: dict) -> dict:
    skill = data.get("skill") or "a skill"
    rank = data.get("rank", "?")
    embed = {"title": f"{full_name(event)} reached {rank} {skill}"}
    if data.get("max_rank"):
        embed["description"] = f"{rank} / {data['max_rank']}"
    return embed


def _generic(event: dict, data: dict) -> dict:
    # Fallback for types without a builder yet (M3), so nothing is lost.
    body = json.dumps(data, indent=2, ensure_ascii=False)[:3900]
    return {
        "title": f"{full_name(event)}: {event['type']}",
        "description": f"```json\n{body}\n```",
    }


BUILDERS = {
    "level_up": _level_up,
    "loot": _loot,
    "death": _death,
    "quest": _quest,
    "boss_kill": _boss_kill,
    "skill_up": _skill_up,
}


def build_embed(event: dict) -> dict:
    data = event.get("data") or {}
    embed = BUILDERS.get(event["type"], _generic)(event, data)

    embed.setdefault("color", CLASS_COLOURS.get(event.get("class"), DEFAULT_COLOUR))
    footer = char_key(event)
    if event.get("test"):
        embed["title"] = "[TEST] " + embed["title"]
        footer += " · test event"
    embed["footer"] = {"text": footer}
    if isinstance(event.get("ts"), int):
        embed["timestamp"] = datetime.fromtimestamp(event["ts"], UTC).isoformat()
    return embed


# ------------------------------------------------------------------ transport

WEBHOOK_URL = re.compile(r"^https://(discord|discordapp)\.com/api/webhooks/\d+/[\w-]+$")


def is_webhook_url(url: str) -> bool:
    return bool(WEBHOOK_URL.match(url))


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


class WebhookPool:
    """One Webhook per URL, so each keeps its own rate-limit timing."""

    def __init__(self, dry_run: bool = False):
        self.dry_run = dry_run
        self._hooks: dict[str, Webhook] = {}

    def get(self, url: str) -> Webhook:
        if url not in self._hooks:
            self._hooks[url] = Webhook(url, dry_run=self.dry_run)
        return self._hooks[url]
