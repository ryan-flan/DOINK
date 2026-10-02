import io
import json
import threading
import unittest
import urllib.error

from doink import updates
from doink.status import Status
from doink.updates import UpdateChecker, UpdateInfo, fetch_latest, is_newer, parse_version


class FakeResponse(io.BytesIO):
    def __enter__(self):
        return self

    def __exit__(self, *exc):
        self.close()


def github(tag, url="https://github.com/ryan-flan/DOINK/releases/tag/" + "v9.9.9"):
    def opener(request, timeout):
        assert request.full_url == updates.LATEST_URL
        assert "DOINK-companion" in request.get_header("User-agent")
        return FakeResponse(json.dumps({"tag_name": tag, "html_url": url}).encode())
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
    def test_status_records_and_notifies_once_per_version(self):
        notices = []
        status = Status()
        status.on_notice = notices.append
        same = UpdateInfo("0.10.0", "0.10.0", updates.RELEASES_URL, 1.0)
        status.update_checked(same)
        self.assertEqual(notices, [])
        self.assertNotIn("available", status.summary())

        newer = UpdateInfo("0.10.0", "0.11.0", updates.RELEASES_URL, 2.0)
        status.update_checked(newer)
        status.update_checked(newer)  # the daily re-check
        self.assertEqual(len(notices), 1)
        self.assertIn("v0.11.0", notices[0])
        self.assertIn("v0.11.0 available", status.summary())
        self.assertIs(status.update(), newer)

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


if __name__ == "__main__":
    unittest.main()
