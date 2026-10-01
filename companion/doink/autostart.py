"""Start with Windows.

One string value under HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Run:
per-user, no admin rights, and listed in Task Manager > Startup apps, where it
can also be switched off. Nothing else is written.
"""

import sys

RUN_KEY = r"Software\Microsoft\Windows\CurrentVersion\Run"
NAME = "DOINK"


def available() -> bool:
    """Only the packaged doink.exe registers itself; a dev checkout doesn't."""
    return sys.platform == "win32" and getattr(sys, "frozen", False)


def command() -> str:
    return f'"{sys.executable}"'


def is_enabled(name: str = NAME, cmd: str | None = None) -> bool:
    """True only if the entry points at *this* exe; a stale entry left by a
    moved or deleted copy counts as off (turning it on rewrites it)."""
    import winreg
    try:
        with winreg.OpenKey(winreg.HKEY_CURRENT_USER, RUN_KEY) as key:
            value, _ = winreg.QueryValueEx(key, name)
    except OSError:
        return False
    return value == (cmd or command())


def set_enabled(on: bool, name: str = NAME, cmd: str | None = None) -> None:
    import winreg
    if on:
        # CreateKeyEx, not OpenKey: a fresh profile may not have a Run key yet.
        with winreg.CreateKeyEx(winreg.HKEY_CURRENT_USER, RUN_KEY, 0,
                                winreg.KEY_SET_VALUE) as key:
            winreg.SetValueEx(key, name, 0, winreg.REG_SZ, cmd or command())
        return
    try:
        with winreg.OpenKey(winreg.HKEY_CURRENT_USER, RUN_KEY, 0,
                            winreg.KEY_SET_VALUE) as key:
            winreg.DeleteValue(key, name)
    except FileNotFoundError:
        pass  # no Run key, or no entry: already off
