"""What the companion is doing, for the tray tooltip and notifications.

Written by the worker thread, read by the tray thread.
"""

import threading
import time
from collections.abc import Callable

TOOLTIP_MAX = 127  # Windows tray tooltip limit (128 incl. terminator)


class Status:
    def __init__(self, on_change: Callable[[str | None], None] | None = None):
        # on_change(new_error): new_error is set only when a *new* problem
        # appears, so the tray notifies once rather than on every retry.
        self.on_change = on_change or (lambda new_error: None)
        self._lock = threading.Lock()
        self._watching = 0
        self._last_post: tuple[float, str] | None = None
        self._error: str | None = None

    def watching(self, count: int) -> None:
        with self._lock:
            self._watching = count
        self.on_change(None)

    def posted(self, who: str, what: str) -> None:
        with self._lock:
            self._last_post = (time.time(), f"{who}: {what}")
            self._error = None
        self.on_change(None)

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
        return text if len(text) <= TOOLTIP_MAX else text[:TOOLTIP_MAX - 1] + "…"
