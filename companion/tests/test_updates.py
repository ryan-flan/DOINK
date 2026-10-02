import hashlib
import io
import json
import sys
import tempfile
import threading
import unittest
import urllib.error
import zipfile
from pathlib import Path

from doink import updates
from doink.status import Status
from doink.updates import (UpdateChecker, UpdateInfo, Updater, fetch_latest, is_newer,
                           parse_version)


class FakeResponse(io.BytesIO):
    def __enter__(self):
        return self

    def __exit__(self, *exc):
        self.close()


DL = updates.DOWNLOAD_PREFIX


def github(tag, url="https://github.com/ryan-flan/DOINK/releases/tag/" + "v9.9.9", assets=True):
    def opener(request, timeout):
        assert request.full_url == updates.LATEST_URL
        assert "DOINK-companion" in request.get_header("User-agent")
        payload = {"tag_name": tag, "html_url": url}
        if assets:
            payload["assets"] = [
                {"name": f"DOINK-{tag}.zip", "browser_download_url": f"{DL}{tag}/DOINK-{tag}.zip"},
                {"name": f"DOINK-{tag}.zip.sha256",
                 "browser_download_url": f"{DL}{tag}/DOINK-{tag}.zip.sha256"},
                {"name": "evil.zip", "browser_download_url": "https://evil.example/x.zip"},
            ]
        return FakeResponse(json.dumps(payload).encode())
    return opener


class VersionTest(unittest.TestCase):
    def test_parse(self):
        self.assertEqual(parse_version("v0.10.0"), (0, 10, 0))
        self.assertEqual(parse_version("1.2"), (1, 2))
        self.assertEqual(parse_version("v1.0.0-beta.1"), (1, 0, 0))
        self.assertIsNone(parse_version("latest"))
        self.assertIsNone(parse_version(""))

    def test_newer(self):
        self.assertTrue(is_newer("v0.10.0", "0.9.5"), "numeric, not lexical")
        self.assertTrue(is_newer("0.10.1", "0.10.0"))
        self.assertFalse(is_newer("0.10.0", "0.10.0"))
        self.assertFalse(is_newer("0.9.9", "0.10.0"))
        self.assertTrue(is_newer("1.0", "0.10.0"))
        self.assertFalse(is_newer("0.10", "0.10.0"), "missing parts count as zero")
        self.assertFalse(is_newer("garbage", "0.10.0"))


class FetchTest(unittest.TestCase):
    def test_newer_release(self):
        info = fetch_latest(opener=github("v9.9.9"), current="0.10.0")
        self.assertEqual((info.latest, info.current), ("9.9.9", "0.10.0"))
        self.assertTrue(info.newer)
        self.assertTrue(info.url.startswith("https://github.com/ryan-flan/DOINK/releases"))
        self.assertEqual(info.zip_url, f"{DL}v9.9.9/DOINK-v9.9.9.zip")
        self.assertEqual(info.sha256_url, f"{DL}v9.9.9/DOINK-v9.9.9.zip.sha256")
        self.assertTrue(info.installable)
        without = fetch_latest(opener=github("v9.9.9", assets=False), current="0.10.0")
        self.assertTrue(without.newer)
        self.assertFalse(without.installable, "no zip asset: link only")

    def test_same_release_and_bad_url(self):
        info = fetch_latest(opener=github("v0.10.0", url="http://evil.example/x"), current="0.10.0")
        self.assertFalse(info.newer)
        self.assertEqual(info.url, updates.RELEASES_URL, "only GitHub links are opened")

    def test_failures_return_none(self):
        def http_error(request, timeout):
            raise urllib.error.HTTPError(request.full_url, 403, "rate limited", {}, None)

        def network(request, timeout):
            raise urllib.error.URLError("offline")

        def garbage(request, timeout):
            return FakeResponse(b"<html>")

        def odd_tag(request, timeout):
            return FakeResponse(b'{"tag_name": "latest"}')

        for opener in (http_error, network, garbage, odd_tag):
            self.assertIsNone(fetch_latest(opener=opener))


class CheckerAndStatusTest(unittest.TestCase):
    def test_status_records_and_notifies_only_when_asked(self):
        notices = []
        status = Status()
        status.on_notice = notices.append
        same = UpdateInfo("0.10.0", "0.10.0", updates.RELEASES_URL, 1.0)
        status.update_checked(same)
        self.assertEqual(notices, [])
        self.assertNotIn("available", status.summary())

        newer = UpdateInfo("0.10.0", "0.11.0", updates.RELEASES_URL, 2.0)
        status.update_checked(newer)
        self.assertEqual(notices, [], "notifications are off by default; the badge shows it")
        self.assertIn("v0.11.0 available", status.summary())
        self.assertIs(status.update(), newer)

        status.notify_updates = True
        status.update_checked(newer)
        status.update_checked(newer)  # the daily re-check
        self.assertEqual(len(notices), 1)
        self.assertIn("v0.11.0", notices[0])
        status.update_checked(UpdateInfo("0.10.0", "0.12.0", updates.RELEASES_URL, 3.0))
        self.assertEqual(len(notices), 2, "a further version is announced again")

    def test_checker_runs_after_the_delay_and_stops(self):
        results = []
        ran = threading.Event()

        def fetch():
            ran.set()
            return UpdateInfo("0.10.0", "0.10.0", updates.RELEASES_URL, 0.0)

        checker = UpdateChecker(results.append, fetch=fetch, first_delay=0.05, interval=60)
        checker.start()
        self.assertTrue(ran.wait(2), "first check happened after the delay")
        checker.stop()
        self.assertEqual(len(results), 1)

    def test_check_now_reports_failure(self):
        checker = UpdateChecker(lambda info: None, fetch=lambda: None)
        self.assertIsNone(checker.check_now())


