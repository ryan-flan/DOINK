"""Reads the addon's pixel strip off the WoW window (realtime, experimental).

Mirror of addon/DOINK/Transports/Pixel.lua; the layout is documented in
CLAUDE.md under "Pixel transport contract".

What this touches: two thin bands of the WoW client area, its top and bottom
BAND_ROWS pixel rows across the full width, copied with GDI ``BitBlt`` into
one buffer that is allocated once and reused, then decoded in place. Nothing
is written to disk and nothing else on the screen is read. ``ResourceMeter``
watches what the reading costs and tells the worker to stop if it climbs.
"""

import ctypes
import os
import re
import sys
import time
from collections import deque
from dataclasses import dataclass

VERSION = 1
SYNC = "1010101010110011"
ROWS = 3                       # 1 header row + 2 data rows
HEADER_BYTES = 10              # before the header crc8
HEADER_BITS = len(SYNC) + (HEADER_BYTES + 1) * 8   # 104 blocks of row 0
MAX_BLOCKS = 1200
BLOCK_MIN, BLOCK_MAX = 2, 8    # pixels per block the reader will look for
BAND_ROWS = ROWS * BLOCK_MAX   # 24: enough for the largest block size
MIN_CONTRAST = 48              # white minus black, out of 255
# Forever beta ("WowB.exe") and retail/classic ("Wow.exe") window classes.
WOW_WINDOW_CLASSES = ("waApplication Window", "GxWindowClass")

# Block counts at which each sync run starts, plus the end: the shape of the
# sync pattern, used to fit the block width from the observed transitions.
_SYNC_RUN_STARTS = [0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 12, 14, 16]


def crc16(data: bytes) -> int:
    """CRC-16/CCITT-FALSE (poly 0x1021, init 0xFFFF)."""
    crc = 0xFFFF
    for byte in data:
        crc ^= byte << 8
        for _ in range(8):
            crc = ((crc << 1) ^ 0x1021) & 0xFFFF if crc & 0x8000 else (crc << 1) & 0xFFFF
    return crc


def crc8(data: bytes) -> int:
    """CRC-8 (poly 0x07, init 0)."""
    crc = 0
    for byte in data:
        crc ^= byte
        for _ in range(8):
            crc = ((crc << 1) ^ 0x07) & 0xFF if crc & 0x80 else (crc << 1) & 0xFF
    return crc


def capacity(blocks: int) -> int:
    return (ROWS - 1) * blocks // 8


@dataclass(frozen=True)
class Chunk:
    msg_id: int
    index: int
    count: int
    payload: bytes
    blocks: int        # strip width in blocks, from the header
    block_px: float    # measured block size


# ------------------------------------------------------------------ decoding

def _sync_regex(block: int) -> re.Pattern:
    """The sync pattern as pixel runs of about ``block`` px. The tolerance
    (±2) only has to *find* the sync; ``_fit_sync`` measures it precisely,
    and anything spurious fails the CRCs."""
    lo, hi = max(1, block - 2), block + 2
    lo2, hi2 = max(2, 2 * block - 2), 2 * block + 2
    single = f"1{{{lo},{hi}}}0{{{lo},{hi}}}"
    # Header bit 0 is the version's MSB, which is 0 for version 1, so a
    # black block always follows the sync.
    return re.compile(f"(?:{single}){{5}}1{{{lo2},{hi2}}}0{{{lo2},{hi2}}}1{{{lo2},{hi2}}}(?=0)")


_SYNC_PATTERNS = [_sync_regex(b) for b in range(BLOCK_MIN, BLOCK_MAX + 1)]


def _threshold_line(line: bytes) -> str | None:
    """Blue-channel scanline -> '0'/'1' string, or None if it's all one shade."""
    lo, hi = min(line), max(line)
    if hi - lo < MIN_CONTRAST:
        return None
    cut = (hi + lo) // 2
    table = bytes(49 if v > cut else 48 for v in range(256))  # '1' / '0'
    return line.translate(table).decode("ascii")


