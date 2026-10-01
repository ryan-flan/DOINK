"""Finds DOINK's SavedVariables files so config.toml can stay optional.

Looks in the usual World of Warcraft install folders on every drive, and in
every flavor folder (``_classic_beta_``, ...) that has the DOINK addon
installed, for each account under ``WTF\\Account``.
"""

import os
import string
from pathlib import Path

WOW_DIRS = (
    "Program Files (x86)/World of Warcraft",
    "Program Files/World of Warcraft",
    "World of Warcraft",
    "Games/World of Warcraft",
)


def drive_roots() -> list[Path]:
    if os.name == "nt":
        roots = (Path(f"{letter}:/") for letter in string.ascii_uppercase[2:])  # C: onward
        return [r for r in roots if r.exists()]
    mnt = Path("/mnt")  # WSL mounts Windows drives as /mnt/c, /mnt/d, ...
    if not mnt.is_dir():
        return []
    return [p for p in sorted(mnt.iterdir()) if len(p.name) == 1 and p.is_dir()]


def discover_savedvariables(roots: list[Path] | None = None) -> list[Path]:
    """``DOINK.lua`` paths, which may not exist yet (before the first /reload)."""
    found = []
    for root in drive_roots() if roots is None else roots:
        for rel in WOW_DIRS:
            wow = root / rel
            if not wow.is_dir():
                continue
            for flavor in sorted(wow.glob("_*_")):
                if not (flavor / "Interface" / "AddOns" / "DOINK").exists():
                    continue
                for sv_dir in sorted(flavor.glob("WTF/Account/*/SavedVariables")):
                    found.append(sv_dir / "DOINK.lua")
    return found
