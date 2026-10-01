"""DOINK companion: watches the SavedVariables file and posts new events to Discord."""

import argparse
import logging
import logging.handlers
import queue
import sys
import threading
from pathlib import Path

from doink.config import load_config
from doink.runner import run

log = logging.getLogger("doink")

LOG_FORMAT = "%(asctime)s %(levelname)-7s %(message)s"


def app_dir() -> Path:
    """Where config.toml, state.json and doink.log live: next to the .exe
    when frozen by PyInstaller, else next to main.py."""
    if getattr(sys, "frozen", False):
        return Path(sys.executable).resolve().parent
    return Path(__file__).resolve().parent


def run_tray(args) -> int:
    """The packaged Windows app: tray icon + settings window, logs to doink.log.

    Three threads: Tk owns the main thread (settings window), the tray runs
    its own Win32 message loop, and the watcher posts in the background.
    Tray commands reach Tk through a queue, since Tk isn't thread-safe.
    """
    from doink import tray, ui
    from doink.app import App
    from doink.status import Status

    log_path = app_dir() / "doink.log"
    handler = logging.handlers.RotatingFileHandler(
        log_path, maxBytes=1_000_000, backupCount=1, encoding="utf-8")
    handler.setFormatter(logging.Formatter(LOG_FORMAT, "%Y-%m-%d %H:%M:%S"))
    logging.basicConfig(level=logging.DEBUG if args.verbose else logging.INFO,
                        handlers=[handler])

    ui.enable_dpi_awareness()
    if not tray.acquire_single_instance():
        tray.message_box("DOINK is already running. Look for its icon in the "
                         "system tray (you may need to click ^ to see it).")
        return 0

    icon = None
    status = Status(on_change=lambda new_error: icon and icon.notify(new_error))
    try:
        app = App(args.config, status, dry_run=args.dry_run)
    except SystemExit as e:  # e.g. a hand-edited config.toml that doesn't parse
        log.error("%s", e)
        tray.message_box(str(e), error=True)
        return 1

    commands: queue.Queue[str] = queue.Queue()
    icon = tray.Tray(status, log_path, app_dir(), command=commands.put)
    threading.Thread(target=icon.run, name="tray", daemon=True).start()
    log.info("DOINK companion started")
    app.start()

    import tkinter as tk
    root = tk.Tk()
    try:
        root.iconbitmap(default=str(tray.icon_path()))
    except tk.TclError:
        pass
    window = ui.SettingsWindow(root, app, tray.asset("doink-48.png"),
                               open_log=lambda: tray.Tray.open(log_path),
                               open_folder=lambda: tray.Tray.open(app_dir()))
    if app.needs_setup():
        window.show()  # first run: say hello rather than sit silently in the tray

    def poll_commands():
        while True:
            try:
                command = commands.get_nowait()
            except queue.Empty:
                break
            if command == "settings":
                window.show()
            elif command == "quit":
                root.quit()
                return
        root.after(150, poll_commands)

    poll_commands()
    root.mainloop()

    app.stop()
    icon.quit()
    log.info("DOINK companion stopped")
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", type=Path, default=app_dir() / "config.toml")
    parser.add_argument("--dry-run", action="store_true",
                        help="log embeds instead of posting; don't save state")
    parser.add_argument("--once", action="store_true",
                        help="process the file once and exit")
    parser.add_argument("--tray", action="store_true",
                        help="run as a Windows tray app (default for doink.exe)")
    parser.add_argument("-v", "--verbose", action="store_true")
    args = parser.parse_args()

    frozen_windows = sys.platform == "win32" and getattr(sys, "frozen", False)
    if (args.tray or frozen_windows) and not args.once:
        return run_tray(args)

    logging.basicConfig(level=logging.DEBUG if args.verbose else logging.INFO,
                        format=LOG_FORMAT, datefmt="%H:%M:%S")
    config = load_config(args.config, dry_run=args.dry_run)
    try:
        return run(config, once=args.once)
    except KeyboardInterrupt:
        log.info("stopped")
        return 0


if __name__ == "__main__":
    sys.exit(main())