class _Grid:
    """Running least-squares fit of pixel position = x0 + k * block over
    (block index, transition pixel) pairs."""

    def __init__(self):
        self.n = self.sk = self.st = self.skk = self.skt = 0.0

    def add(self, k: float, t: float) -> None:
        self.n += 1
        self.sk += k
        self.st += t
        self.skk += k * k
        self.skt += k * t

    def fit(self) -> tuple[float, float] | None:
        det = self.n * self.skk - self.sk * self.sk
        if self.n < 2 or det == 0:
            return None
        block = (self.n * self.skt - self.sk * self.st) / det
        return (self.st - block * self.sk) / self.n, block


_TRANSITION = re.compile(r"(?=01|10)")


def _fit_sync(bits: str, start: int) -> _Grid | None:
    """Seed a grid from the sync's run starts, skipping the first run (it
    may be merged with whatever is left of the strip)."""
    transitions = [start]
    i = start
    while len(transitions) < 14 and i < len(bits):
        j = i
        while j < len(bits) and bits[j] == bits[i]:
            j += 1
        transitions.append(j)
        i = j
    if len(transitions) < 14:
        return None
    grid = _Grid()
    for k, t in zip(_SYNC_RUN_STARTS[1:], transitions[1:14]):
        grid.add(k, t)
    fit = grid.fit()
    if fit is None or not BLOCK_MIN - 0.6 <= fit[1] <= BLOCK_MAX + 0.6:
        return None
    return grid


def _refine_grid(bits: str, grid: _Grid, limit_blocks: int) -> tuple[float, float]:
    """Walk a row's transitions, snapping each to the nearest grid line of
    the running fit and folding it in. Transitions far along the row pin the
    block width down far better than the sync's 16 blocks can: a 0.03 px
    error there is a whole block by column 400."""
    x0, block = grid.fit()
    start = int(x0 + 16 * block)                     # past the sync
    end = min(len(bits), int(x0 + (limit_blocks + 1) * block))
    if end <= start:
        return x0, block
    for match in _TRANSITION.finditer(bits, start, end):
        t = match.start() + 1
        k = (t - x0) / block
        nearest = round(k)
        if abs(k - nearest) <= 0.3:
            grid.add(nearest, t)
            x0, block = grid.fit()
    return x0, block


def decode(buf, width: int, stride: int, band_top: int, band_height: int,
           bottom: bool, trace: list[str] | None = None) -> Chunk | None:
    """Look for one chunk in a band of ``buf`` (32-bit BGRA rows).

    ``band_top``/``band_height`` select the band's rows in ``buf``. With
    ``bottom`` the strip sits against the band's bottom edge, else its top.
    ``trace``, if given, collects why candidates were rejected.
    """
    if band_height < ROWS * BLOCK_MIN or width < HEADER_BITS * BLOCK_MIN:
        return None

    def blue(x: int, y: int) -> int:
        return buf[(band_top + y) * stride + x * 4]

    def line(y: int) -> bytes:
        offset = (band_top + y) * stride
        return bytes(buf[offset:offset + width * 4][::4])

    lines: dict[int, str | None] = {}  # thresholded scanline per y, shared across sizes
    for pattern, block_guess in zip(_SYNC_PATTERNS, range(BLOCK_MIN, BLOCK_MAX + 1)):
        y = band_height - ROWS * block_guess + block_guess // 2 if bottom else block_guess // 2
        if not 0 <= y < band_height:
            continue
        if y not in lines:
            lines[y] = _threshold_line(line(y))
        bits = lines[y]
        if bits is None:
            continue
        for match in pattern.finditer(bits):
            grid = _fit_sync(bits, match.start())
            if grid is None:
                if trace is not None:
                    trace.append(f"B~{block_guess}: sync-like run at x={match.start()} didn't fit")
                continue
            chunk = _decode_at(blue, line, bits, width, band_height, bottom, grid, trace)
            if chunk is not None:
                return chunk
    return None


