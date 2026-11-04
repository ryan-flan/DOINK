// Package shot reads the code the addon draws into a screenshot for
// realtime posts (addon/DOINK/Transports/Screenshot.lua; the layout is in
// CLAUDE.md under "Screenshot transport contract").
//
// The grid is a strip as wide as the screen allows (MinCols..MaxCols
// blocks, given in the header). Row 0 holds a 16-block sync pattern and a
// 9-byte header; rows 1..R hold the payload bits, MSB first, row-major.
// 1 is white, 0 black. The addon draws it pixel-perfect at the top-left
// corner, 2 px per block; Decode searches a little around
// that in case a client scales or offsets it.
package shot

import (
	"errors"
	"image"
	"image/color"
	_ "image/jpeg" // screenshot formats the addon allows
	_ "image/png"
	"math"
	"os"
)

const (
	Version    = 2
	MinCols    = 256
	MaxCols    = 1200
	MaxRows    = 32
	MaxPayload = 1024 // bytes: MaxRows rows at MinCols
	flagHello  = 1
	headerLen  = 8 // bytes before the header CRC
)

var syncPattern = [16]uint8{1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 1, 0, 0, 1, 1}

// Message is a decoded grid: an event's JSON, or a hello from
// /doink realtime test.
type Message struct {
	Hello   bool
	Payload []byte
}

// ErrTooLong: the payload doesn't fit one screenshot.
var ErrTooLong = errors.New("payload longer than one screenshot holds")

