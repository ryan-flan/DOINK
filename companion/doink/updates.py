"""Checks GitHub Releases for a newer companion.

One GET to ``https://api.github.com/repos/ryan-flan/DOINK/releases/latest``
30 s after start-up and then once a day (``update_check = false`` in
config.toml turns it off; the tray menu and settings window can still ask
by hand). Nothing but the user agent is sent, nothing is downloaded or
installed: a newer version is shown in the settings window and the tray,
with a link to the releases page, and that is all.
"""

import json
import logging
import re
import threading
import time
import urllib.error
import urllib.request
from collections.abc import Callable
from dataclasses import dataclass

from . import __version__

log = logging.getLogger(__name__)

LATEST_URL = "https://api.github.com/repos/ryan-flan/DOINK/releases/latest"
RELEASES_URL = "https://github.com/ryan-flan/DOINK/releases"
USER_AGENT = f"DOINK-companion/{__version__} (+https://github.com/ryan-flan/DOINK)"
TIMEOUT = 10.0
FIRST_CHECK_DELAY = 30.0        # seconds after start-up
CHECK_INTERVAL = 24 * 3600.0    # then daily
RETRY_INTERVAL = 3600.0         # after a failed check


@dataclass
class UpdateInfo:
    current: str
    latest: str
    url: str
    checked_at: float

    @property
    def newer(self) -> bool:
        return is_newer(self.latest, self.current)


def parse_version(text: str) -> tuple[int, ...] | None:
    """'v0.10.0' -> (0, 10, 0); None for anything that isn't dotted numbers.
    A pre-release suffix ('v1.0.0-beta.1') is ignored."""
    match = re.fullmatch(r"v?(\d+(?:\.\d+)*)(?:[-+].*)?", text.strip())
    if not match:
        return None
    return tuple(int(part) for part in match.group(1).split("."))


def is_newer(latest: str, current: str) -> bool:
    a, b = parse_version(latest), parse_version(current)
    if a is None or b is None:
        return False
    width = max(len(a), len(b))
    return a + (0,) * (width - len(a)) > b + (0,) * (width - len(b))


def fetch_latest(opener=urllib.request.urlopen, current: str = __version__) -> UpdateInfo | None:
    """The latest release on GitHub, or None if it couldn't be fetched."""
    request = urllib.request.Request(LATEST_URL, headers={
        "User-Agent": USER_AGENT,
        "Accept": "application/vnd.github+json",
    })
    try:
        with opener(request, timeout=TIMEOUT) as response:
            data = json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        log.info("update check: GitHub answered HTTP %s", e.code)
        return None
    except (urllib.error.URLError, OSError, ValueError) as e:
        log.info("update check failed: %s", e)
        return None
    tag = data.get("tag_name") if isinstance(data, dict) else None
    if not isinstance(tag, str) or parse_version(tag) is None:
        log.info("update check: unexpected answer from GitHub")
        return None
    url = data.get("html_url") if isinstance(data.get("html_url"), str) else RELEASES_URL
    if not url.startswith("https://github.com/"):
        url = RELEASES_URL
    return UpdateInfo(current=current, latest=tag.lstrip("v"), url=url, checked_at=time.time())


class UpdateChecker:
    """Background thread: first check after a delay, then daily. ``on_result``
    gets every successful check's UpdateInfo (the Status stores it)."""

    def __init__(self, on_result: Callable[[UpdateInfo], None], fetch=fetch_latest,
                 first_delay: float = FIRST_CHECK_DELAY, interval: float = CHECK_INTERVAL):
        self._on_result = on_result
        self._fetch = fetch
        self._first_delay = first_delay
        self._interval = interval
        self._stop = threading.Event()
        self._wake = threading.Event()
        self._thread: threading.Thread | None = None

    def start(self) -> None:
        self._thread = threading.Thread(target=self._run, name="update-check", daemon=True)
        self._thread.start()

    def stop(self) -> None:
        self._stop.set()
        self._wake.set()
        if self._thread:
            self._thread.join(timeout=5)

    def check_now(self) -> UpdateInfo | None:
        """A check on the calling thread (the UI runs this off-thread itself)."""
        info = self._fetch()
        if info is not None:
            log.info("update check: latest is v%s, this is v%s%s", info.latest, info.current,
                     " (update available)" if info.newer else "")
            self._on_result(info)
        return info

    def _run(self) -> None:
        delay = self._first_delay
        while not self._stop.is_set():
            self._wake.wait(delay)
            self._wake.clear()
            if self._stop.is_set():
                return
            info = self.check_now()
            delay = self._interval if info is not None else min(self._interval, RETRY_INTERVAL)
