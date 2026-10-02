"""The pixel decoder, against a Python reference encoder and the Lua fixture."""

import random
import unittest
from pathlib import Path

from doink import pixel
from doink.parser import decode_message
from doink.pixel import Assembler, Chunk, ResourceMeter, crc8, crc16, decode

FIXTURE = Path(__file__).resolve().parents[2] / "addon" / "tests" / "fixtures" / "pixel_levelup.txt"
SCENE = 90  # what the game draws around the strip in these tests


def encode_chunks(blocks: int, msg_id: int, payload: bytes) -> list[list[str]]:
    """Reference encoder: rows of '0'/'1' per chunk, as Transports/Pixel.lua lays them out."""
    cap = pixel.capacity(blocks)
    count = max(1, -(-len(payload) // cap))
    chunks = []
    for i in range(count):
        part = payload[i * cap:(i + 1) * cap]
        header = bytes([pixel.VERSION << 4, blocks >> 8, blocks & 0xFF, msg_id >> 8, msg_id & 0xFF,
                        i, count, len(part), crc16(part) >> 8, crc16(part) & 0xFF])
        header += bytes([crc8(header)])
        row0 = (pixel.SYNC + "".join(f"{b:08b}" for b in header)).ljust(blocks, "0")
        data = "".join(f"{b:08b}" for b in part)
        rows = [row0] + [data[r * blocks:(r + 1) * blocks].ljust(blocks, "0")
                         for r in range(pixel.ROWS - 1)]
        chunks.append(rows)
    return chunks


def render(rows: list[str], block: float, width: int, x0: int = 0, white: int = 255,
           black: int = 0, bottom: bool = False, band_rows: int = pixel.BAND_ROWS) -> bytearray:
    """One band of BGRA pixels with the strip drawn at x0 (top- or bottom-aligned)."""
    buf = bytearray([SCENE, SCENE, SCENE, 255] * (width * band_rows))
    strip_h = pixel.ROWS * block
    top = band_rows - strip_h if bottom else 0
    for y in range(band_rows):
        rel_y = y - top
        if rel_y < 0 or rel_y >= strip_h:
            continue
        r = int(rel_y // block)
        for x in range(width):
            rel_x = x - x0
            if rel_x < 0:
                continue
            c = int(rel_x // block)
            if c >= len(rows[r]):
                continue
            v = white if rows[r][c] == "1" else black
            i = (y * width + x) * 4
            buf[i:i + 3] = bytes([v, v, v])
    return buf


def soften(buf: bytearray, width: int, band_rows: int) -> None:
    """Average horizontally across every block edge: the grey fringe you get
    when the strip isn't pixel-aligned."""
    for y in range(band_rows):
        row = y * width * 4
        values = [buf[row + x * 4] for x in range(width)]
        for x in range(1, width):
            if values[x] != values[x - 1]:
                v = (values[x] + values[x - 1]) // 2
                buf[row + x * 4:row + x * 4 + 3] = bytes([v, v, v])


def decode_band(buf, width, bottom=False, band_rows=pixel.BAND_ROWS):
    return decode(buf, width, width * 4, 0, band_rows, bottom)


class CrcTest(unittest.TestCase):
    def test_check_values(self):
        self.assertEqual(crc16(b"123456789"), 0x29B1)  # CRC-16/CCITT-FALSE
        self.assertEqual(crc8(b"123456789"), 0xF4)     # CRC-8


class DecodeTest(unittest.TestCase):
    PAYLOAD = b'{"char":"Paul","realm":"R","seq":7,"type":"level_up","data":{"level":10}}'

    def roundtrip(self, block, width=1300, x0=0, **kw):
        chunks = encode_chunks(400, 0xBEEF, self.PAYLOAD)
        self.assertEqual(len(chunks), 1)
        buf = render(chunks[0], block, width, x0, **kw)
        return decode_band(buf, width, bottom=kw.get("bottom", False))

    def test_block_sizes_and_positions(self):
        for block in (2, 3, 4, 6, 8):
            for x0 in (0, 37, 1300 - 400 * block):
                if x0 < 0:
                    continue
                width = max(1300, x0 + 400 * block)
                chunk = self.roundtrip(block, width=width, x0=x0)
                self.assertIsNotNone(chunk, f"block {block} at x0 {x0}")
                self.assertEqual(chunk.payload, self.PAYLOAD)
                self.assertEqual((chunk.msg_id, chunk.index, chunk.count, chunk.blocks),
                                 (0xBEEF, 0, 1, 400))
                self.assertAlmostEqual(chunk.block_px, block, delta=0.15)

    def test_fractional_block_with_soft_edges(self):
        chunks = encode_chunks(400, 1, self.PAYLOAD)
        width = 1500
        buf = render(chunks[0], 3.5, width, x0=11)
        soften(buf, width, pixel.BAND_ROWS)
        chunk = decode_band(buf, width)
        self.assertIsNotNone(chunk)
        self.assertEqual(chunk.payload, self.PAYLOAD)
        self.assertAlmostEqual(chunk.block_px, 3.5, delta=0.1)

    def test_low_contrast_and_offset_levels(self):
        chunk = self.roundtrip(3, white=160, black=40)
        self.assertEqual(chunk.payload, self.PAYLOAD)
        self.assertIsNone(self.roundtrip(3, white=60, black=40), "too little contrast")

    def test_bottom_band(self):
        chunk = self.roundtrip(3, x0=20, bottom=True)
        self.assertIsNotNone(chunk)
        self.assertEqual(chunk.payload, self.PAYLOAD)

    def test_no_strip(self):
        width = 800
        flat = bytearray([SCENE, SCENE, SCENE, 255] * (width * pixel.BAND_ROWS))
        self.assertIsNone(decode_band(flat, width))
        rng = random.Random(1)
        noise = bytearray(rng.randrange(256) for _ in range(width * pixel.BAND_ROWS * 4))
        self.assertIsNone(decode_band(noise, width))
        self.assertIsNone(decode_band(noise, width, bottom=True))

    def test_corruption_is_rejected(self):
        chunks = encode_chunks(400, 2, self.PAYLOAD)
        width = 1300
        clean = render(chunks[0], 3, width)
        self.assertIsNotNone(decode_band(clean, width))
        # A block in the payload area flipped: payload crc fails.
        damaged = bytearray(clean)
        y, x = 4, 50 * 3 + 1  # row 1, block 50
        i = (y * width + x) * 4
        damaged[i:i + 3] = bytes([255 - damaged[i]] * 3)
        self.assertIsNone(decode_band(damaged, width))
        # A header block flipped: header crc fails.
        damaged = bytearray(clean)
        x = 40 * 3 + 1  # row 0, a header block
        i = (1 * width + x) * 4
        damaged[i:i + 3] = bytes([255 - damaged[i]] * 3)
        self.assertIsNone(decode_band(damaged, width))

    def test_multi_chunk_message_decodes_each_chunk(self):
        payload = bytes(range(256)) * 2  # 512 bytes -> 6 chunks of 100
        chunks = encode_chunks(400, 9, payload)
        self.assertEqual(len(chunks), 6)
        parts = []
        for rows in chunks:
            chunk = decode_band(render(rows, 3, 1300), 1300)
            self.assertIsNotNone(chunk)
            parts.append(chunk.payload)
        self.assertEqual(b"".join(parts), payload)


class FixtureTest(unittest.TestCase):
    """The committed fixture was written by the Lua encoder; both the Python
    reference encoder and the decoder must agree with it."""

    def load(self):
        lines = FIXTURE.read_text(encoding="utf-8").splitlines()
        meta = {}
        chunks, current = [], None
        for line in lines:
            if line.startswith("chunk "):
                current = []
                chunks.append(current)
            elif current is not None:
                current.append(line)
            else:
                key, _, value = line.partition(" ")
                meta[key] = value
        return meta, chunks

    def test_lua_and_python_encoders_agree(self):
        meta, chunks = self.load()
        expected = encode_chunks(int(meta["blocks"]), int(meta["msg_id"]),
                                 meta["payload"].encode("utf-8"))
        self.assertEqual(chunks, expected)

    def test_decoder_reads_the_lua_output(self):
        meta, chunks = self.load()
        assembler = Assembler()
        message = None
        for rows in chunks:
            chunk = decode_band(render(rows, 3, 1300), 1300)
            self.assertIsNotNone(chunk)
            self.assertEqual(chunk.msg_id, int(meta["msg_id"]))
            message = assembler.add(chunk) or message
        self.assertEqual(message, meta["payload"].encode("utf-8"))
        event = decode_message(message)
        self.assertEqual((event["char"], event["surname"], event["seq"]), ("Paul", "Hebbs", 3))


class AssemblerTest(unittest.TestCase):
    def chunk(self, msg_id, index, count, payload=b"x"):
        return Chunk(msg_id, index, count, payload, 400, 3.0)

    def test_out_of_order_and_once_only(self):
        a = Assembler()
        self.assertIsNone(a.add(self.chunk(1, 1, 2, b"B")))
        self.assertEqual(a.add(self.chunk(1, 0, 2, b"A")), b"AB")
        # The strip repeats every message; repeats must not re-emit.
        self.assertIsNone(a.add(self.chunk(1, 0, 2, b"A")))
        self.assertIsNone(a.add(self.chunk(1, 1, 2, b"B")))

    def test_single_chunk_and_count_change(self):
        a = Assembler()
        self.assertEqual(a.add(self.chunk(5, 0, 1, b"solo")), b"solo")
        self.assertIsNone(a.add(self.chunk(6, 0, 3, b"a")))
        self.assertIsNone(a.add(self.chunk(6, 0, 2, b"a")))  # id reused with a new shape
        self.assertEqual(a.add(self.chunk(6, 1, 2, b"b")), b"ab")

    def test_partial_messages_expire(self):
        a = Assembler(ttl=10)
        self.assertIsNone(a.add(self.chunk(7, 0, 2, b"a"), now=0))
        self.assertEqual(a.add(self.chunk(8, 0, 1, b"z"), now=20), b"z")  # triggers expiry
        self.assertIsNone(a.add(self.chunk(7, 1, 2, b"b"), now=21))  # first half is gone


class MeterTest(unittest.TestCase):
    def test_expensive_captures_stop_after_warmup(self):
        m = ResourceMeter(max_avg_ms=25)
        for _ in range(ResourceMeter.WARMUP - 1):
            m.record(0.100, cpu_seconds=0.100)
        self.assertIsNone(m.should_stop(), "not before warm-up")
        m.record(0.100, cpu_seconds=0.100)
        self.assertIn("costing too much", m.should_stop())

    def test_waiting_on_the_gpu_is_not_cost(self):
        m = ResourceMeter(max_avg_ms=25)
        for _ in range(ResourceMeter.WARMUP + 5):
            m.record(0.050, cpu_seconds=0.001)  # 50 ms wall, 1 ms CPU: BitBlt waiting
        self.assertIsNone(m.should_stop())
        self.assertAlmostEqual(m.snapshot()["avg_cpu_ms"], 1.0, places=1)

    def test_handle_growth_stops(self):
        m = ResourceMeter(max_gdi_growth=50)
        for _ in range(ResourceMeter.BASELINE_SAMPLE):
            m.inject(40.0, 100)
        self.assertIsNone(m.should_stop())
        m.inject(40.0, 151)
        self.assertIn("GDI", m.should_stop())

    def test_memory_step_is_tolerated_but_a_steady_climb_stops(self):
        m = ResourceMeter(max_growth_mb=100)
        for _ in range(ResourceMeter.BASELINE_SAMPLE):
            m.inject(45.0, 54)
        # The settings window opens: +80 MB in one step, then flat.
        for _ in range(8):
            m.inject(125.0, 54)
            self.assertIsNone(m.should_stop(), "a one-off step is not a leak")
        # Then it climbs 5 samples in a row past the limit: that is one.
        for ws in (130.0, 136.0, 142.0, 150.0, 158.0):
            m.inject(ws, 54)
        self.assertIn("climbing", m.should_stop())

    def test_baseline_waits_for_startup(self):
        m = ResourceMeter()
        m.inject(30.0, 50)
        m.inject(60.0, 50)
        self.assertIsNone(m.baseline, "first samples are start-up noise")
        m.inject(90.0, 50)
        self.assertEqual(m.baseline, (90.0, 50))

    def test_snapshot_fields(self):
        m = ResourceMeter()
        m.record(0.002)
        snap = m.snapshot()
        self.assertEqual(snap["captures"], 1)
        self.assertAlmostEqual(snap["avg_ms"], 2.0, places=1)
        for key in ("cpu_pct", "rate_hz", "ws_mb", "gdi"):
            self.assertIn(key, snap)


class DecodeMessageTest(unittest.TestCase):
    def test_hello_event_and_garbage(self):
        hello = decode_message(b'{"type":"hello","char":"Paul","realm":"R","addon":"0.6.0"}')
        self.assertEqual(hello["type"], "hello")
        event = decode_message(b'{"char":"Paul","realm":"R","seq":1,"type":"death","data":{}}')
        self.assertEqual(event["seq"], 1)
        self.assertIsNone(decode_message(b'{"type":"hello"}'))        # no char/realm
        self.assertIsNone(decode_message(b'{"char":"Paul","realm":"R"}'))  # no seq/type
        self.assertIsNone(decode_message(b"\xff\xfe not json"))


if __name__ == "__main__":
    unittest.main()