def _decode_at(blue, line, row0_bits: str, width: int, band_height: int,
               bottom: bool, grid: _Grid, trace: list[str] | None = None) -> Chunk | None:
    x0, block = _refine_grid(row0_bits, grid, HEADER_BITS)

    def reject(why: str) -> None:
        if trace is not None:
            trace.append(f"sync at x={x0:.1f} block={block:.2f}px: {why}")

    def row_y(r: int) -> int:
        if bottom:
            return int(band_height - (ROWS - r - 0.5) * block)
        return int((r + 0.5) * block)

    def sample(r: int, k: int, gx0: float = x0, gblock: float = block) -> int | None:
        x = int(gx0 + (k + 0.5) * gblock)
        y = row_y(r)
        if not (0 <= x < width and 0 <= y < band_height):
            return None
        return blue(x, y)

    # Calibrate black/white on the sync blocks themselves.
    whites = [v for k, c in enumerate(SYNC) if c == "1" and (v := sample(0, k)) is not None]
    blacks = [v for k, c in enumerate(SYNC) if c == "0" and (v := sample(0, k)) is not None]
    if len(whites) < 8 or len(blacks) < 6:
        reject("sync blocks off the edge")
        return None
    white, black = sum(whites) / len(whites), sum(blacks) / len(blacks)
    if white - black < MIN_CONTRAST:
        reject(f"low contrast (white {white:.0f}, black {black:.0f})")
        return None
    cut = (white + black) / 2

    def read_bits(r: int, first: int, count: int, gx0: float = x0,
                  gblock: float = block) -> list[int] | None:
        out = []
        for k in range(first, first + count):
            v = sample(r, k, gx0, gblock)
            if v is None:
                return None
            out.append(1 if v > cut else 0)
        return out

    header_bits = read_bits(0, len(SYNC), (HEADER_BYTES + 1) * 8)
    if header_bits is None:
        reject("header runs off the edge")
        return None
    header = bytes(_pack(header_bits))
    if crc8(header[:HEADER_BYTES]) != header[HEADER_BYTES]:
        reject(f"header crc mismatch (header {header.hex()})")
        return None
    if header[0] >> 4 != VERSION:
        reject(f"version {header[0] >> 4}")
        return None
    blocks = (header[1] << 8) | header[2]
    msg_id = (header[3] << 8) | header[4]
    index, count, length = header[5], header[6], header[7]
    payload_crc = (header[8] << 8) | header[9]
    if not HEADER_BITS <= blocks <= MAX_BLOCKS or count < 1 or index >= count:
        reject(f"bad header fields (blocks {blocks}, chunk {index}/{count})")
        return None
    if length > capacity(blocks):
        reject(f"payload length {length} over capacity")
        return None

    data_bits = []
    for i in range(0, length * 8, blocks):
        row = 1 + i // blocks
        if row >= ROWS:
            return None
        # Each data row re-fits the grid from its own transitions, so a
        # fractional block width can't drift the sampling off the far blocks.
        row_bits = _threshold_line(line(row_y(row)))
        gx0, gblock = x0, block
        if row_bits is not None:
            row_grid = _Grid()
            for k, t in zip(_SYNC_RUN_STARTS[1:], (x0 + s * block for s in _SYNC_RUN_STARTS[1:])):
                row_grid.add(k, t)  # anchor on the header row's grid
            gx0, gblock = _refine_grid(row_bits, row_grid, blocks)
        got = read_bits(row, 0, min(blocks, length * 8 - i), gx0, gblock)
        if got is None:
            reject(f"data row {row} runs off the edge")
            return None
        data_bits.extend(got)
    payload = bytes(_pack(data_bits))
    if crc16(payload) != payload_crc:
        reject(f"payload crc mismatch (chunk {index}/{count}, {length} bytes)")
        return None
    return Chunk(msg_id, index, count, payload, blocks, block)


def _pack(bits: list[int]) -> list[int]:
    return [int("".join(map(str, bits[i:i + 8])), 2) for i in range(0, len(bits) - 7, 8)]


# ------------------------------------------------------------------ assembly

class Assembler:
    """Collects chunks by message id; returns each complete message once."""

    def __init__(self, ttl: float = 30.0, remember: int = 64):
        self.ttl = ttl
        self._partial: dict[int, dict] = {}
        self._done: deque[int] = deque(maxlen=remember)

    def add(self, chunk: Chunk, now: float | None = None) -> bytes | None:
        now = time.monotonic() if now is None else now
        if chunk.msg_id in self._done:
            return None
        for msg_id in [m for m, p in self._partial.items() if now - p["at"] > self.ttl]:
            del self._partial[msg_id]

        entry = self._partial.setdefault(
            chunk.msg_id, {"count": chunk.count, "parts": {}, "at": now})
        if entry["count"] != chunk.count:
            entry.update(count=chunk.count, parts={})  # id reuse: start over
        entry["parts"][chunk.index] = chunk.payload
        entry["at"] = now
        if len(entry["parts"]) < entry["count"]:
            return None
        del self._partial[chunk.msg_id]
        self._done.append(chunk.msg_id)
        return b"".join(entry["parts"][i] for i in range(entry["count"]))


