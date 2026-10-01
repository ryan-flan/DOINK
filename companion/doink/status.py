"""What the companion is doing, for the tray tooltip, notifications and the
settings window.

Written by the worker threads, read by the tray and UI threads.
"""

import threading
import time
from collections import deque
from collections.abc import Callable

TOOLTIP_MAX = 127  # Windows tray tooltip limit (128 incl. terminator)
RECENT_MAX = 8     # posts listed in the settings window


class Status:
    def __init__(self, on_change: Callable[[str | None], None] | None = None):
        # on_change(new_error): new_error is set only when a *new* problem
        # appears, so the tray notifies once rather than on every retry.
        self.on_change = on_change or (lambda new_error: None)
        self._lock = threading.Lock()
        self._watching = 0
        self._last_post: tuple[float, str] | None = None
        self._recent: deque[tuple[float, str, str, bool]] = deque(maxlen=RECENT_MAX)
        self._error: str | None = None
        self._realtime: dict = {
            "enabled": False,     # the reader thread is running
            "reason": "off",      # why it isn't, when it isn't
            "window": "none",     # none | minimized | background | foreground
            "strip": None,        # (block_px, blocks) last decoded
            "strip_seen": None,   # unix time the strip was last decoded
            "hello": None,        # {"at", "who", "addon", "test", "position"}
            "meter": {},          # ResourceMeter.snapshot()
        }

    def watching(self, count: int) -> None:
        with self._lock:
            self._watching = count
            if count:
                self._error = None
        self.on_change(None)

    def posted(self, who: str, what: str, realtime: bool = False) -> None:
        now = time.time()
        with self._lock:
            self._last_post = (now, f"{who}: {what}")
            self._recent.appendleft((now, who, what, realtime))
            self._error = None
        self.on_change(None)

    def recent(self) -> list[tuple[float, str, str, bool]]:
        """Newest first: (unix time, who, event type, via realtime)."""
        with self._lock:
            return list(self._recent)

    @property
    def error(self) -> str | None:
        with self._lock:
            return self._error

    def failed(self, message: str) -> None:
        with self._lock:
            new = message != self._error
            self._error = message
        if new:
            self.on_change(message)

    def ok(self) -> None:
        """Everything pending was handled; clear any standing problem."""
        with self._lock:
            had_error = self._error is not None
            self._error = None
        if had_error:
            self.on_change(None)

    # ---------------------------------------------------------- realtime

    def realtime_update(self, **fields) -> None:
        """Called up to 8x/s by the reader; only an on/off change notifies."""
        with self._lock:
            was = self._realtime["enabled"]
            self._realtime.update(fields)
            changed = self._realtime["enabled"] != was
        if changed:
            self.on_change(None)

    def realtime_hello(self, who: str, addon: str, test: bool, position: str | None) -> None:
        with self._lock:
            self._realtime["hello"] = {"at": time.time(), "who": who, "addon": addon,
                                       "test": test, "position": position}

    def realtime(self) -> dict:
        with self._lock:
            return dict(self._realtime)

    # ---------------------------------------------------------- summary

    def summary(self) -> str:
        with self._lock:
            if self._error:
                text = f"DOINK: {self._error}"
            elif self._last_post:
                at, what = self._last_post
                text = f"DOINK: last post {time.strftime('%H:%M', time.localtime(at))}, {what}"
            else:
                files = "file" if self._watching == 1 else "files"
                text = f"DOINK: watching {self._watching} {files}, nothing posted yet"
            if self._realtime["enabled"]:
                text += " · realtime"
        return text if len(text) <= TOOLTIP_MAX else text[:TOOLTIP_MAX - 1] + "…"
