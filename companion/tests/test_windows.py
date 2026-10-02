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
        icon = tray.Tray(status, folder / "doink.log", folder, command=lambda c: None)
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
        status.posted("Flano Wren", "level_up")
        time.sleep(0.5)

        icon.quit()
        thread.join(timeout=5)
        self.assertFalse(thread.is_alive(), "message loop exited on quit")

    def test_assets_ship(self):
        from doink import tray
        for name in ("doink.ico", "doink-update.ico", "doink-48.png"):
            self.assertTrue(tray.asset(name).is_file(), tray.asset(name))


@unittest.skipUnless(WINDOWS, "Windows only")
class CaptureTest(unittest.TestCase):
    """The GDI path against whatever is on the desktop: it must not leak and
    must not find a strip where there is none."""

    def desktop_capture(self):
        from doink import pixel
        user32 = pixel._user32
        return pixel.Capture(find_window=lambda: user32.GetDesktopWindow(),
                             require_foreground=False)

    def test_no_handle_or_memory_leak_over_many_captures(self):
        from doink import pixel
        capture = self.desktop_capture()
        try:
            for _ in range(20):  # warm up: first allocations
                capture.grab()
            gdi0, ws0 = pixel.gdi_handles(), pixel.working_set_mb()
            grabbed = 0
            for _ in range(1000):
                if capture.grab() is not None:
                    grabbed += 1
            gdi1, ws1 = pixel.gdi_handles(), pixel.working_set_mb()
        finally:
            capture.close()
        self.assertGreater(grabbed, 0, "desktop capture produced frames")
        self.assertLessEqual(abs(gdi1 - gdi0), 2, f"GDI handles drifted {gdi0}->{gdi1}")
        self.assertLess(ws1 - ws0, 5.0, f"working set grew {ws0:.1f}->{ws1:.1f} MB")

    def test_reader_degrades_without_wow(self):
        from doink import pixel
        reader = pixel.Reader(capture=pixel.Capture(find_window=lambda: None))
        try:
            for _ in range(5):
                self.assertEqual(reader.poll(), [])
            self.assertEqual(reader.window, "none")
            self.assertEqual(reader.meter.snapshot()["captures"], 5)
        finally:
            reader.close()

    def test_reader_on_desktop_finds_nothing_and_is_cheap(self):
        from doink import pixel
        reader = pixel.Reader(capture=self.desktop_capture())
        try:
            for _ in range(100):
                self.assertEqual(reader.poll(), [])
            snap = reader.meter.snapshot()
        finally:
            reader.close()
        self.assertEqual(snap["captures"], 100)
        self.assertLess(snap["avg_ms"], 200, "a capture+decode of two bands is far under this")


@unittest.skipUnless(WINDOWS, "Windows only")
class SettingsWindowTest(unittest.TestCase):
    def test_builds_shows_refreshes_and_hides(self):
        import tkinter as tk

        from doink import tray, ui
        from doink.app import App
        from doink.config import save_settings

        folder = Path(tempfile.mkdtemp())
        save_settings(folder / "config.toml", {"wow_dir": str(folder / "no-wow")})
        app = App(folder / "config.toml", Status())
        app.start()  # no WoW: the window must explain that, not crash
        app.status.posted("Flano Wren", "level_up")

        root = tk.Tk()
        try:
            window = ui.SettingsWindow(root, app, tray.asset("doink-48.png"),
                                       open_log=lambda: None, open_folder=lambda: None)
            window.show()
            root.update()
            self.assertIn("Level up", window.recent.cget("text"))
            self.assertIn("wasn't found", window.paths.cget("text"))
            window.webhook.set("nope")
            window._save_webhook()
            self.assertIn("isn't a Discord webhook", window.webhook_msg.cget("text"))
            self.assertIn("Reader: off", window.rt_reader.cget("text"))
            self.assertFalse(window.realtime_var.get(), "realtime is off by default")
            window.hide()
            root.update()
        finally:
            root.destroy()


if __name__ == "__main__":
    unittest.main()
