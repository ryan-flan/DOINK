"""Windows system tray icon, written against the Win32 API with ctypes so the
companion stays dependency-free.

The tray runs its own thread (Win32 windows must be serviced by the thread
that created them). Status changes reach it as a posted window message;
"settings" and "quit" go back to the UI thread through the ``command``
callback, which must be thread-safe (main.py passes a queue's put).
"""

import ctypes
import logging
import os
import sys
from collections.abc import Callable
from ctypes import wintypes
from pathlib import Path

from . import __version__, autostart
from .status import Status

log = logging.getLogger(__name__)

user32 = ctypes.WinDLL("user32", use_last_error=True)
shell32 = ctypes.WinDLL("shell32", use_last_error=True)
kernel32 = ctypes.WinDLL("kernel32", use_last_error=True)

LRESULT = ctypes.c_ssize_t
WNDPROC = ctypes.WINFUNCTYPE(LRESULT, wintypes.HWND, wintypes.UINT,
                             wintypes.WPARAM, wintypes.LPARAM)


class WNDCLASSW(ctypes.Structure):
    _fields_ = [
        ("style", wintypes.UINT),
        ("lpfnWndProc", WNDPROC),
        ("cbClsExtra", ctypes.c_int),
        ("cbWndExtra", ctypes.c_int),
        ("hInstance", wintypes.HINSTANCE),
        ("hIcon", wintypes.HICON),
        ("hCursor", wintypes.HANDLE),
        ("hbrBackground", wintypes.HBRUSH),
        ("lpszMenuName", wintypes.LPCWSTR),
        ("lpszClassName", wintypes.LPCWSTR),
    ]


class NOTIFYICONDATAW(ctypes.Structure):
    _fields_ = [
        ("cbSize", wintypes.DWORD),
        ("hWnd", wintypes.HWND),
        ("uID", wintypes.UINT),
        ("uFlags", wintypes.UINT),
        ("uCallbackMessage", wintypes.UINT),
        ("hIcon", wintypes.HICON),
        ("szTip", wintypes.WCHAR * 128),
        ("dwState", wintypes.DWORD),
        ("dwStateMask", wintypes.DWORD),
        ("szInfo", wintypes.WCHAR * 256),
        ("uVersion", wintypes.UINT),
        ("szInfoTitle", wintypes.WCHAR * 64),
        ("dwInfoFlags", wintypes.DWORD),
        ("guidItem", ctypes.c_byte * 16),
        ("hBalloonIcon", wintypes.HICON),
    ]


def _api(fn, restype, *argtypes):
    # Explicit signatures: without them ctypes passes 64-bit handles as
    # 32-bit ints and things crash in confusing ways.
    fn.restype, fn.argtypes = restype, argtypes
    return fn


GetModuleHandleW = _api(kernel32.GetModuleHandleW, wintypes.HMODULE, wintypes.LPCWSTR)
CreateMutexW = _api(kernel32.CreateMutexW, wintypes.HANDLE,
                    wintypes.LPVOID, wintypes.BOOL, wintypes.LPCWSTR)
RegisterClassW = _api(user32.RegisterClassW, wintypes.ATOM, ctypes.POINTER(WNDCLASSW))
CreateWindowExW = _api(user32.CreateWindowExW, wintypes.HWND, wintypes.DWORD,
                       wintypes.LPCWSTR, wintypes.LPCWSTR, wintypes.DWORD,
                       ctypes.c_int, ctypes.c_int, ctypes.c_int, ctypes.c_int,
                       wintypes.HWND, wintypes.HMENU, wintypes.HINSTANCE, wintypes.LPVOID)
DefWindowProcW = _api(user32.DefWindowProcW, LRESULT, wintypes.HWND, wintypes.UINT,
                      wintypes.WPARAM, wintypes.LPARAM)
DestroyWindow = _api(user32.DestroyWindow, wintypes.BOOL, wintypes.HWND)
PostMessageW = _api(user32.PostMessageW, wintypes.BOOL, wintypes.HWND, wintypes.UINT,
                    wintypes.WPARAM, wintypes.LPARAM)
