package shot

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"math/rand"
	"os"
	"strings"
	"testing"
)

// The addon's encoder writes this file (addon/tests/test_addon.lua); both
// sides assert it, so the encoders can't drift apart.
const fixture = "../../../addon/tests/fixtures/shot_levelup.txt"

func readFixture(t *testing.T) (string, [][]uint8) {
	t.Helper()
	raw, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	payload := strings.TrimPrefix(lines[0], "payload ")
	var rows [][]uint8
	for _, line := range lines[1:] {
		row := make([]uint8, len(line))
		for i, ch := range line {
			row[i] = uint8(ch - '0')
		}
		rows = append(rows, row)
	}
	return payload, rows
}

// screen is a noisy 2554x1404 screenshot (the retail test size) with rows
// drawn at (ox, oy), b pixels per block (fractional b by nearest block).
func screen(rows [][]uint8, ox, oy int, b float64) *image.RGBA {
	r := rand.New(rand.NewSource(1))
	img := image.NewRGBA(image.Rect(0, 0, 2554, 1404))
	for i := range img.Pix {
		img.Pix[i] = uint8(r.Intn(256))
	}
	cols := len(rows[0])
	w := int(math.Ceil(float64(cols+1) * b))
	h := int(math.Ceil(float64(len(rows)+1) * b))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			rr, c := int(float64(y)/b), int(float64(x)/b)
			v := uint8(0)
			if rr < len(rows) && c < cols && rows[rr][c] == 1 {
				v = 255
			}
			img.Set(ox+x, oy+y, color.RGBA{v, v, v, 255})
		}
	}
	return img
}

func jpegRoundTrip(t testing.TB, img image.Image, quality int) image.Image {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		t.Fatal(err)
	}
	out, err := jpeg.Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCRCCheckValues(t *testing.T) {
	if got := crc16([]byte("123456789")); got != 0x29B1 {
		t.Errorf("crc16 = %#x", got)
	}
	if got := crc8([]byte("123456789")); got != 0xF4 {
		t.Errorf("crc8 = %#x", got)
	}
}

func TestRowsMatchTheAddon(t *testing.T) {
	payload, want := readFixture(t)
	got, err := Rows([]byte(payload), 0, len(want[0]))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("%d rows, want %d", len(got), len(want))
	}
	for r := range want {
		if !bytes.Equal(got[r], want[r]) {
			t.Fatalf("row %d differs:\n got %v\nwant %v", r, got[r], want[r])
		}
	}
}

func TestRowsTooLong(t *testing.T) {
	if rows, err := Rows(make([]byte, MaxPayload), 0, MinCols); err != nil || len(rows) != 1+MaxRows {
		t.Errorf("1024 bytes at the narrowest: %d rows, %v", len(rows), err)
	}
	if _, err := Rows(make([]byte, MaxPayload+1), 0, MaxCols); !errors.Is(err, ErrTooLong) {
		t.Errorf("1025 bytes: %v", err)
	}
}

func TestDecode(t *testing.T) {
	payload, rows := readFixture(t)
	cases := []struct {
		name string
		img  func(t *testing.T) image.Image
	}{
		{"4 px at the corner", func(*testing.T) image.Image { return screen(rows, 0, 0, 4) }},
		{"jpeg quality 80", func(t *testing.T) image.Image { return jpegRoundTrip(t, screen(rows, 0, 0, 4), 80) }},
		{"jpeg quality 50", func(t *testing.T) image.Image { return jpegRoundTrip(t, screen(rows, 0, 0, 4), 50) }},
		{"jpeg quality 20", func(t *testing.T) image.Image { return jpegRoundTrip(t, screen(rows, 0, 0, 4), 20) }},
		{"offset (3,2)", func(*testing.T) image.Image { return screen(rows, 3, 2, 4) }},
		{"3 px", func(*testing.T) image.Image { return screen(rows, 0, 0, 3) }},
		{"2 px", func(*testing.T) image.Image { return screen(rows, 0, 0, 2) }},
		{"scaled to 4.5 px", func(*testing.T) image.Image { return screen(rows, 0, 0, 4.5) }},
		{"scaled to 4.5 px, jpeg 80", func(t *testing.T) image.Image { return jpegRoundTrip(t, screen(rows, 1, 1, 4.5), 80) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, ok := Decode(c.img(t))
			if !ok {
				t.Fatal("not found")
			}
			if string(m.Payload) != payload || m.Hello {
				t.Errorf("got %q hello=%v", m.Payload, m.Hello)
			}
		})
	}
}

func TestWidths(t *testing.T) {
	payload := []byte(strings.Repeat("loot! ", 60))
	for _, cols := range []int{MinCols, 959, MaxCols} {
		rows, err := Rows(payload, 0, cols)
		if err != nil {
			t.Fatal(err)
		}
		img := jpegRoundTrip(t, screen(rows, 0, 0, 2), 50)
		if m, ok := Decode(img); !ok || !bytes.Equal(m.Payload, payload) {
			t.Errorf("cols %d (%d rows): not decoded", cols, len(rows))
		}
	}
}