# ------------------------------------------------------------------ resources

class ResourceMeter:
    """What the reader costs, and whether it should stop.

    Records each capture+decode duration; samples the process working set
    and GDI handle count every ``sample_every`` captures. ``should_stop``
    returns a reason once captures cost real CPU, GDI handles have grown,
    or memory has climbed *steadily* well past the baseline.

    The whole process is measured, so memory is judged on a steady climb
    (``CLIMB`` consecutive rising samples) and not on its level: opening
    the settings window adds ~80 MB of Tk in one step, and that must not
    count as a leak."""

    WARMUP = 20
    BASELINE_SAMPLE = 3  # skip the first samples: the UI is still starting up
    CLIMB = 5

    def __init__(self, max_avg_ms: float = 25.0, max_growth_mb: float = 100.0,
                 max_gdi_growth: int = 50, sample_every: int = 50):
        self.max_avg_ms = max_avg_ms
        self.max_growth_mb = max_growth_mb
        self.max_gdi_growth = max_gdi_growth
        self.sample_every = sample_every
        self._durations: deque[float] = deque(maxlen=64)
        self._cpu: deque[float] = deque(maxlen=64)
        self._count = 0
        self._samples = 0
        self._window_start = (time.monotonic(), time.process_time(), 0)
        self._cpu_pct = 0.0
        self._rate = 0.0
        self.baseline: tuple[float | None, int | None] | None = None  # (ws_mb, gdi)
        self.latest: tuple[float | None, int | None] = (None, None)
        self._ws_history: deque[float] = deque(maxlen=self.CLIMB)

    def record(self, seconds: float, cpu_seconds: float | None = None) -> None:
        """``seconds`` is wall time (latency: includes waiting for the GPU
        inside BitBlt, ~15 ms here); ``cpu_seconds`` is what the capture
        actually cost this process, and is what the safety valve judges."""
        self._durations.append(seconds)
        self._cpu.append(seconds if cpu_seconds is None else cpu_seconds)
        self._count += 1
        wall0, cpu0, count0 = self._window_start
        wall = time.monotonic()
        if wall - wall0 >= 5.0:
            self._cpu_pct = (time.process_time() - cpu0) / (wall - wall0) * 100
            self._rate = (self._count - count0) / (wall - wall0)
            self._window_start = (wall, time.process_time(), self._count)
        if self._count % self.sample_every == 1:
            self._sample(working_set_mb(), gdi_handles())

    def _sample(self, ws_mb: float | None, gdi: int | None) -> None:
        self._samples += 1
        self.latest = (ws_mb, gdi)
        if ws_mb is not None:
            self._ws_history.append(ws_mb)
        if self.baseline is None and self._samples >= self.BASELINE_SAMPLE:
            self.baseline = self.latest

    def inject(self, ws_mb: float | None, gdi: int | None) -> None:
        """For tests: pretend a sample read these values."""
        self._sample(ws_mb, gdi)

    @property
    def avg_ms(self) -> float:
        return sum(self._durations) / len(self._durations) * 1000 if self._durations else 0.0

    @property
    def avg_cpu_ms(self) -> float:
        return sum(self._cpu) / len(self._cpu) * 1000 if self._cpu else 0.0

    def snapshot(self) -> dict:
        ws_mb, gdi = self.latest
        return {"avg_ms": round(self.avg_ms, 2), "avg_cpu_ms": round(self.avg_cpu_ms, 2),
                "cpu_pct": round(self._cpu_pct, 2), "rate_hz": round(self._rate, 1),
                "ws_mb": ws_mb, "gdi": gdi, "captures": self._count}

    def should_stop(self) -> str | None:
        if len(self._cpu) >= self.WARMUP and self.avg_cpu_ms > self.max_avg_ms:
            return f"screen reading is costing too much here ({self.avg_cpu_ms:.0f} ms of CPU per capture)"
        if self.baseline:
            ws0, gdi0 = self.baseline
            ws, gdi = self.latest
            if gdi0 is not None and gdi is not None and gdi - gdi0 > self.max_gdi_growth:
                return f"GDI handles grew by {gdi - gdi0} while reading"
            if ws0 is not None and ws is not None and ws - ws0 > self.max_growth_mb:
                history = list(self._ws_history)
                climbing = (len(history) == self.CLIMB
                            and all(b > a for a, b in zip(history, history[1:])))
                if climbing:
                    return f"memory keeps climbing while reading (+{ws - ws0:.0f} MB)"
        return None