def release_zip(tag: str, files: dict[str, bytes]) -> bytes:
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w") as zf:
        zf.writestr(f"DOINK-{tag}/README.md", "readme")
        zf.writestr(f"DOINK-{tag}/AddOns/DOINK/DOINK.toc", "## Interface: 16001")
        for name, data in files.items():
            zf.writestr(f"DOINK-{tag}/Companion/{name}", data)
    return buf.getvalue()


class UpdaterTest(unittest.TestCase):
    def setUp(self):
        self.app_dir = Path(tempfile.mkdtemp())
        (self.app_dir / "config.toml").write_text("webhook_url = \"x\"\n")
        (self.app_dir / "doink.exe").write_bytes(b"old exe")
        (self.app_dir / "_internal").mkdir()
        (self.app_dir / "_internal" / "old.dll").write_bytes(b"old")
        self.tag = "v9.9.9"
        self.zip = release_zip(self.tag, {"doink.exe": b"new exe", "_internal/new.dll": b"new",
                                           "config.example.toml": b"# example"})
        self.sha = hashlib.sha256(self.zip).hexdigest()
        self.info = UpdateInfo("0.10.0", "9.9.9", updates.RELEASES_URL, 0.0,
                               zip_url=f"{DL}{self.tag}/DOINK-{self.tag}.zip",
                               sha256_url=f"{DL}{self.tag}/DOINK-{self.tag}.zip.sha256")

    def opener(self, sha=None):
        sha = self.sha if sha is None else sha

        def open_(request, timeout):
            url = request.full_url
            if url.endswith(".sha256"):
                return FakeResponse(f"{sha}  DOINK-{self.tag}.zip".encode())
            if url.endswith(".zip"):
                return FakeResponse(self.zip)
            raise AssertionError(url)
        return open_

    def test_download_verify_and_stage(self):
        progress = []
        updater = Updater(self.app_dir, opener=self.opener(), pid=4242)
        companion = updater.download(self.info, progress.append)
        self.assertEqual(companion, self.app_dir / "update" / "Companion")
        self.assertEqual((companion / "doink.exe").read_bytes(), b"new exe")
        self.assertEqual((companion / "_internal" / "new.dll").read_bytes(), b"new")
        self.assertFalse((self.app_dir / "update" / "release.zip").exists(), "zip removed after unpack")
        self.assertFalse(any(p.name == "README.md" for p in companion.rglob("*")),
                         "only the Companion folder is staged")
        self.assertTrue(any("Verified" in p for p in progress))
        # The running files are untouched until the script runs.
        self.assertEqual((self.app_dir / "doink.exe").read_bytes(), b"old exe")

        script = updater.script(companion)
        self.assertIn('tasklist /FI "PID eq 4242"', script)
        self.assertIn(f'rmdir /s /q "{self.app_dir / "_internal"}"', script)
        self.assertIn(f'xcopy "{companion}" "{self.app_dir}" /E /I /Y /Q', script)
        self.assertIn(f'start "" "{self.app_dir / "doink.exe"}"', script)
        self.assertNotIn("config.toml", script)
        self.assertNotIn("state.json", script)

    def test_bad_checksum_refuses(self):
        updater = Updater(self.app_dir, opener=self.opener(sha="0" * 64))
        with self.assertRaises(updates.UpdateError) as ctx:
            updater.download(self.info)
        self.assertIn("checksum", str(ctx.exception))

    def test_only_github_downloads(self):
        bad = UpdateInfo("0.10.0", "9.9.9", updates.RELEASES_URL, 0.0,
                         zip_url="https://evil.example/DOINK.zip", sha256_url=self.info.sha256_url)
        with self.assertRaises(updates.UpdateError):
            Updater(self.app_dir, opener=self.opener()).download(bad)

    def test_zip_without_exe_or_with_escaping_paths(self):
        self.zip = release_zip(self.tag, {"readme.txt": b"nope"})
        self.sha = hashlib.sha256(self.zip).hexdigest()
        with self.assertRaises(updates.UpdateError):
            Updater(self.app_dir, opener=self.opener()).download(self.info)

        buf = io.BytesIO()
        with zipfile.ZipFile(buf, "w") as zf:
            zf.writestr(f"DOINK-{self.tag}/Companion/../../escape.exe", b"x")
            zf.writestr(f"DOINK-{self.tag}/Companion/doink.exe", b"x")
        self.zip = buf.getvalue()
        self.sha = hashlib.sha256(self.zip).hexdigest()
        with self.assertRaises(updates.UpdateError):
            Updater(self.app_dir, opener=self.opener()).download(self.info)

    @unittest.skipUnless(sys.platform == "win32", "the hand-off script is cmd.exe")
    def test_apply_launches_the_script(self):
        launched = []
        updater = Updater(self.app_dir, opener=self.opener(), launch=lambda *a, **k: launched.append((a, k)))
        companion = updater.download(self.info)
        updater.apply(companion)
        (args,), kwargs = launched[0]
        self.assertEqual(args[:2], ["cmd.exe", "/c"])
        self.assertTrue(Path(args[2]).read_text().startswith("@echo off"))


if __name__ == "__main__":
    unittest.main()
