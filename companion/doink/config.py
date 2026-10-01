"""Loads and validates config.toml."""

import os
import re
import tomllib
from dataclasses import dataclass, field
from pathlib import Path

WEBHOOK_URL = re.compile(r"^https://(discord|discordapp)\.com/api/webhooks/\d+/[\w-]+")
WINDOWS_PATH = re.compile(r"^([A-Za-z]):[\\/](.*)$")


def native_path(raw: str) -> Path:
    """Accept Windows paths everywhere: under WSL, ``C:\\x`` becomes
    ``/mnt/c/x``. One config then works from WSL and from Windows."""
    match = WINDOWS_PATH.match(raw)
    if match and os.name == "posix":
        drive, rest = match.groups()
        return Path("/mnt", drive.lower(), *re.split(r"[\\/]+", rest))
    return Path(raw)


@dataclass
class Config:
    savedvariables_path: Path
    webhook_url: str
    state_path: Path
    dry_run: bool = False
    poll_interval: float = 2.0
    max_backlog: int = 10
    notifiers: dict[str, bool] = field(default_factory=dict)

    def enabled(self, event_type: str) -> bool:
        return self.notifiers.get(event_type, True)


def load_config(path: Path, dry_run: bool = False) -> Config:
    if not path.exists():
        raise SystemExit(f"{path} not found. Copy config.example.toml to config.toml and edit it.")
    with path.open("rb") as f:
        raw = tomllib.load(f)

    try:
        config = Config(
            savedvariables_path=native_path(raw["savedvariables_path"]),
            webhook_url=raw.get("webhook_url", ""),
            state_path=path.parent / "state.json",
            dry_run=dry_run or raw.get("dry_run", False),
            poll_interval=float(raw.get("poll_interval", 2.0)),
            max_backlog=int(raw.get("max_backlog", 10)),
            notifiers=dict(raw.get("notifiers", {})),
        )
    except KeyError as e:
        raise SystemExit(f"{path}: missing required setting {e}")

    if not config.dry_run and not WEBHOOK_URL.match(config.webhook_url):
        raise SystemExit(f"{path}: webhook_url doesn't look like a Discord webhook URL")
    return config