PostQuitMessage = _api(user32.PostQuitMessage, None, ctypes.c_int)
GetMessageW = _api(user32.GetMessageW, wintypes.BOOL, ctypes.POINTER(wintypes.MSG),
                   wintypes.HWND, wintypes.UINT, wintypes.UINT)
TranslateMessage = _api(user32.TranslateMessage, wintypes.BOOL, ctypes.POINTER(wintypes.MSG))
DispatchMessageW = _api(user32.DispatchMessageW, LRESULT, ctypes.POINTER(wintypes.MSG))
RegisterWindowMessageW = _api(user32.RegisterWindowMessageW, wintypes.UINT, wintypes.LPCWSTR)
CreatePopupMenu = _api(user32.CreatePopupMenu, wintypes.HMENU)
AppendMenuW = _api(user32.AppendMenuW, wintypes.BOOL, wintypes.HMENU, wintypes.UINT,
                   ctypes.c_size_t, wintypes.LPCWSTR)
TrackPopupMenu = _api(user32.TrackPopupMenu, ctypes.c_int, wintypes.HMENU, wintypes.UINT,
                      ctypes.c_int, ctypes.c_int, ctypes.c_int, wintypes.HWND, wintypes.LPVOID)
DestroyMenu = _api(user32.DestroyMenu, wintypes.BOOL, wintypes.HMENU)
SetMenuDefaultItem = _api(user32.SetMenuDefaultItem, wintypes.BOOL, wintypes.HMENU,
                          wintypes.UINT, wintypes.UINT)
GetCursorPos = _api(user32.GetCursorPos, wintypes.BOOL, ctypes.POINTER(wintypes.POINT))
SetForegroundWindow = _api(user32.SetForegroundWindow, wintypes.BOOL, wintypes.HWND)
GetSystemMetrics = _api(user32.GetSystemMetrics, ctypes.c_int, ctypes.c_int)
LoadImageW = _api(user32.LoadImageW, wintypes.HANDLE, wintypes.HINSTANCE, wintypes.LPCWSTR,
                  wintypes.UINT, ctypes.c_int, ctypes.c_int, wintypes.UINT)
LoadIconW = _api(user32.LoadIconW, wintypes.HICON, wintypes.HINSTANCE, wintypes.LPVOID)
MessageBoxW = _api(user32.MessageBoxW, ctypes.c_int, wintypes.HWND, wintypes.LPCWSTR,
                   wintypes.LPCWSTR, wintypes.UINT)
Shell_NotifyIconW = _api(shell32.Shell_NotifyIconW, wintypes.BOOL, wintypes.DWORD,
                         ctypes.POINTER(NOTIFYICONDATAW))

WM_NULL, WM_DESTROY, WM_CLOSE = 0x0000, 0x0002, 0x0010
WM_LBUTTONUP, WM_RBUTTONUP = 0x0202, 0x0205
WM_APP = 0x8000
WM_TRAY = WM_APP + 1    # mouse events on the icon
WM_STATUS = WM_APP + 2  # Status changed (posted from the worker thread)

NIM_ADD, NIM_MODIFY, NIM_DELETE = 0, 1, 2
NIF_MESSAGE, NIF_ICON, NIF_TIP, NIF_INFO = 0x01, 0x02, 0x04, 0x10
NIIF_INFO, NIIF_WARNING = 0x01, 0x02
MF_STRING, MF_GRAYED, MF_CHECKED, MF_SEPARATOR = 0x0000, 0x0001, 0x0008, 0x0800
TPM_RIGHTBUTTON, TPM_NONOTIFY, TPM_RETURNCMD = 0x0002, 0x0080, 0x0100
IMAGE_ICON, LR_LOADFROMFILE, SM_CXSMICON = 1, 0x0010, 49
IDI_APPLICATION = 32512
MB_ICONINFORMATION, MB_ICONERROR = 0x40, 0x10
ERROR_ALREADY_EXISTS = 183

