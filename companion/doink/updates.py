"""Checks GitHub Releases for a newer companion, and installs one on request.

One GET to ``https://api.github.com/repos/ryan-flan/DOINK/releases/latest``
30 s after start-up and then once a day (``update_check = false`` in
config.toml turns it off; the tray menu and settings window can still ask
by hand). Nothing but the user agent is sent and nothing is downloaded by
itself: a newer version puts an orange badge on the tray icon and an
"Update to vX" entry in its menu.

Choosing that runs ``Updater``: it downloads ``DOINK-vX.zip`` and its
``.sha256`` from the release (github.com only), verifies the hash, unpacks
the ``Companion`` folder into ``update/`` next to the exe, writes a small
batch script that waits for this process to exit, swaps ``doink.exe`` and
``_internal`` over, deletes the staging folder and starts the new exe, and
then the companion quits. ``config.toml``, ``state.json`` and ``doink.log``
are never touched. A running exe can't be overwritten on Windows, hence
the hand-off.
"""

import hashlib
import json
import logging
import os
import re
import shutil
import subprocess
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.request
import zipfile
from collections.abc import Callable
from dataclasses import dataclass
from pathlib import Path

from . import __version__

log = logging.getLogger(__name__)

LATEST_URL = "https://api.github.com/repos/ryan-flan/DOINK/releases/latest"
RELEASES_URL = "https://github.com/ryan-flan/DOINK/releases"
DOWNLOAD_PREFIX = "https://github.com/ryan-flan/DOINK/releases/download/"
USER_AGENT = f"DOINK-companion/{__version__} (+https://github.com/ryan-flan/DOINK)"
TIMEOUT = 10.0
FIRST_CHECK_DELAY = 30.0        # seconds after start-up
CHECK_INTERVAL = 24 * 3600.0    # then daily
RETRY_INTERVAL = 3600.0         # after a failed check
MAX_ZIP_BYTES = 200 * 1024 * 1024


@dataclass
class UpdateInfo:
    current: str
    latest: str
    url: str
    checked_at: float
    zip_url: str | None = None     # the release's DOINK-vX.zip, on github.com
    sha256_url: str | None = None  # its .sha256

    @property
    def newer(self) -> bool:
        return is_newer(self.latest, self.current)

    @property
    def installable(self) -> bool:
        return self.newer and bool(self.zip_url and self.sha256_url)


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
    zip_url = sha_url = None
    for asset in data.get("assets") or []:
        if not isinstance(asset, dict):
            continue
        name, link = asset.get("name"), asset.get("browser_download_url")
        if not (isinstance(name, str) and isinstance(link, str) and link.startswith(DOWNLOAD_PREFIX)):
            continue
        if name == f"DOINK-{tag}.zip":
            zip_url = link
        elif name == f"DOINK-{tag}.zip.sha256":
            sha_url = link
    return UpdateInfo(current=current, latest=tag.lstrip("v"), url=url, checked_at=time.time(),
                      zip_url=zip_url, sha256_url=sha_url)


# ------------------------------------------------------------------ install

class UpdateError(Exception):
    """Shown to the user as-is."""


