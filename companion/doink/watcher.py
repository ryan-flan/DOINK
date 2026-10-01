"""Polls the SavedVariables file for changes.

Deliberately mtime polling, not OS file-watch hooks: simple, and reliable
across filesystems.
"""

import logging
from pathlib import Path

log = logging.getLogger(__name__)


class Watcher:
    """Reports a change once the file has changed *and then held still for
    one poll*, so we don't read it while WoW is halfway through writing it."""

    def __init__(self, path: Path):
        self.path = path
        self._seen = None     # signature we last reported
        self._pending = None  # signature waiting to settle
        self._missing_logged = False

    def poll(self) -> bool:
        try:
            st = self.path.stat()
        except FileNotFoundError:
            if not self._missing_logged:
                log.warning("waiting for %s to exist (log in and /reload once)", self.path)
                self._missing_logged = True
            return False
        self._missing_logged = False

        signature = (st.st_mtime_ns, st.st_size)
        if signature == self._seen:
            return False
        if signature != self._pending:
            self._pending = signature
            return False
        self._seen = signature
        return True
