"""The settings window: tkinter (part of Python), so the companion still has
no third-party dependencies. Styled dark to match the icon.

Runs on the main thread; the tray and the watcher live in other threads and
never touch Tk directly.
"""

import ctypes
import sys
import threading
import time
import tkinter as tk
import webbrowser
from collections.abc import Callable
from pathlib import Path
from tkinter import filedialog, ttk

from . import __version__
from .app import App, SettingsError

BG = "#1E2124"
PANEL = "#2B2E33"
HOVER = "#383C42"
FG = "#DCDDDE"
MUTED = "#8E9297"
GOLD = "#F2B83C"
GOLD_HOVER = "#F5C862"
RED = "#ED6A5E"
GREEN = "#57C27A"

FONT = ("Segoe UI", 10)
SMALL = ("Segoe UI", 9)
SECTION = ("Segoe UI Semibold", 10)
TITLE = ("Segoe UI Semibold", 16)
WRAP = 470

EVENT_NAMES = {
    "level_up": "Level up", "loot": "Loot", "death": "Death", "quest": "Quest",
    "boss_kill": "Boss kill", "skill_up": "Skill up",
}

REALTIME_EXPLAIN = (
    "Normally events post when WoW saves (on /reload or logout). With this on, "
    "the addon shows a thin black-and-white strip in a corner of the game for "
    "about two seconds after an event, and DOINK reads it straight off the WoW "
    "window. It reads only that strip, never saves images, and switches itself "
    "off if reading starts costing CPU or memory. Needs /doink realtime on in "
    "game as well.")
REALTIME_DOCS = "https://github.com/ryan-flan/DOINK#how-realtime-works-experimental"


def enable_dpi_awareness() -> None:
    """Crisp text on scaled displays. Call before creating any window."""
    try:
        ctypes.windll.shcore.SetProcessDpiAwareness(1)
    except (AttributeError, OSError):
        try:
            ctypes.windll.user32.SetProcessDPIAware()
        except (AttributeError, OSError):
            pass


def describe_path(path: Path) -> str:
    """"_classic_beta_, account 12345#1" from .../_classic_beta_/WTF/Account/12345#1/SavedVariables/DOINK.lua"""
    try:
        return f"{path.parents[4].name}, account {path.parents[1].name}"
    except IndexError:
        return str(path)