func TestDecodeRender(t *testing.T) {
	payload, rows := readFixture(t)
	m, ok := Decode(Render(rows, 4))
	if !ok || string(m.Payload) != payload {
		t.Fatalf("Render didn't round-trip: %v %q", ok, m.Payload)
	}
}

func TestHelloRoundTrips(t *testing.T) {
	rows, _ := Rows([]byte(`{"type":"hello"}`), FlagHello, MinCols)
	m, ok := Decode(screen(rows, 0, 0, 4))
	if !ok || !m.Hello || string(m.Payload) != `{"type":"hello"}` {
		t.Errorf("got %v %+v", ok, m)
	}
}

func TestDamagedGridIsRejected(t *testing.T) {
	_, rows := readFixture(t)
	flip := func(r, c int) [][]uint8 {
		out := make([][]uint8, len(rows))
		for i := range rows {
			out[i] = append([]uint8{}, rows[i]...)
		}
		out[r][c] ^= 1
		return out
	}
	if _, ok := Decode(screen(flip(2, 10), 0, 0, 4)); ok {
		t.Error("flipped payload bit accepted")
	}
	if _, ok := Decode(screen(flip(0, 30), 0, 0, 4)); ok {
		t.Error("flipped header bit accepted")
	}
}

func TestNoGrid(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	noise := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	for i := range noise.Pix {
		noise.Pix[i] = uint8(r.Intn(256))
	}
	for name, img := range map[string]image.Image{
		"noise": noise,
		"blank": image.NewGray(image.Rect(0, 0, 1280, 720)),
		"tiny":  image.NewGray(image.Rect(0, 0, 10, 10)),
		"empty": image.NewGray(image.Rect(0, 0, 0, 0)),
	} {
		if _, ok := Decode(img); ok {
			t.Errorf("%s: found a grid", name)
		}
	}
}

func BenchmarkDecodeNoGrid(b *testing.B) {
	img := jpegRoundTrip(b, image.NewGray(image.Rect(0, 0, 2554, 1404)), 80)
	for range b.N {
		Decode(img)
	}
}

func TestAttachFlagAndStrip(t *testing.T) {
	payload := []byte(`{"type":"level_up","seq":1}`)
	rows, err := Rows(payload, FlagImage, 959)
	if err != nil {
		t.Fatal(err)
	}
	// The grid is drawn at y=2 and its backing ends at 2 + (len(rows)+1)*4.
	// The offset sweep may settle 1 px early (a 2 px sample still sits
	// inside a 4 px block), so Strip is that plus a margin of 1 or 2.
	backing := 2 + (len(rows)+1)*4
	img := screen(rows, 3, 2, 4)
	for name, in := range map[string]image.Image{"exact": img, "jpeg": jpegRoundTrip(t, img, 80)} {
		m, ok := Decode(in)
		if !ok {
			t.Fatalf("%s: no grid found", name)
		}
		if !m.Attach || m.Hello {
			t.Errorf("%s: Attach=%v Hello=%v, want true false", name, m.Attach, m.Hello)
		}
		if m.Strip <= backing || m.Strip > backing+2 {
			t.Errorf("%s: Strip = %d, want %d..%d", name, m.Strip, backing+1, backing+2)
		}
	}

	rows, _ = Rows(payload, FlagHello, 959)
	m, ok := Decode(screen(rows, 0, 0, 4))
	if !ok || !m.Hello || m.Attach {
		t.Errorf("hello grid: ok=%v Hello=%v Attach=%v", ok, m.Hello, m.Attach)
	}
	rows, _ = Rows(payload, 0, 959)
	if m, ok := Decode(screen(rows, 0, 0, 4)); !ok || m.Attach {
		t.Errorf("plain grid: ok=%v Attach=%v", ok, m.Attach)
	}
}

// noSub hides SubImage so Crop has to copy.
type noSub struct{ image.Image }

func TestCrop(t *testing.T) {
	img := screen([][]uint8{make([]uint8, 300)}, 0, 0, 4)
	if got := Crop(img, 20).Bounds(); got != image.Rect(0, 20, 2554, 1404) {
		t.Errorf("SubImage crop bounds = %v", got)
	}
	cp := Crop(noSub{img}, 20)
	if _, ok := cp.(*image.RGBA); !ok || cp.Bounds() != image.Rect(0, 0, 2554, 1384) {
		t.Errorf("copy crop = %T %v", cp, cp.Bounds())
	}
	if want := img.RGBAAt(7, 20); cp.(*image.RGBA).RGBAAt(7, 0) != want {
		t.Error("copy crop moved pixels")
	}
	for _, top := range []int{1404, 5000} {
		if got := Crop(img, top).Bounds(); got.Dy() != 0 {
			t.Errorf("top %d: height %d, want 0", top, got.Dy())
		}
	}
	if got := Crop(img, -5).Bounds(); got != img.Bounds() {
		t.Errorf("negative top: %v", got)
	}
}

func TestJPEG(t *testing.T) {
	img := Crop(screen([][]uint8{make([]uint8, 300)}, 0, 0, 4), 21) // odd top
	data, err := JPEG(img)
	if err != nil {
		t.Fatal(err)
	}
	out, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if got := out.Bounds().Size(); got != image.Pt(2554, 1383) {
		t.Errorf("decoded size = %v", got)
	}
}
