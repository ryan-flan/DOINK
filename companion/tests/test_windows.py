"""Windows-only: exercises the real Win32 / registry calls. Runs on the
windows-latest CI jobs; skipped elsewhere."""

import sys
import tempfile
import threading
import time
import unittest
import uuid
from pathlib import Path

from doink.status import Status

WINDOWS = sys.platform == "win32"


@unittest.skipUnless(WINDOWS, "Windows only")
class AutostartTest(unittest.TestCase):
    NAME = f"DOINK-test-{uuid.uuid4().hex[:8]}"  # never touches the real entry
    CMD = r'"C:\DOINK\doink.exe"'

    def tearDown(self):
        from doink import autostart
        autostart.set_enabled(False, name=self.NAME)

    def test_round_trip(self):
        from doink import autostart
        self.assertFalse(autostart.is_enabled(self.NAME, self.CMD))
        autostart.set_enabled(True, name=self.NAME, cmd=self.CMD)
        self.assertTrue(autostart.is_enabled(self.NAME, self.CMD))
        self.assertFalse(autostart.is_enabled(self.NAME, r'"D:\moved\doink.exe"'),
                         "an entry for another path counts as off")
        autostart.set_enabled(False, name=self.NAME)
        autostart.set_enabled(False, name=self.NAME)  # idempotent
        self.assertFalse(autostart.is_enabled(self.NAME, self.CMD))


@unittest.skipUnless(WINDOWS, "Windows only")
class TrayTest(unittest.TestCase):
    def test_single_instance(self):
        from doink import tray
        name = f"Local\\DOINK-test-{uuid.uuid4().hex}"
        self.assertTrue(tray.acquire_single_instance(name))
        self.assertFalse(tray.acquire_single_instance(name))

    def test_starts_updates_and_quits(self):
        from doink import tray
        folder = Path(tempfile.mkdtemp())
        status = Status()
        icon = tray.Tray(status, folder / "doink.log", folder, on_quit=lambda: None)
        status.on_change = icon.notify

        thread = threading.Thread(target=icon.run, daemon=True)
        thread.start()
        for _ in range(50):
            if icon.hwnd:
                break
            time.sleep(0.1)
        self.assertTrue(icon.hwnd, "tray window was created")

        status.watching(1)
        status.failed("test notification")  # balloon path
        status.posted("Paul Hebbs", "level_up")
        time.sleep(0.5)

        icon.quit()
        thread.join(timeout=5)
        self.assertFalse(thread.is_alive(), "message loop exited on quit")

    def test_icon_file_ships(self):
        from doink import tray
        self.assertTrue(tray.icon_path().is_file(), tray.icon_path())


if __name__ == "__main__":
    unittest.main()