class SettingsWindow:
    def __init__(self, root: tk.Tk, app: App, header_png: Path,
                 open_log: Callable[[], None], open_folder: Callable[[], None]):
        self.root = root
        self.app = app
        self._open_log = open_log
        self._open_folder = open_folder
        self._visible = False
        self._styled_title_bar = False

        root.title("DOINK")
        root.configure(bg=BG)
        root.resizable(False, False)
        root.protocol("WM_DELETE_WINDOW", self.hide)
        self._style()
        self._logo = self._load_png(header_png)
        self._build()
        root.withdraw()

    # ---------------------------------------------------------- public

    def show(self) -> None:
        self.webhook.set(self.app.config.webhook_url)
        self.autostart.set(self.app.autostart_enabled())
        self.realtime_var.set(self.app.config.realtime)
        self._message(self.webhook_msg, "")
        self._message(self.wow_msg, "")
        self._refresh_overrides()
        self._refresh()
        self.root.deiconify()
        if not self._styled_title_bar:
            self._dark_title_bar()
            self._styled_title_bar = True
        self.root.lift()
        self.root.attributes("-topmost", True)  # come to the front once...
        self.root.after(200, lambda: self.root.attributes("-topmost", False))
        self.root.focus_force()
        if not self._visible:
            self._visible = True
            self._tick()

    def hide(self) -> None:
        self._visible = False
        self.root.withdraw()

    # ---------------------------------------------------------- layout

    def _style(self) -> None:
        s = ttk.Style(self.root)
        s.theme_use("clam")
        s.configure(".", background=BG, foreground=FG, font=FONT, bordercolor=PANEL,
                    lightcolor=PANEL, darkcolor=PANEL, troughcolor=PANEL,
                    focuscolor=GOLD)
        s.configure("TFrame", background=BG)
        s.configure("TLabel", background=BG, foreground=FG)
        s.configure("Muted.TLabel", foreground=MUTED, font=SMALL)
        s.configure("Title.TLabel", font=TITLE)
        s.configure("Section.TLabel", foreground=GOLD, font=SECTION)
        s.configure("TEntry", fieldbackground=PANEL, foreground=FG, insertcolor=FG,
                    padding=6)
        s.configure("TButton", background=PANEL, foreground=FG, padding=(12, 5),
                    borderwidth=0, focusthickness=0)
        s.map("TButton", background=[("disabled", BG), ("active", HOVER)],
              foreground=[("disabled", MUTED)])
        s.configure("Accent.TButton", background=GOLD, foreground=BG,
                    font=("Segoe UI Semibold", 10))
        s.map("Accent.TButton", background=[("disabled", PANEL), ("active", GOLD_HOVER)])
        s.configure("Link.TButton", background=BG, foreground=MUTED, padding=(0, 2),
                    font=SMALL)
        s.map("Link.TButton", background=[("active", BG)], foreground=[("active", GOLD)])
        s.configure("TCheckbutton", background=BG, foreground=FG)
        s.map("TCheckbutton", background=[("active", BG)],
              indicatorcolor=[("selected", GOLD), ("!selected", PANEL)],
              foreground=[("disabled", MUTED)])

    def _build(self) -> None:
        page = ttk.Frame(self.root, padding=(22, 18, 22, 16))
        page.grid(sticky="nsew")
        page.columnconfigure(0, weight=1)
        row = 0

        # Header: logo, name, version, live status.
        header = ttk.Frame(page)
        header.grid(row=row, sticky="ew")
        if self._logo:
            tk.Label(header, image=self._logo, bg=BG).grid(row=0, column=0, rowspan=2,
                                                          padx=(0, 12))
        ttk.Label(header, text="DOINK", style="Title.TLabel").grid(row=0, column=1, sticky="w")
        ttk.Label(header, text=f"v{__version__}", style="Muted.TLabel").grid(
            row=0, column=2, sticky="sw", padx=(8, 0), pady=(0, 4))
        self.status_label = ttk.Label(header, wraplength=WRAP - 60, font=SMALL)
        self.status_label.grid(row=1, column=1, columnspan=2, sticky="w")
        row += 1

        # Discord webhook.
        row = self._section(page, row, "Discord webhook")
        entry_row = ttk.Frame(page)
        entry_row.grid(row=row, sticky="ew")
        entry_row.columnconfigure(0, weight=1)
        self.webhook = tk.StringVar()
        self.webhook_entry = ttk.Entry(entry_row, textvariable=self.webhook, show="•",
                                       width=48)
        self.webhook_entry.grid(row=0, column=0, sticky="ew")
        self.reveal = ttk.Button(entry_row, text="Show", width=6, command=self._toggle_reveal)
        self.reveal.grid(row=0, column=1, padx=(6, 0))
        row += 1

        buttons = ttk.Frame(page)
        buttons.grid(row=row, sticky="w", pady=(8, 0))
        self.save_btn = ttk.Button(buttons, text="Save", style="Accent.TButton",
                                   command=self._save_webhook)
        self.save_btn.grid(row=0, column=0)
        self.test_btn = ttk.Button(buttons, text="Send test message",
                                   command=self._send_test)
        self.test_btn.grid(row=0, column=1, padx=(8, 0))
        self.webhook_msg = ttk.Label(buttons, font=SMALL)
        self.webhook_msg.grid(row=0, column=2, padx=(12, 0))
        row += 1

        ttk.Label(page, style="Muted.TLabel", wraplength=WRAP, justify="left",
                  text="In Discord: Server Settings → Integrations → Webhooks → "
                       "New Webhook → Copy Webhook URL.").grid(row=row, sticky="w",
                                                               pady=(8, 0))
        row += 1
        self.overrides = ttk.Label(page, style="Muted.TLabel", wraplength=WRAP,
                                   justify="left")
        self.overrides.grid(row=row, sticky="w")
        row += 1

        # World of Warcraft.
        row = self._section(page, row, "World of Warcraft")
        self.paths = ttk.Label(page, style="Muted.TLabel", wraplength=WRAP, justify="left")
        self.paths.grid(row=row, sticky="w")
        row += 1
        wow_row = ttk.Frame(page)
        wow_row.grid(row=row, sticky="w", pady=(8, 0))
        ttk.Button(wow_row, text="Choose WoW folder…", command=self._choose_wow).grid(
            row=0, column=0)
        self.wow_msg = ttk.Label(wow_row, font=SMALL, wraplength=WRAP - 170)
        self.wow_msg.grid(row=0, column=1, padx=(12, 0))
        row += 1

        # Realtime (experimental).
        row = self._section(page, row, "Realtime (experimental)")
        self.realtime_var = tk.BooleanVar()
        rt_check = ttk.Checkbutton(page, text="Realtime posting: post events while you play",
                                   variable=self.realtime_var, command=self._toggle_realtime)
        rt_check.grid(row=row, sticky="w")
        if not self.app.realtime_available():
            rt_check.state(["disabled"])
            rt_check.configure(text="Realtime posting (Windows only)")
        row += 1
        ttk.Label(page, style="Muted.TLabel", wraplength=WRAP, justify="left",
                  text=REALTIME_EXPLAIN).grid(row=row, sticky="w", pady=(4, 0))
        row += 1
        ttk.Button(page, text="How it works, in detail", style="Link.TButton",
                   command=self._learn_more).grid(row=row, sticky="w", pady=(2, 6))
        row += 1
        self.rt_reader = ttk.Label(page, font=SMALL, wraplength=WRAP, justify="left")
        self.rt_reader.grid(row=row, sticky="w")
        row += 1
        self.rt_addon = ttk.Label(page, font=SMALL, wraplength=WRAP, justify="left")
        self.rt_addon.grid(row=row, sticky="w")
        row += 1

        # Startup.
        row = self._section(page, row, "Startup")
        self.autostart = tk.BooleanVar()
        check = ttk.Checkbutton(page, text="Start DOINK with Windows",
                                variable=self.autostart, command=self._toggle_autostart)
        check.grid(row=row, sticky="w")
        if not self.app.autostart_available():
            check.state(["disabled"])
            check.configure(text="Start DOINK with Windows (doink.exe only)")
        row += 1

        # Recent posts.
        row = self._section(page, row, "Recent posts")
        self.recent = ttk.Label(page, font=("Consolas", 9), foreground=FG, justify="left")
        self.recent.grid(row=row, sticky="w")
        row += 1

        # Footer.
        footer = ttk.Frame(page)
        footer.grid(row=row, sticky="ew", pady=(18, 0))
        footer.columnconfigure(2, weight=1)
        ttk.Button(footer, text="Open log", style="Link.TButton",
                   command=self._open_log).grid(row=0, column=0)
        ttk.Button(footer, text="Open DOINK folder", style="Link.TButton",
                   command=self._open_folder).grid(row=0, column=1, padx=(14, 0))
        ttk.Button(footer, text="Close", command=self.hide).grid(row=0, column=3)

    @staticmethod
    def _section(page, row: int, title: str) -> int:
        ttk.Label(page, text=title.upper(), style="Section.TLabel").grid(
            row=row, sticky="w", pady=(18, 6))
        return row + 1

    # ---------------------------------------------------------- live state

    def _tick(self) -> None:
        if not self._visible:
            return
        self._refresh()
        self.root.after(1000, self._tick)

    def _refresh(self) -> None:
        status = self.app.status
        error = status.error
        text = status.summary().removeprefix("DOINK: ")
        self.status_label.configure(text=("⚠ " if error else "● ") + text[:1].upper() + text[1:],
                                    foreground=RED if error else GREEN)

        watched = self.app.watched
        if watched:
            wow = watched[0].parents[5] if len(watched[0].parents) > 5 else ""
            lines = [f"Watching {len(watched)} file{'s' if len(watched) != 1 else ''} in {wow}"]
            lines += [f"   {describe_path(p)}" for p in watched]
        else:
            lines = ["World of Warcraft with the DOINK addon wasn't found in the usual "
                     "places. Choose the folder that contains _classic_beta_ (or similar)."]
        self.paths.configure(text="\n".join(lines))

        self._refresh_realtime(status.realtime())

        recent = status.recent()
        if recent:
            self.recent.configure(font=("Consolas", 9), text="\n".join(
                f"{'⚡' if live else ' '} {time.strftime('%H:%M', time.localtime(at))}  "
                f"{who:<18.18} {EVENT_NAMES.get(what, what)}" for at, who, what, live in recent))
        else:
            self.recent.configure(text="Nothing posted yet. Events post when WoW saves:\n"
                                       "on /reload, logout, or /doink flush.",
                                  font=SMALL)

    def _refresh_realtime(self, rt: dict) -> None:
        if not rt["enabled"]:
            reason = rt.get("reason") or "off"
            if reason == "off":
                self._message(self.rt_reader, "Reader: off", MUTED)
            else:
                self._message(self.rt_reader, f"Reader: off — {reason}", RED)
        else:
            meter = rt.get("meter") or {}
            cost = ""
            if meter.get("ws_mb") is not None and meter.get("rate_hz"):
                cost = (f" · {meter['cpu_pct']:.1f}% CPU · {meter['ws_mb']:.0f} MB"
                        f" · {meter['rate_hz']:.0f} captures/s")
            window = rt.get("window")
            if window == "foreground":
                self._message(self.rt_reader, "Reader: watching the WoW window" + cost, GREEN)
            elif window == "background":
                self._message(self.rt_reader, "Reader: WoW is behind another window; "
                              "reading resumes when it's in front" + cost, MUTED)
            elif window == "minimized":
                self._message(self.rt_reader, "Reader: WoW is minimized", MUTED)
            else:
                self._message(self.rt_reader, "Reader: waiting for WoW to start", MUTED)

        hello = rt.get("hello")
        if hello:
            when = time.strftime("%H:%M", time.localtime(hello["at"]))
            strip = rt.get("strip")
            detail = f" · {strip[0]:g} px blocks, {strip[1]}/row" if strip else ""
            test = " (test pattern)" if hello.get("test") else ""
            self._message(self.rt_addon, f"Addon: sending · hello from {hello['who']} at {when}"
                          f" · v{hello['addon']}{detail}{test}", GREEN)
        elif rt["enabled"]:
            self._message(self.rt_addon, "Addon: nothing received yet. In game: "
                          "/doink realtime on, then /doink realtime test to check.", MUTED)
        else:
            self._message(self.rt_addon, "Addon: /doink realtime on in game turns the strip on.",
                          MUTED)

    def _refresh_overrides(self) -> None:
        hooks = self.app.game_webhooks()
        notes = []
        if "*" in hooks:
            notes.append("An in-game webhook (/doink webhook) is set for all characters "
                         "and takes priority over this one.")
        chars = sorted(k.rsplit("-", 1)[0] for k in hooks if k != "*")
        if chars:
            notes.append(f"Using their own in-game webhook: {', '.join(chars)}.")
        self.overrides.configure(text=" ".join(notes))

    # ---------------------------------------------------------- actions

    def _toggle_reveal(self) -> None:
        hidden = self.webhook_entry.cget("show") == "•"
        self.webhook_entry.configure(show="" if hidden else "•")
        self.reveal.configure(text="Hide" if hidden else "Show")

    def _save_webhook(self) -> None:
        try:
            self.app.set_webhook(self.webhook.get())
        except SettingsError as e:
            self._message(self.webhook_msg, str(e), RED)
            return
        except OSError as e:
            self._message(self.webhook_msg, f"Couldn't save settings: {e}", RED)
            return
        self._message(self.webhook_msg, "Saved" if self.webhook.get().strip() else "Cleared",
                      GREEN)

    def _send_test(self) -> None:
        url = self.webhook.get()
        self.test_btn.state(["disabled"])
        self._message(self.webhook_msg, "Sending…", MUTED)

        def done(error: Exception | None) -> None:
            self.test_btn.state(["!disabled"])
            if error:
                self._message(self.webhook_msg, str(error), RED)
            else:
                self._message(self.webhook_msg, "Sent! Check your Discord channel.", GREEN)

        self._in_background(lambda: self.app.send_test(url), done)

    def _choose_wow(self) -> None:
        folder = filedialog.askdirectory(parent=self.root, mustexist=True,
                                         title="Choose your World of Warcraft folder")
        if not folder:
            return
        try:
            count = self.app.set_wow_dir(folder)
        except SettingsError as e:
            self._message(self.wow_msg, str(e), RED)
            return
        self._message(self.wow_msg, f"Watching {count} file{'s' if count != 1 else ''}",
                      GREEN)
        self._refresh_overrides()
        self._refresh()

    def _toggle_realtime(self) -> None:
        try:
            self.app.set_realtime(self.realtime_var.get())
        except OSError as e:
            self.realtime_var.set(self.app.config.realtime)
            self._message(self.rt_reader, f"Couldn't save settings: {e}", RED)
            return
        self._refresh_realtime(self.app.status.realtime())

    @staticmethod
    def _learn_more() -> None:
        webbrowser.open(REALTIME_DOCS)

    def _toggle_autostart(self) -> None:
        try:
            self.app.set_autostart(self.autostart.get())
        except OSError:
            self.autostart.set(self.app.autostart_enabled())

    # ---------------------------------------------------------- helpers

    def _in_background(self, work: Callable[[], None],
                       done: Callable[[Exception | None], None]) -> None:
        """Run ``work`` off the UI thread (e.g. a network call); call ``done``
        back on it. Tk isn't thread-safe, so the result is polled."""
        result: dict = {}

        def target():
            try:
                work()
                result["error"] = None
            except Exception as e:  # shown to the user, never raised in Tk
                result["error"] = e

        def poll():
            if "error" in result:
                done(result["error"])
            else:
                self.root.after(100, poll)

        threading.Thread(target=target, daemon=True).start()
        poll()

    @staticmethod
    def _message(label: ttk.Label, text: str, colour: str = FG) -> None:
        label.configure(text=text, foreground=colour)

    def _load_png(self, path: Path):
        try:
            return tk.PhotoImage(master=self.root, file=str(path))
        except tk.TclError:
            return None

    def _dark_title_bar(self) -> None:
        """Windows 10 20H1+/11: dark caption to match the window."""
        if sys.platform != "win32":
            return
        try:
            from ctypes import wintypes
            get_parent = ctypes.windll.user32.GetParent
            get_parent.restype, get_parent.argtypes = wintypes.HWND, [wintypes.HWND]
            hwnd = get_parent(self.root.winfo_id())
            on = ctypes.c_int(1)
            ctypes.windll.dwmapi.DwmSetWindowAttribute(
                hwnd, 20, ctypes.byref(on), ctypes.sizeof(on))  # DWMWA_USE_IMMERSIVE_DARK_MODE
        except (AttributeError, OSError):
            pass
