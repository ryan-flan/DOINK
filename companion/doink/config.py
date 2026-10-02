"""Loads and saves config.toml. Every setting is optional: with no file at
all, the SavedVariables path is discovered and the webhook comes from the
game. The settings window writes the file too (save_settings)."""

import json
import logging
import os
import re
import tomllib
from dataclasses import dataclass
from pathlib import Path

from .discord import is_webhook_url
from .discover import discover_in_wow_dir, discover_savedvariables

log = logging.getLogger(__name__)

WINDOWS_PATH = re.compile(r"^([A-Za-z]):[\\/](.*)$")
HEADER = ("# DOINK companion settings. The settings window rewrites this file;\n"
          "# see config.example.toml for every option.\n")


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
    realtime: bool = False  # experimental screen reader; opt-in
    wowhead: bool = True    # look up looted items on Wowhead for icon + stats


def _read(path: Path) -> dict:
    if not path.exists():
        return {}
    with path.open("rb") as f:
        return tomllib.load(f)


def load_config(path: Path, dry_run: bool = False, require_paths: bool = True) -> Config:
    raw = _read(path)
    if not raw:
        log.info("no settings in %s; using defaults", path.name)

    if "notifiers" in raw:
        log.warning("[notifiers] in %s is ignored now. Toggle notifiers in game: "
                    "/doink enable|disable <type>", path.name)

    if raw.get("savedvariables_path"):
        paths = [native_path(raw["savedvariables_path"])]
    elif raw.get("wow_dir"):
        paths = discover_in_wow_dir(native_path(raw["wow_dir"]))
    else:
        paths = discover_savedvariables()
    if not paths and require_paths:
        raise SystemExit(
            "couldn't find World of Warcraft with the DOINK addon installed. "
            f"Set wow_dir or savedvariables_path in {path}")

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
        realtime=bool(raw.get("realtime", False)),
        wowhead=bool(raw.get("wowhead", True)),
    )


def _toml_value(value) -> str:
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, (int, float)):
        return repr(value)
    if isinstance(value, str):
        return json.dumps(value, ensure_ascii=False)  # a valid TOML basic string
    raise TypeError(f"can't write {type(value).__name__} to config.toml")


def save_settings(path: Path, updates: dict) -> None:
    """Merge ``updates`` into config.toml; a value of None or "" removes the
    key. Comments aren't preserved; tables (the obsolete [notifiers]) are
    dropped."""
    raw = _read(path)
    for key, value in updates.items():
        if value is None or value == "":
            raw.pop(key, None)
        else:
            raw[key] = value
    lines = [f"{key} = {_toml_value(value)}" for key, value in raw.items()
             if not isinstance(value, dict)]
    tmp = path.with_suffix(".tmp")
    tmp.write_text(HEADER + "\n" + "\n".join(lines) + "\n", encoding="utf-8")
    os.replace(tmp, path)