ID_SETTINGS, ID_LOG, ID_FOLDER, ID_AUTOSTART, ID_UPDATES, ID_QUIT = 1, 2, 3, 4, 5, 9

_mutex = None  # held for the life of the process


def acquire_single_instance(name: str = "Local\\DOINK-companion") -> bool:
    """False if another DOINK companion is already running for this user."""
    global _mutex
    _mutex = CreateMutexW(None, False, name)
    return ctypes.get_last_error() != ERROR_ALREADY_EXISTS


def message_box(text: str, error: bool = False) -> None:
    MessageBoxW(None, text, "DOINK", MB_ICONERROR if error else MB_ICONINFORMATION)


def asset(name: str) -> Path:
    if getattr(sys, "frozen", False):
        return Path(sys._MEIPASS) / "assets" / name  # bundled by PyInstaller
    return Path(__file__).resolve().parent.parent / "assets" / name


def icon_path() -> Path:
    return asset("doink.ico")


class Tray:
    def __init__(self, status: Status, log_path: Path, folder: Path,
                 command: Callable[[str], None]):
        self.status = status
        self.log_path = log_path
        self.folder = folder
        self.command = command  # "settings" | "updates" | "quit"; called from the tray thread
        self.hwnd = None
        self._balloon: str | None = None
        self._balloon_flags = NIIF_WARNING
        self._wndproc = WNDPROC(self._proc)  # keep a reference: Windows calls it
        self._taskbar_created = RegisterWindowMessageW("TaskbarCreated")

    # ---------------------------------------------------------- any thread

    def notify(self, new_error: str | None = None) -> None:
        """Status.on_change hook: refresh the tooltip, and pop a notification
        for a new problem."""
        if new_error:
            self._balloon, self._balloon_flags = new_error, NIIF_WARNING
        if self.hwnd:
            PostMessageW(self.hwnd, WM_STATUS, 0, 0)

    def inform(self, text: str) -> None:
        """Status.on_notice hook: a plain notification (an update is out)."""
        self._balloon, self._balloon_flags = text, NIIF_INFO
        if self.hwnd:
            PostMessageW(self.hwnd, WM_STATUS, 0, 0)

    def quit(self) -> None:
        if self.hwnd:
            PostMessageW(self.hwnd, WM_CLOSE, 0, 0)

    # ---------------------------------------------------------- tray thread

    def run(self) -> None:
        """Create the icon and pump messages until Quit. Blocks."""
        hinstance = GetModuleHandleW(None)
        wc = WNDCLASSW()
        wc.lpfnWndProc = self._wndproc
        wc.hInstance = hinstance
        wc.lpszClassName = "DOINKTray"
        RegisterClassW(ctypes.byref(wc))
        # A hidden top-level window rather than a message-only one: only
        # top-level windows hear "TaskbarCreated" when Explorer restarts.
        self.hwnd = CreateWindowExW(0, "DOINKTray", "DOINK", 0, 0, 0, 0, 0,
                                    None, None, hinstance, None)
        if not self.hwnd:
            raise OSError(ctypes.get_last_error(), "CreateWindowExW failed")

        self._hicon = self._load_icon()
        self._show(NIM_ADD)

        msg = wintypes.MSG()
        while GetMessageW(ctypes.byref(msg), None, 0, 0) > 0:
            TranslateMessage(ctypes.byref(msg))
            DispatchMessageW(ctypes.byref(msg))

    def _load_icon(self):
        size = GetSystemMetrics(SM_CXSMICON)
        icon = LoadImageW(None, str(icon_path()), IMAGE_ICON, size, size, LR_LOADFROMFILE)
        return icon or LoadIconW(None, IDI_APPLICATION)

    def _show(self, action: int) -> None:
        nid = NOTIFYICONDATAW()
        nid.cbSize = ctypes.sizeof(NOTIFYICONDATAW)
        nid.hWnd = self.hwnd
        nid.uID = 1
        nid.uFlags = NIF_MESSAGE | NIF_ICON | NIF_TIP
        nid.uCallbackMessage = WM_TRAY
        nid.hIcon = self._hicon
        nid.szTip = self.status.summary()
        if self._balloon and action != NIM_DELETE:
            nid.uFlags |= NIF_INFO
            nid.szInfoTitle = "DOINK"
            nid.szInfo = self._balloon[:255]
            nid.dwInfoFlags = self._balloon_flags
            self._balloon = None
        if not Shell_NotifyIconW(action, ctypes.byref(nid)) and action == NIM_ADD:
            log.warning("couldn't add the tray icon (no taskbar yet?)")

    def _proc(self, hwnd, msg, wparam, lparam):
        try:
            if msg == WM_TRAY:
                if lparam == WM_LBUTTONUP:
                    self.command("settings")
                elif lparam == WM_RBUTTONUP:
                    self._menu()
                return 0
            if msg == WM_STATUS:
                self._show(NIM_MODIFY)
                return 0
            if msg == self._taskbar_created:  # Explorer restarted
                self._show(NIM_ADD)
                return 0
            if msg == WM_CLOSE:
                self._show(NIM_DELETE)
                DestroyWindow(hwnd)
                return 0
            if msg == WM_DESTROY:
                PostQuitMessage(0)
                return 0
        except Exception:
            log.exception("tray message %#x failed", msg)
            return 0
        return DefWindowProcW(hwnd, msg, wparam, lparam)

    def _menu(self) -> None:
        menu = CreatePopupMenu()
        update = self.status.update()
        version = f"DOINK v{__version__}"
        if update is not None and update.newer:
            version += f"  (v{update.latest} available)"
        AppendMenuW(menu, MF_GRAYED, 0, version)
        AppendMenuW(menu, MF_GRAYED, 0, self.status.summary().removeprefix("DOINK: "))
        AppendMenuW(menu, MF_SEPARATOR, 0, None)
        AppendMenuW(menu, MF_STRING, ID_SETTINGS, "Settings…")
        SetMenuDefaultItem(menu, ID_SETTINGS, 0)  # bold, like a double-click default
        AppendMenuW(menu, MF_STRING, ID_UPDATES, "Check for updates")
        AppendMenuW(menu, MF_STRING, ID_LOG, "Open log")
        AppendMenuW(menu, MF_STRING, ID_FOLDER, "Open DOINK folder")
        if autostart.available():
            checked = MF_CHECKED if autostart.is_enabled() else 0
            AppendMenuW(menu, MF_STRING | checked, ID_AUTOSTART, "Start with Windows")
        AppendMenuW(menu, MF_SEPARATOR, 0, None)
        AppendMenuW(menu, MF_STRING, ID_QUIT, "Quit")

        point = wintypes.POINT()
        GetCursorPos(ctypes.byref(point))
        SetForegroundWindow(self.hwnd)  # else the menu won't close on click-away
        command = TrackPopupMenu(menu, TPM_RIGHTBUTTON | TPM_NONOTIFY | TPM_RETURNCMD,
                                 point.x, point.y, 0, self.hwnd, None)
        PostMessageW(self.hwnd, WM_NULL, 0, 0)
        DestroyMenu(menu)

        if command == ID_SETTINGS:
            self.command("settings")
        elif command == ID_UPDATES:
            self.command("updates")
        elif command == ID_LOG:
            self.open(self.log_path)
        elif command == ID_FOLDER:
            self.open(self.folder)
        elif command == ID_AUTOSTART:
            on = not autostart.is_enabled()
            autostart.set_enabled(on)
            log.info("start with Windows: %s", "on" if on else "off")
        elif command == ID_QUIT:
            log.info("quit from tray")
            self.command("quit")

    @staticmethod
    def open(path: Path) -> None:
        try:
            os.startfile(path)
        except OSError as e:
            log.warning("couldn't open %s: %s", path, e)
