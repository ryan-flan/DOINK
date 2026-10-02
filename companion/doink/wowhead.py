"""Item details from Wowhead, for richer loot embeds.

Wowhead serves a small JSON tooltip per item for the Forever data set:
``https://nether.wowhead.com/forever/tooltip/item/<id>`` returns the name,
quality, icon name and the tooltip as HTML (verified 2026-10-03 with items
6314 and 769; unknown ids answer 404). The icon lives at
``https://wow.zamimg.com/images/wow/icons/large/<icon>.jpg``. Only the item
id ever leaves the machine. Results are cached in memory for the session;
``wowhead = false`` in config.toml turns the lookups off.
"""

import html
import json
import logging
import re
import threading
import urllib.error
import urllib.request
from dataclasses import dataclass, field

from . import __version__

log = logging.getLogger(__name__)

TOOLTIP_URL = "https://nether.wowhead.com/forever/tooltip/item/{item_id}?locale=0"
ICON_URL = "https://wow.zamimg.com/images/wow/icons/large/{icon}.jpg"
USER_AGENT = f"DOINK-companion/{__version__} (+https://github.com/ryan-flan/DOINK)"
TIMEOUT = 5.0
MAX_LINES = 12

_MONEY = re.compile(r'<span class="money(gold|silver|copper)">(\d+)</span>')
_BREAKS = re.compile(r"<br\s*/?>|</?(?:td|tr|div|table)\b[^>]*>", re.IGNORECASE)
_TAGS = re.compile(r"<!--.*?-->|<[^>]+>", re.DOTALL)
_SKIP = ("Sell Price", "Max Stack")  # the game already gives us the stack value


@dataclass
class ItemInfo:
    item_id: int
    name: str
    quality: int | None
    icon_url: str | None
    lines: list[str] = field(default_factory=list)  # tooltip text, name excluded


def tooltip_lines(tooltip_html: str) -> list[str]:
    """The tooltip's text lines, in order, without the item name, sell
    price or stack size: 'Item Level 27', 'Binds when picked up', 'Back',
    '22 Armor', '+3 Stamina', 'Requires Level 22', 'Equip: ...'."""
    text = _MONEY.sub(lambda m: f"{m.group(2)}{m.group(1)[0]} ", tooltip_html)
    text = _BREAKS.sub("\n", text)
    text = html.unescape(_TAGS.sub("", text))
    lines = []
    for raw in text.split("\n"):
        line = " ".join(raw.split())
        if line and line not in lines and not line.startswith(_SKIP):
            lines.append(line)
    return lines[1:][:MAX_LINES]  # the first line is the name


def parse_item(item_id: int, payload: bytes) -> ItemInfo | None:
    try:
        data = json.loads(payload.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError):
        return None
    if not isinstance(data, dict) or not data.get("name"):
        return None
    icon = data.get("icon")
    quality = data.get("quality")
    return ItemInfo(
        item_id=item_id,
        name=str(data["name"]),
        quality=quality if isinstance(quality, int) else None,
        icon_url=ICON_URL.format(icon=icon) if isinstance(icon, str) and re.fullmatch(r"[a-z0-9_]+", icon) else None,
        lines=tooltip_lines(str(data.get("tooltip", ""))),
    )


def fetch_item(item_id: int, opener=urllib.request.urlopen) -> ItemInfo | None:
    """One GET to Wowhead; None on any failure (never raises)."""
    request = urllib.request.Request(TOOLTIP_URL.format(item_id=item_id),
                                     headers={"User-Agent": USER_AGENT})
    try:
        with opener(request, timeout=TIMEOUT) as response:
            return parse_item(item_id, response.read())
    except urllib.error.HTTPError as e:
        log.debug("wowhead: item %d: HTTP %s", item_id, e.code)
    except (urllib.error.URLError, OSError, ValueError) as e:
        log.debug("wowhead: item %d: %s", item_id, e)
    return None


class ItemCache:
    """Per-session memory of looked-up items, misses included, so one bad
    network moment or unknown item never costs more than one request."""

    def __init__(self, fetch=fetch_item):
        self._fetch = fetch
        self._items: dict[int, ItemInfo | None] = {}
        self._lock = threading.Lock()

    def lookup(self, item_id) -> ItemInfo | None:
        if not isinstance(item_id, int) or item_id <= 0:
            return None
        with self._lock:
            if item_id in self._items:
                return self._items[item_id]
        info = self._fetch(item_id)
        with self._lock:
            self._items[item_id] = info
        return info
