"""Loads config.toml. Every setting is optional: with no file at all, the
SavedVariables path is discovered and the webhook comes from the game."""

import logging
import os
import re
import tomllib
from dataclasses import dataclass
from pathlib import Path

from .discord import is_webhook_url
from .discover import discover_savedvariables

log = logging.getLogger(__name__)

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
    savedvariables_paths: list[Path]
    state_path: Path
    webhook_url: str = ""  # fallback when the game hasn't set one
    dry_run: bool = False
    poll_interval: float = 2.0
    max_backlog: int = 10


def load_config(path: Path, dry_run: bool = False) -> Config:
    raw = {}
    if path.exists():
        with path.open("rb") as f:
            raw = tomllib.load(f)
    else:
        log.info("no %s; using defaults", path.name)

    if "notifiers" in raw:
        log.warning("[notifiers] in %s is ignored now. Toggle notifiers in game: "
                    "/doink enable|disable <type>", path.name)

    if raw.get("savedvariables_path"):
        paths = [native_path(raw["savedvariables_path"])]
    else:
        paths = discover_savedvariables()
        if not paths:
            raise SystemExit(
                "couldn't find World of Warcraft with the DOINK addon installed. "
                f"Set savedvariables_path in {path}")

    webhook_url = raw.get("webhook_url", "")
    if webhook_url and not is_webhook_url(webhook_url):
        raise SystemExit(f"{path}: webhook_url doesn't look like a Discord webhook URL")

    return Config(
        savedvariables_paths=paths,
        state_path=path.parent / "state.json",
        webhook_url=webhook_url,
        dry_run=dry_run or raw.get("dry_run", False),
        poll_interval=float(raw.get("poll_interval", 2.0)),
        max_backlog=int(raw.get("max_backlog", 10)),
    )