def working_set_mb() -> float | None:
    if sys.platform != "win32":
        return None
    counters = _PROCESS_MEMORY_COUNTERS()
    counters.cb = ctypes.sizeof(counters)
    if not _psapi.GetProcessMemoryInfo(_kernel32.GetCurrentProcess(),
                                       ctypes.byref(counters), counters.cb):
        return None
    return counters.WorkingSetSize / (1024 * 1024)


def gdi_handles() -> int | None:
    if sys.platform != "win32":
        return None
    return _user32.GetGuiResources(_kernel32.GetCurrentProcess(), 0)  # GR_GDIOBJECTS


# ------------------------------------------------------------------ capture (Windows)

if sys.platform == "win32":
    from ctypes import wintypes

    _user32 = ctypes.WinDLL("user32", use_last_error=True)
    _gdi32 = ctypes.WinDLL("gdi32", use_last_error=True)
    _kernel32 = ctypes.WinDLL("kernel32", use_last_error=True)
    _psapi = ctypes.WinDLL("psapi", use_last_error=True)

    class _BITMAPINFOHEADER(ctypes.Structure):
        _fields_ = [
            ("biSize", wintypes.DWORD), ("biWidth", wintypes.LONG), ("biHeight", wintypes.LONG),
            ("biPlanes", wintypes.WORD), ("biBitCount", wintypes.WORD),
            ("biCompression", wintypes.DWORD), ("biSizeImage", wintypes.DWORD),
            ("biXPelsPerMeter", wintypes.LONG), ("biYPelsPerMeter", wintypes.LONG),
            ("biClrUsed", wintypes.DWORD), ("biClrImportant", wintypes.DWORD),
        ]

    class _PROCESS_MEMORY_COUNTERS(ctypes.Structure):
        _fields_ = [
            ("cb", wintypes.DWORD), ("PageFaultCount", wintypes.DWORD),
            ("PeakWorkingSetSize", ctypes.c_size_t), ("WorkingSetSize", ctypes.c_size_t),
            ("QuotaPeakPagedPoolUsage", ctypes.c_size_t), ("QuotaPagedPoolUsage", ctypes.c_size_t),
            ("QuotaPeakNonPagedPoolUsage", ctypes.c_size_t),
            ("QuotaNonPagedPoolUsage", ctypes.c_size_t),
            ("PagefileUsage", ctypes.c_size_t), ("PeakPagefileUsage", ctypes.c_size_t),
        ]

    def _api(fn, restype, *argtypes):
        fn.restype, fn.argtypes = restype, argtypes
        return fn

    FindWindowW = _api(_user32.FindWindowW, wintypes.HWND, wintypes.LPCWSTR, wintypes.LPCWSTR)
    IsIconic = _api(_user32.IsIconic, wintypes.BOOL, wintypes.HWND)
    IsWindow = _api(_user32.IsWindow, wintypes.BOOL, wintypes.HWND)
    GetForegroundWindow = _api(_user32.GetForegroundWindow, wintypes.HWND)
    GetClientRect = _api(_user32.GetClientRect, wintypes.BOOL, wintypes.HWND,
                         ctypes.POINTER(wintypes.RECT))
    ClientToScreen = _api(_user32.ClientToScreen, wintypes.BOOL, wintypes.HWND,
                          ctypes.POINTER(wintypes.POINT))
    GetDC = _api(_user32.GetDC, wintypes.HDC, wintypes.HWND)
    ReleaseDC = _api(_user32.ReleaseDC, ctypes.c_int, wintypes.HWND, wintypes.HDC)
    GetGuiResources = _api(_user32.GetGuiResources, wintypes.DWORD, wintypes.HANDLE, wintypes.DWORD)
    GetCurrentProcess = _api(_kernel32.GetCurrentProcess, wintypes.HANDLE)
    GetProcessMemoryInfo = _api(_psapi.GetProcessMemoryInfo, wintypes.BOOL, wintypes.HANDLE,
                                ctypes.POINTER(_PROCESS_MEMORY_COUNTERS), wintypes.DWORD)
    CreateCompatibleDC = _api(_gdi32.CreateCompatibleDC, wintypes.HDC, wintypes.HDC)
    CreateDIBSection = _api(_gdi32.CreateDIBSection, wintypes.HBITMAP, wintypes.HDC,
                            ctypes.POINTER(_BITMAPINFOHEADER), wintypes.UINT,
                            ctypes.POINTER(ctypes.c_void_p), wintypes.HANDLE, wintypes.DWORD)
    SelectObject = _api(_gdi32.SelectObject, wintypes.HGDIOBJ, wintypes.HDC, wintypes.HGDIOBJ)
    DeleteObject = _api(_gdi32.DeleteObject, wintypes.BOOL, wintypes.HGDIOBJ)
    DeleteDC = _api(_gdi32.DeleteDC, wintypes.BOOL, wintypes.HDC)
    BitBlt = _api(_gdi32.BitBlt, wintypes.BOOL, wintypes.HDC, ctypes.c_int, ctypes.c_int,
                  ctypes.c_int, ctypes.c_int, wintypes.HDC, ctypes.c_int, ctypes.c_int,
                  wintypes.DWORD)

    SRCCOPY = 0x00CC0020

    def find_wow_window() -> int | None:
        for cls in WOW_WINDOW_CLASSES:
            hwnd = FindWindowW(cls, None)
            if hwnd:
                return hwnd
        return None