func crc16(b []byte) uint16 { // CRC-16/CCITT-FALSE
	crc := uint16(0xFFFF)
	for _, c := range b {
		crc ^= uint16(c) << 8
		for range 8 {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

func crc8(b []byte) uint8 { // poly 0x07, init 0
	var crc uint8
	for _, c := range b {
		crc ^= c
		for range 8 {
			if crc&0x80 != 0 {
				crc = crc<<1 ^ 0x07
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

func appendBits(bits []uint8, v uint, n int) []uint8 {
	for i := n - 1; i >= 0; i-- {
		bits = append(bits, uint8(v>>i&1))
	}
	return bits
}

// Rows encodes a payload exactly as the addon does: row 0 (sync + header),
// then the data rows, each cols blocks (MinCols..MaxCols) of 0 or 1.
func Rows(payload []byte, hello bool, cols int) ([][]uint8, error) {
	if len(payload) > MaxPayload {
		return nil, ErrTooLong
	}
	if cols < MinCols || cols > MaxCols {
		return nil, errors.New("cols out of range")
	}
	dataRows := max(1, (len(payload)*8+cols-1)/cols)
	flags := uint8(0)
	if hello {
		flags = flagHello
	}
	pc := crc16(payload)
	n := len(payload)
	header := []byte{Version<<4 | flags, byte(cols >> 8), byte(cols), byte(dataRows),
		byte(n >> 8), byte(n), byte(pc >> 8), byte(pc)}

	row0 := append([]uint8{}, syncPattern[:]...)
	for _, b := range header {
		row0 = appendBits(row0, uint(b), 8)
	}
	row0 = appendBits(row0, uint(crc8(header)), 8)
	row0 = append(row0, make([]uint8, cols-len(row0))...)

	var data []uint8
	for _, b := range payload {
		data = appendBits(data, uint(b), 8)
	}
	data = append(data, make([]uint8, dataRows*cols-len(data))...)
	rows := [][]uint8{row0}
	for r := range dataRows {
		rows = append(rows, data[r*cols:(r+1)*cols])
	}
	return rows, nil
}

// Render draws rows as the addon does (black backing one block bigger on
// the right and bottom), at the origin. For tests and tools.
func Render(rows [][]uint8, block int) *image.Gray {
	cols := MinCols
	if len(rows) > 0 {
		cols = len(rows[0])
	}
	img := image.NewGray(image.Rect(0, 0, (cols+1)*block, (len(rows)+1)*block))
	for r, row := range rows {
		for c, v := range row {
			if v == 0 {
				continue
			}
			for y := r * block; y < (r+1)*block; y++ {
				for x := c * block; x < (c+1)*block; x++ {
					img.Pix[img.PixOffset(x, y)] = 255
				}
			}
		}
	}
	return img
}

// luma reads brightness fast for the decoders' usual types.
type luma struct {
	img    image.Image
	bounds image.Rectangle
	ycc    *image.YCbCr
	gray   *image.Gray
}

func newLuma(img image.Image) *luma {
	l := &luma{img: img, bounds: img.Bounds()}
	switch m := img.(type) {
	case *image.YCbCr:
		l.ycc = m
	case *image.Gray:
		l.gray = m
	}
	return l
}

func (l *luma) at(x, y int) uint8 {
	switch {
	case l.ycc != nil:
		return l.ycc.Y[l.ycc.YOffset(x, y)]
	case l.gray != nil:
		return l.gray.Pix[l.gray.PixOffset(x, y)]
	}
	return color.GrayModel.Convert(l.img.At(x, y)).(color.Gray).Y
}

// grid is one candidate placement: origin and block size in pixels.
type grid struct {
	l      *luma
	x0, y0 float64
	b      float64
	side   int // pixels sampled per axis, centred in the block
}

// bit reads block (r, c): the mean of its central side×side pixels.
// ok is false when the block is off the image.
func (g grid) bit(r, c int) (v uint8, ok bool) {
	cx := g.x0 + (float64(c)+0.5)*g.b
	cy := g.y0 + (float64(r)+0.5)*g.b
	half := float64(g.side) / 2
	sx := int(math.Floor(cx - half + 0.5))
	sy := int(math.Floor(cy - half + 0.5))
	if !(image.Point{sx, sy}.In(g.l.bounds)) || !(image.Point{sx + g.side - 1, sy + g.side - 1}.In(g.l.bounds)) {
		return 0, false
	}
	sum := 0
	for y := sy; y < sy+g.side; y++ {
		for x := sx; x < sx+g.side; x++ {
			sum += int(g.l.at(x, y))
		}
	}
	if sum >= 128*g.side*g.side {
		return 1, true
	}
	return 0, true
}

// bytes reads n bytes starting at block index start of the bit stream
// that runs row-major, cols blocks per row, from row firstRow.
func (g grid) bytes(firstRow, start, n, cols int) ([]byte, bool) {
	out := make([]byte, n)
	for i := range n * 8 {
		pos := start + i
		v, ok := g.bit(firstRow+pos/cols, pos%cols)
		if !ok {
			return nil, false
		}
		out[i/8] |= v << (7 - i%8)
	}
	return out, true
}

func (g grid) decode() (Message, bool) {
	for c, want := range syncPattern {
		if v, ok := g.bit(0, c); !ok || v != want {
			return Message{}, false
		}
	}
	// The header sits in row 0 before any wrap (88 blocks < MinCols), so
	// it reads the same whatever the width.
	h, ok := g.bytes(0, len(syncPattern), headerLen+1, MaxCols)
	if !ok || crc8(h[:headerLen]) != h[headerLen] || h[0]>>4 != Version {
		return Message{}, false
	}
	cols := int(h[1])<<8 | int(h[2])
	rows := int(h[3])
	n := int(h[4])<<8 | int(h[5])
	if cols < MinCols || cols > MaxCols || rows < 1 || rows > MaxRows || n > MaxPayload || n > rows*cols/8 {
		return Message{}, false
	}
	payload, ok := g.bytes(1, 0, n, cols)
	if !ok || crc16(payload) != uint16(h[6])<<8|uint16(h[7]) {
		return Message{}, false
	}
	return Message{Hello: h[0]&flagHello != 0, Payload: payload}, true
}

// blockSizes is the search order: the sizes the addon draws, then a
// sweep for clients that scale the UI.
var blockSizes = func() []float64 {
	s := []float64{2, 4, 3}
	for i := 0; i <= 80; i++ {
		s = append(s, 2+float64(i)*0.05)
	}
	return s
}()

// searchOffset is how far from the corner (in pixels, each axis) the grid
// may start.
const searchOffset = 8

// Decode finds and reads a DOINK grid near the image's top-left corner.
// false means there is none (or it's damaged): leave that file alone.
func Decode(img image.Image) (Message, bool) {
	l := newLuma(img)
	origin := l.bounds.Min
	for _, b := range blockSizes {
		side := max(1, int(math.Round(b/2)))
		for dy := 0; dy <= searchOffset; dy++ {
			for dx := 0; dx <= searchOffset; dx++ {
				g := grid{l: l, x0: float64(origin.X + dx), y0: float64(origin.Y + dy), b: b, side: side}
				if m, ok := g.decode(); ok {
					return m, true
				}
			}
		}
	}
	return Message{}, false
}

// DecodeFile opens and decodes a screenshot. err is set when the image
// itself can't be read (for example, still being written).
func DecodeFile(path string) (Message, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return Message{}, false, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return Message{}, false, err
	}
	m, ok := Decode(img)
	return m, ok, nil
}
