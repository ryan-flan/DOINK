"""What has been posted per character, persisted so restarts never re-post.

Per character: ``last_seen``, the highest seq posted, and ``missing``, seqs
below it that haven't been posted yet. Gaps appear when the realtime path
delivers an event the reader didn't catch the predecessors of; the
SavedVariables pass fills them in later.
"""

import json
import logging
import os
import threading
from pathlib import Path

log = logging.getLogger(__name__)

MISSING_MAX = 500  # the addon's ring buffer size; older gaps can't be filled


class State:
    def __init__(self, path: Path, persist: bool = True):
        self.path = path
        self.persist = persist  # False in dry-run: track in memory only
        self._lock = threading.RLock()
        self._chars: dict[str, dict] = {}
        if path.exists():
            try:
                self._chars = json.loads(path.read_text(encoding="utf-8"))
            except (OSError, json.JSONDecodeError) as e:
                # Refuse to guess: starting empty could re-post a backlog.
                raise SystemExit(f"can't read state file {path}: {e}")
        for entry in self._chars.values():
            entry.setdefault("missing", [])  # entries written before v0.6.0

    def last_seen(self, key: str) -> int | None:
        with self._lock:
            entry = self._chars.get(key)
            return entry["last_seen"] if entry else None

    def missing(self, key: str) -> set[int]:
        with self._lock:
            entry = self._chars.get(key)
            return set(entry["missing"]) if entry else set()

    def is_pending(self, key: str, seq: int) -> bool:
        """True if ``seq`` still needs posting."""
        with self._lock:
            entry = self._chars.get(key)
            if entry is None:
                return True
            return seq > entry["last_seen"] or seq in entry["missing"]

    def mark_posted(self, key: str, seq: int, first_sight_backlog: int = 0) -> None:
        """Record ``seq`` as posted. Seqs skipped over become ``missing``.

        For a character never seen before, ``first_sight_backlog`` older
        seqs are marked missing so the usual backlog rule applies when the
        SavedVariables file is next read."""
        with self._lock:
            entry = self._chars.get(key)
            if entry is None:
                low = max(1, seq - first_sight_backlog + 1) if first_sight_backlog else seq
                self._chars[key] = {"last_seen": seq, "missing": list(range(low, seq))}
            else:
                missing = set(entry["missing"])
                last = entry["last_seen"]
                if seq > last:
                    missing.update(range(last + 1, seq))
                    last = seq
                else:
                    missing.discard(seq)
                entry["last_seen"] = last
                entry["missing"] = sorted(missing)[-MISSING_MAX:]
            self.save()

    def baseline(self, key: str, seq: int) -> None:
        """Everything up to ``seq`` is accounted for (posted or deliberately
        skipped); no gaps."""
        with self._lock:
            entry = self._chars.get(key)
            last = max(seq, entry["last_seen"]) if entry else seq
            self._chars[key] = {"last_seen": last, "missing": []}
            self.save()

    def prune_missing(self, key: str, present: set[int], newest: int) -> None:
        """Forget gaps at or below ``newest`` that the file doesn't contain:
        they can never be filled."""
        with self._lock:
            entry = self._chars.get(key)
            if not entry:
                return
            keep = [s for s in entry["missing"] if s > newest or s in present]
            if keep != entry["missing"]:
                entry["missing"] = keep
                self.save()

    def forget(self, key: str) -> None:
        with self._lock:
            if self._chars.pop(key, None) is not None:
                self.save()

    def rename(self, old: str, new: str) -> bool:
        """Move ``old``'s entry to ``new`` if ``new`` has none. Returns True if moved."""
        with self._lock:
            if old not in self._chars or new in self._chars:
                return False
            self._chars[new] = self._chars.pop(old)
            self.save()
            return True

    def save(self) -> None:
        if not self.persist:
            return
        with self._lock:
            # Write-then-rename so a crash mid-write can't corrupt the file.
            tmp = self.path.with_suffix(".tmp")
            tmp.write_text(json.dumps(self._chars, indent=2), encoding="utf-8")
            os.replace(tmp, self.path)
