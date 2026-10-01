"""Last-posted seq per character, persisted so restarts never re-post."""

import json
import logging
import os
from pathlib import Path

log = logging.getLogger(__name__)


class State:
    def __init__(self, path: Path, persist: bool = True):
        self.path = path
        self.persist = persist  # False in dry-run: track in memory only
        self._chars: dict[str, dict] = {}
        if path.exists():
            try:
                self._chars = json.loads(path.read_text(encoding="utf-8"))
            except (OSError, json.JSONDecodeError) as e:
                # Refuse to guess: starting empty could re-post a backlog.
                raise SystemExit(f"can't read state file {path}: {e}")

    def last_seen(self, key: str) -> int | None:
        entry = self._chars.get(key)
        return entry["last_seen"] if entry else None

    def set_last_seen(self, key: str, seq: int) -> None:
        self._chars[key] = {"last_seen": seq}
        self.save()

    def save(self) -> None:
        if not self.persist:
            return
        # Write-then-rename so a crash mid-write can't corrupt the file.
        tmp = self.path.with_suffix(".tmp")
        tmp.write_text(json.dumps(self._chars, indent=2), encoding="utf-8")
        os.replace(tmp, self.path)
