"""Extract DOINK events from the SavedVariables file.

The addon stores each event as a JSON string inside a Lua table, so instead
of a Lua parser we find the ``["events"] = { ... }`` blocks and read the Lua
string literals inside them. WoW writes them as double-quoted strings with
backslash escapes, e.g. ``"{\\"seq\\":1,...}",``.
"""

import json
import logging
import re

log = logging.getLogger(__name__)

EVENTS_BLOCK = re.compile(r'\["events"\]\s*=\s*\{')
REQUIRED_FIELDS = ("seq", "char", "realm", "type")

# Character after the backslash -> what it stands for.
_ESCAPES = {
    "n": "\n", "t": "\t", "r": "\r", "a": "\a", "b": "\b", "f": "\f",
    "v": "\v", "\\": "\\", '"': '"', "'": "'", "\n": "\n",
}
_DIGITS = "0123456789"


class ParseError(Exception):
    pass


def char_key(event: dict) -> str:
    """The addon's per-character key, e.g. ``Paul-Classic Beta PvE 2``."""
    return f"{event['char']}-{event['realm']}"


def parse_events(text: str) -> list[dict]:
    """Return every valid event in the file, in file order.

    ``text`` must be the file's bytes decoded as latin-1, so that ``\\ddd``
    byte escapes and raw UTF-8 bytes can be put back together before
    decoding as UTF-8.
    """
    events = []
    for match in EVENTS_BLOCK.finditer(text):
        events.extend(_parse_block(text, match.end()))
    return events


def _parse_block(text: str, i: int):
    n = len(text)
    while i < n:
        c = text[i]
        if c in " \t\r\n,":
            i += 1
        elif text.startswith("--", i):  # WoW sometimes writes "-- [1]" comments
            end = text.find("\n", i)
            i = n if end == -1 else end
        elif c == "}":
            return
        elif c in "\"'":
            try:
                raw, i = _read_string(text, i)
            except ParseError as e:
                log.warning("events block at offset %d: %s; skipping rest of block", i, e)
                return
            event = _decode_event(raw)
            if event is not None:
                yield event
        else:
            log.warning("unexpected %r in events block at offset %d; skipping rest of block", c, i)
            return


def _read_string(text: str, i: int) -> tuple[str, int]:
    """Read the Lua short string whose opening quote is at ``text[i]``.

    Returns the unescaped value and the index just past the closing quote.
    """
    quote = text[i]
    out = []
    i += 1
    n = len(text)
    while i < n:
        c = text[i]
        if c == quote:
            return "".join(out), i + 1
        if c == "\n":
            raise ParseError("unterminated string")
        if c != "\\":
            out.append(c)
            i += 1
            continue

        i += 1
        if i >= n:
            break
        e = text[i]
        if e in _DIGITS:  # \ddd: up to three decimal digits, one byte
            j = i
            while j < n and j < i + 3 and text[j] in _DIGITS:
                j += 1
            value = int(text[i:j])
            if value > 255:
                raise ParseError(f"escape \\{text[i:j]} out of range")
            out.append(chr(value))
            i = j
        elif e in _ESCAPES:
            out.append(_ESCAPES[e])
            i += 1
        else:
            raise ParseError(f"unknown escape \\{e}")
    raise ParseError("unterminated string")


def _decode_event(raw: str) -> dict | None:
    text = raw.encode("latin-1").decode("utf-8", errors="replace")
    try:
        event = json.loads(text)
    except json.JSONDecodeError as e:
        log.warning("skipping unparseable event (%s): %.120s", e, text)
        return None
    if not isinstance(event, dict) or any(k not in event for k in REQUIRED_FIELDS):
        log.warning("skipping event missing required fields: %.120s", text)
        return None
    if not isinstance(event["seq"], int):
        log.warning("skipping event with non-integer seq: %.120s", text)
        return None
    return event