class Updater:
    """Downloads, verifies and stages a release, then hands the file swap to
    a script that runs after this process exits."""

    def __init__(self, app_dir: Path, opener=urllib.request.urlopen,
                 launch=subprocess.Popen, pid: int | None = None):
        self.app_dir = Path(app_dir)
        self.stage = self.app_dir / "update"
        self._opener = opener
        self._launch = launch
        self._pid = os.getpid() if pid is None else pid

    def _get(self, url: str, progress: Callable[[str], None], what: str) -> bytes:
        if not url.startswith(DOWNLOAD_PREFIX):
            raise UpdateError("Refusing to download from anywhere but GitHub.")
        request = urllib.request.Request(url, headers={"User-Agent": USER_AGENT})
        chunks, got = [], 0
        try:
            with self._opener(request, timeout=30) as response:
                while True:
                    chunk = response.read(256 * 1024)
                    if not chunk:
                        break
                    chunks.append(chunk)
                    got += len(chunk)
                    if got > MAX_ZIP_BYTES:
                        raise UpdateError("The download is far larger than a DOINK release.")
                    progress(f"Downloading {what}… {got / 1048576:.1f} MB")
        except urllib.error.URLError as e:
            raise UpdateError(f"Download failed: {e.reason if hasattr(e, 'reason') else e}") from None
        except OSError as e:
            raise UpdateError(f"Download failed: {e}") from None
        return b"".join(chunks)

    def download(self, info: UpdateInfo, progress: Callable[[str], None] = lambda s: None) -> Path:
        """Fetch and verify the release; returns the staged Companion folder."""
        if not info.installable:
            raise UpdateError("This release has no companion download to install.")
        if self.stage.exists():
            shutil.rmtree(self.stage, ignore_errors=True)
        self.stage.mkdir(parents=True)

        data = self._get(info.zip_url, progress, f"v{info.latest}")
        expected = self._get(info.sha256_url, progress, "checksum").decode("utf-8", "replace")
        expected = expected.strip().split()[0].lower() if expected.strip() else ""
        actual = hashlib.sha256(data).hexdigest()
        if not re.fullmatch(r"[0-9a-f]{64}", expected) or actual != expected:
            raise UpdateError("The download's checksum doesn't match the one published with "
                              "the release, so it wasn't installed.")
        progress("Verified. Unpacking…")

        zip_path = self.stage / "release.zip"
        zip_path.write_bytes(data)
        companion = self.stage / "Companion"
        companion.mkdir()
        count = 0
        with zipfile.ZipFile(zip_path) as zf:
            for member in zf.infolist():
                parts = Path(member.filename).parts
                # DOINK-vX/Companion/<...>: only that folder, and nothing that
                # could escape it.
                if len(parts) < 3 or parts[1] != "Companion" or member.is_dir():
                    continue
                rel = Path(*parts[2:])
                if rel.is_absolute() or ".." in rel.parts:
                    raise UpdateError("The release zip contains an unexpected path.")
                target = companion / rel
                target.parent.mkdir(parents=True, exist_ok=True)
                with zf.open(member) as src, open(target, "wb") as dst:
                    shutil.copyfileobj(src, dst)
                count += 1
        zip_path.unlink()
        if not (companion / "doink.exe").is_file():
            raise UpdateError("The release zip has no doink.exe in it.")
        log.info("update: staged v%s (%d files) in %s", info.latest, count, companion)
        return companion

    def script(self, companion: Path, restart: bool = True) -> str:
        """The batch script that swaps the files once this process is gone.
        It logs every step to ``update.log`` in the app folder. The sleep is
        a ``ping`` rather than ``timeout``: ``timeout`` refuses to run
        without console input, and the script gets none."""
        app, stage, pid = self.app_dir, self.stage, self._pid
        logfile = app / "update.log"
        lines = [
            "@echo off",
            "rem DOINK self-update: waits for the old companion to exit, swaps the files, restarts it.",
            f'echo [%date% %time%] update script started, waiting for PID {pid} >> "{logfile}"',
            ":wait",
            f'tasklist /FI "PID eq {pid}" 2>nul | find "{pid}" >nul',
            "if not errorlevel 1 (",
            "  ping -n 2 127.0.0.1 >nul",
            "  goto wait",
            ")",
            f'echo [%date% %time%] old companion gone >> "{logfile}"',
            f'if exist "{app / "_internal"}" rmdir /s /q "{app / "_internal"}"',
            f'echo [%date% %time%] removed _internal, errorlevel %errorlevel% >> "{logfile}"',
            f'xcopy "{companion}" "{app}" /E /I /Y /Q >nul',
            f'echo [%date% %time%] copied new files, errorlevel %errorlevel% >> "{logfile}"',
            f'rmdir /s /q "{stage}"',
        ]
        if restart:
            lines += [
                f'start "" "{app / "doink.exe"}"',
                f'echo [%date% %time%] started doink.exe, errorlevel %errorlevel% >> "{logfile}"',
            ]
        return "\r\n".join(lines + [""])

    def apply(self, companion: Path, restart: bool = True) -> None:
        """Write and launch the swap script. The caller quits right after."""
        if sys.platform != "win32":
            raise UpdateError("Installing updates only works for the Windows doink.exe build.")
        fd, path = tempfile.mkstemp(prefix="doink-update-", suffix=".cmd")
        with os.fdopen(fd, "w", encoding="utf-8", newline="") as f:
            f.write(self.script(companion, restart))
        # Its own hidden console (CREATE_NO_WINDOW), not DETACHED_PROCESS: a
        # windowless exe has no console to hand down, and a detached cmd.exe
        # with no console at all never ran the script (beta, 2026-10-03).
        self._launch(["cmd.exe", "/c", path], creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0),
                     stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                     close_fds=True, cwd=str(self.app_dir))
        log.info("update: hand-off script started (%s); exiting for the swap", path)


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