@dataclass
class Grab:
    buf: memoryview      # 2 * BAND_ROWS rows of BGRA: top band, then bottom band
    width: int
    stride: int
    band_rows: int = BAND_ROWS


class Capture:
    """Copies the top and bottom bands of a window's client area into one
    reusable 32-bit DIB section. Only ever those two bands.

    ``window`` after each ``grab``: "none", "minimized", "background" or
    "foreground". By default nothing is copied unless the window is in the
    foreground (reading what another window covers isn't possible with a
    plain screen copy anyway)."""

    def __init__(self, find_window=None, require_foreground: bool = True):
        self._find = find_window or find_wow_window
        self.require_foreground = require_foreground
        self.window = "none"
        self._hwnd = None
        self._screen = GetDC(None)
        self._memdc = CreateCompatibleDC(self._screen)
        self._bitmap = None
        self._old_bitmap = None
        self._buf: memoryview | None = None
        self._width = 0

    def grab(self) -> Grab | None:
        hwnd = self._hwnd if self._hwnd and IsWindow(self._hwnd) else self._find()
        self._hwnd = hwnd
        if not hwnd:
            self.window = "none"
            return None
        if IsIconic(hwnd):
            self.window = "minimized"
            return None
        if GetForegroundWindow() != hwnd:
            self.window = "background"
            if self.require_foreground:
                return None
        else:
            self.window = "foreground"

        rect = wintypes.RECT()
        origin = wintypes.POINT(0, 0)
        if not GetClientRect(hwnd, ctypes.byref(rect)) or not ClientToScreen(hwnd, ctypes.byref(origin)):
            return None
        width, height = rect.right - rect.left, rect.bottom - rect.top
        if width < 1 or height < 2 * BAND_ROWS:
            return None
        if width != self._width:
            self._allocate(width)

        ok = BitBlt(self._memdc, 0, 0, width, BAND_ROWS,
                    self._screen, origin.x, origin.y, SRCCOPY)
        ok = BitBlt(self._memdc, 0, BAND_ROWS, width, BAND_ROWS,
                    self._screen, origin.x, origin.y + height - BAND_ROWS, SRCCOPY) and ok
        if not ok:
            return None
        return Grab(self._buf, width, width * 4)

    def _allocate(self, width: int) -> None:
        self._release_bitmap()
        info = _BITMAPINFOHEADER()
        info.biSize = ctypes.sizeof(info)
        info.biWidth = width
        info.biHeight = -2 * BAND_ROWS  # negative: top-down rows
        info.biPlanes = 1
        info.biBitCount = 32
        bits = ctypes.c_void_p()
        bitmap = CreateDIBSection(self._screen, ctypes.byref(info), 0, ctypes.byref(bits), None, 0)
        if not bitmap or not bits.value:
            raise OSError(ctypes.get_last_error(), "CreateDIBSection failed")
        size = width * 4 * 2 * BAND_ROWS
        self._buf = memoryview((ctypes.c_ubyte * size).from_address(bits.value)).cast("B")
        self._bitmap = bitmap
        self._old_bitmap = SelectObject(self._memdc, bitmap)
        self._width = width

    def _release_bitmap(self) -> None:
        if self._bitmap:
            if self._old_bitmap:
                SelectObject(self._memdc, self._old_bitmap)
            DeleteObject(self._bitmap)
        self._bitmap = self._old_bitmap = self._buf = None
        self._width = 0

    def close(self) -> None:
        self._release_bitmap()
        if self._memdc:
            DeleteDC(self._memdc)
            self._memdc = None
        if self._screen:
            ReleaseDC(None, self._screen)
            self._screen = None

    def __del__(self):
        try:
            self.close()
        except Exception:
            pass


