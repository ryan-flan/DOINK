"""Draws doink.ico: a gold "D" on a dark rounded square.

Standard library only. Re-run after changing the design:
    cd companion && python3 assets/make_icon.py
"""

import struct
import zlib
from pathlib import Path

BG = (0x1E, 0x21, 0x24)   # dark slate
FG = (0xF2, 0xB8, 0x3C)   # gold
SUPERSAMPLE = 4           # samples per pixel per axis, for smooth edges
SIZES = (16, 24, 32, 48, 256)


def in_background(u: float, v: float) -> bool:
    """Rounded square filling the canvas."""
    inset, radius = 0.03, 0.22
    half = 0.5 - inset - radius
    dx = max(abs(u - 0.5) - half, 0.0)
    dy = max(abs(v - 0.5) - half, 0.0)
    return dx * dx + dy * dy <= radius * radius


def in_letter(u: float, v: float) -> bool:
    """A "D": a half-ellipse with a flat left side, minus a smaller one."""
    x0, cy, rx, ry, stroke = 0.29, 0.5, 0.44, 0.33, 0.13
    outer = u >= x0 and ((u - x0) / rx) ** 2 + ((v - cy) / ry) ** 2 <= 1
    inner = u >= x0 + stroke and \
        ((u - x0) / (rx - stroke)) ** 2 + ((v - cy) / (ry - stroke)) ** 2 <= 1
    return outer and not inner


def render(size: int) -> list[list[tuple[int, int, int, int]]]:
    """Rows top to bottom of (r, g, b, a)."""
    n = SUPERSAMPLE
    rows = []
    for py in range(size):
        row = []
        for px in range(size):
            bg = fg = 0
            for sy in range(n):
                for sx in range(n):
                    u = (px + (sx + 0.5) / n) / size
                    v = (py + (sy + 0.5) / n) / size
                    if in_background(u, v):
                        bg += 1
                        fg += in_letter(u, v)
            t = fg / bg if bg else 0.0
            colour = tuple(round(b + (f - b) * t) for b, f in zip(BG, FG))
            row.append((*colour, round(255 * bg / (n * n))))
        rows.append(row)
    return rows


def bmp_entry(rows) -> bytes:
    """32-bit BMP icon image: header, bottom-up BGRA, empty AND mask."""
    size = len(rows)
    header = struct.pack("<IiiHHIIiiII", 40, size, size * 2, 1, 32, 0, 0, 0, 0, 0, 0)
    pixels = b"".join(bytes((b, g, r, a)) for row in reversed(rows) for r, g, b, a in row)
    mask = bytes(((size + 31) // 32) * 4 * size)
    return header + pixels + mask


def png_entry(rows) -> bytes:
    """PNG image, the standard encoding for 256px icon entries."""
    def chunk(kind: bytes, data: bytes) -> bytes:
        return (struct.pack(">I", len(data)) + kind + data
                + struct.pack(">I", zlib.crc32(kind + data)))
    size = len(rows)
    raw = b"".join(b"\0" + bytes(c for px in row for c in px) for row in rows)
    return (b"\x89PNG\r\n\x1a\n"
            + chunk(b"IHDR", struct.pack(">IIBBBBB", size, size, 8, 6, 0, 0, 0))
            + chunk(b"IDAT", zlib.compress(raw, 9))
            + chunk(b"IEND", b""))


def main() -> None:
    images = [png_entry(render(s)) if s >= 256 else bmp_entry(render(s)) for s in SIZES]
    out = struct.pack("<HHH", 0, 1, len(images))
    offset = 6 + 16 * len(images)
    for size, data in zip(SIZES, images):
        dim = 0 if size >= 256 else size  # 0 means 256 in an icon directory
        out += struct.pack("<BBBBHHII", dim, dim, 0, 0, 1, 32, len(data), offset)
        offset += len(data)
    out += b"".join(images)
    path = Path(__file__).with_name("doink.ico")
    path.write_bytes(out)
    print(f"wrote {path} ({len(out):,} bytes)")

    # Tk can't show .ico in a window body; the settings header uses this.
    png = Path(__file__).with_name("doink-48.png")
    png.write_bytes(png_entry(render(48)))
    print(f"wrote {png}")

    # CurseForge project logo (not bundled with the app).
    logo = Path(__file__).resolve().parents[2] / "docs" / "logo-400.png"
    logo.write_bytes(png_entry(render(400)))
    print(f"wrote {logo}")


if __name__ == "__main__":
    main()