# ------------------------------------------------------------------ reader

class Reader:
    """Capture + decode + assemble, with the meter around every poll.

    Diagnostics: ``trace`` collects (time, reason) for candidates that
    looked like a strip but failed a check, newest last. With the
    ``DOINK_DUMP_BANDS`` environment variable set to a directory, every
    band that fails that way is written there as a raw BGRA ``.bin`` (at
    most ``DUMP_MAX`` files) so the capture can be replayed offline."""

    DUMP_MAX = 20

    def __init__(self, capture=None, meter: ResourceMeter | None = None):
        self.capture = capture or Capture()
        self.meter = meter or ResourceMeter()
        self.assembler = Assembler()
        self.strip: tuple[float, int] | None = None  # (block_px, blocks) last decoded
        self.last_seen: float | None = None
        self.trace: deque[tuple[float, str]] = deque(maxlen=50)
        self._dump_dir = os.environ.get("DOINK_DUMP_BANDS")
        self._dumped = 0

    @property
    def window(self) -> str:
        return self.capture.window

    def poll(self) -> list[bytes]:
        started = time.perf_counter()
        cpu_started = time.process_time()
        messages = []
        try:
            grab = self.capture.grab()
            if grab is not None:
                for band_top, bottom in ((0, False), (grab.band_rows, True)):
                    reasons: list[str] = []
                    chunk = decode(grab.buf, grab.width, grab.stride, band_top, grab.band_rows,
                                   bottom, reasons)
                    if chunk is None:
                        if reasons:
                            now = time.time()
                            self.trace.extend((now, r) for r in reasons)
                            self._dump(grab, band_top, bottom)
                        continue
                    self.strip = (round(chunk.block_px, 2), chunk.blocks)
                    self.last_seen = time.time()
                    message = self.assembler.add(chunk)
                    if message is not None:
                        messages.append(message)
                    break
        finally:
            self.meter.record(time.perf_counter() - started, time.process_time() - cpu_started)
        return messages

    def _dump(self, grab: "Grab", band_top: int, bottom: bool) -> None:
        if not self._dump_dir or self._dumped >= self.DUMP_MAX:
            return
        try:
            os.makedirs(self._dump_dir, exist_ok=True)
            name = (f"band_{time.strftime('%H%M%S')}_{self._dumped:02d}_"
                    f"{grab.width}x{grab.band_rows}_{'bottom' if bottom else 'top'}.bin")
            start = band_top * grab.stride
            with open(os.path.join(self._dump_dir, name), "wb") as f:
                f.write(grab.buf[start:start + grab.band_rows * grab.stride])
            self._dumped += 1
        except OSError:
            pass

    def close(self) -> None:
        self.capture.close()
