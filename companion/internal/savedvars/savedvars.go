// Package savedvars extracts DOINK events and webhooks from the addon's
// SavedVariables file (DOINK.lua).
//
// The addon stores each event as a JSON string inside a Lua table, so
// instead of a Lua parser this finds the ["events"] = { ... } blocks and
// reads the Lua string literals inside them. Everything works on bytes:
// \ddd escapes and raw UTF-8 are put back together before any string is
// treated as UTF-8.
package savedvars

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
)

// Log receives parse warnings; nil means slog.Default().
var Log *slog.Logger

func logger() *slog.Logger {
	if Log != nil {
		return Log
	}
	return slog.Default()
}

// Event is one queued DOINK event.
type Event struct {
	Seq     int
	Char    string
	Surname string // "" when absent
	Realm   string
	Class   string // "" when absent
	Level   int    // 0 when absent
	TS      int64  // valid only if HasTS
	HasTS   bool
	Type    string
	Test    bool
	Data    map[string]any // never nil; numbers are float64
}

// FullName is "Flano Wren" on Forever (surnames), "Flano" elsewhere.
func (e Event) FullName() string {
	if e.Surname != "" {
		return e.Char + " " + e.Surname
	}
	return e.Char
}

// Key is the addon's per-character key, e.g. "Flano Wren-Classic Beta PvE 2".
func (e Event) Key() string { return e.FullName() + "-" + e.Realm }

// LegacyKey is the key used before surnames existed (v0.2.0).
func (e Event) LegacyKey() string { return e.Char + "-" + e.Realm }

var (
	eventsBlock   = regexp.MustCompile(`\["events"\]\s*=\s*\{`)
	webhooksBlock = regexp.MustCompile(`\["webhooks"\]\s*=\s*\{`)
)

// ParseEvents returns every valid event in the file, in file order.
func ParseEvents(file []byte) []Event {
	var events []Event
	for _, m := range eventsBlock.FindAllIndex(file, -1) {
		events = append(events, parseBlock(file, m[1])...)
	}
	return events
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }

// skipComment returns the index of the next "\n" (or len) when file[i:]
// starts with "--", else -1.
func skipComment(file []byte, i int) int {
	if !bytes.HasPrefix(file[i:], []byte("--")) {
		return -1
	}
	if end := bytes.IndexByte(file[i:], '\n'); end >= 0 {
		return i + end
	}
	return len(file)
}

func parseBlock(file []byte, i int) []Event {
	var events []Event
	n := len(file)
	for i < n {
		c := file[i]
		switch {
		case isSpace(c) || c == ',':
			i++
		case skipComment(file, i) >= 0: // WoW sometimes writes "-- [1]" comments
			i = skipComment(file, i)
		case c == '}':
			return events
		case c == '"' || c == '\'':
			raw, next, err := readString(file, i)
			if err != nil {
				logger().Warn(fmt.Sprintf("events block at offset %d: %v; skipping rest of block", i, err))
				return events
			}
			i = next
			e, err := DecodeEvent(raw)
			if err != nil {
				logger().Warn(fmt.Sprintf("skipping event (%v): %s", err, head(raw, 120)))
				continue
			}
			events = append(events, e)
		default:
			logger().Warn(fmt.Sprintf("unexpected %q in events block at offset %d; skipping rest of block", string(c), i))
			return events
		}
	}
	return events
}

func head(b []byte, n int) string {
	if len(b) > n {
		b = b[:n]
	}
	return strings.ToValidUTF8(string(b), "�")
}

var escapes = map[byte]byte{
	'n': '\n', 't': '\t', 'r': '\r', 'a': '\a', 'b': '\b', 'f': '\f',
	'v': '\v', '\\': '\\', '"': '"', '\'': '\'', '\n': '\n',
}

// readString reads the Lua short string whose opening quote is at file[i]
// and returns its unescaped bytes and the index just past the closing quote.
func readString(file []byte, i int) ([]byte, int, error) {
	quote := file[i]
	var out []byte
	n := len(file)
	i++
	for i < n {
		c := file[i]
		if c == quote {
			return out, i + 1, nil
		}
		if c == '\n' {
			return nil, i, errors.New("unterminated string")
		}
		if c != '\\' {
			out = append(out, c)
			i++
			continue
		}
		i++
		if i >= n {
			break
		}
		e := file[i]
		if e >= '0' && e <= '9' { // \ddd: up to three decimal digits, one byte
			j, v := i, 0
			for j < n && j < i+3 && file[j] >= '0' && file[j] <= '9' {
				v = v*10 + int(file[j]-'0')
				j++
			}
			if v > 255 {
				return nil, i, fmt.Errorf("escape \\%s out of range", file[i:j])
			}
			out = append(out, byte(v))
			i = j
		} else if b, ok := escapes[e]; ok {
			out = append(out, b)
			i++
		} else {
			return nil, i, fmt.Errorf("unknown escape \\%c", e)
		}
	}
	return nil, i, errors.New("unterminated string")
}

// DecodeEvent decodes and validates one JSON event.
func DecodeEvent(raw []byte) (Event, error) {
	text := strings.ToValidUTF8(string(raw), "�")
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		return Event{}, fmt.Errorf("not JSON: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return Event{}, errors.New("not JSON: data after the object")
	}
	if obj == nil {
		return Event{}, errors.New("not a JSON object")
	}
	for _, k := range []string{"seq", "char", "realm", "type"} {
		if _, ok := obj[k]; !ok {
			return Event{}, fmt.Errorf("missing %q", k)
		}
	}
	var e Event
	seq, ok := intOf(obj["seq"])
	if !ok {
		return Event{}, errors.New("seq isn't an integer")
	}
	e.Seq = int(seq)
	var err error
	if e.Char, err = strField(obj, "char", true); err != nil {
		return Event{}, err
	}
	if e.Realm, err = strField(obj, "realm", true); err != nil {
		return Event{}, err
	}
	if e.Type, err = strField(obj, "type", true); err != nil {
		return Event{}, err
	}
	if e.Surname, err = strField(obj, "surname", false); err != nil {
		return Event{}, err
	}
	if e.Class, err = strField(obj, "class", false); err != nil {
		return Event{}, err
	}
	if v, present := obj["level"]; present {
		level, ok := intOf(v)
		if !ok {
			return Event{}, errors.New("level isn't an integer")
		}
		e.Level = int(level)
	}
	if v, present := obj["ts"]; present {
		ts, ok := intOf(v)
		if !ok {
			return Event{}, errors.New("ts isn't an integer")
		}
		e.TS, e.HasTS = ts, true
	}
	if v, present := obj["test"]; present {
		b, ok := v.(bool)
		if !ok {
			return Event{}, errors.New("test isn't a boolean")
		}
		e.Test = b
	}
	e.Data = map[string]any{}
	if v, present := obj["data"]; present {
		m, ok := v.(map[string]any)
		if !ok {
			return Event{}, errors.New("data isn't an object")
		}
		e.Data = plainNumbers(m).(map[string]any)
	}
	return e, nil
}

func intOf(v any) (int64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	i, err := n.Int64()
	return i, err == nil
}

func strField(obj map[string]any, key string, required bool) (string, error) {
	v, present := obj[key]
	if !present {
		if required {
			return "", fmt.Errorf("missing %q", key)
		}
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%s isn't a string", key)
	}
	return s, nil
}

// plainNumbers turns json.Number values back into float64, recursively,
// so embed builders can type-switch on float64.
func plainNumbers(v any) any {
	switch t := v.(type) {
	case json.Number:
		f, _ := t.Float64()
		return f
	case map[string]any:
		for k, x := range t {
			t[k] = plainNumbers(x)
		}
		return t
	case []any:
		for i, x := range t {
			t[i] = plainNumbers(x)
		}
		return t
	}
	return v
}

// ParseWebhooks returns DOINKDB.webhooks: {"*": url, "Name-Realm": url}.
// On a malformed entry it logs a warning and returns what was parsed so far.
func ParseWebhooks(file []byte) map[string]string {
	hooks := map[string]string{}
	m := webhooksBlock.FindIndex(file)
	if m == nil {
		return hooks
	}
	i, n := m[1], len(file)
	fail := func(at int, msg string) map[string]string {
		logger().Warn(fmt.Sprintf("webhooks block at offset %d: %s", at, msg))
		return hooks
	}
	for i < n {
		c := file[i]
		switch {
		case isSpace(c) || c == ',':
			i++
		case skipComment(file, i) >= 0:
			i = skipComment(file, i)
		case c == '}':
			return hooks
		case c == '[':
			j := expect(file, i+1, "\"'")
			if j < 0 {
				return fail(i, "expected a quote")
			}
			key, next, err := readString(file, j)
			if err != nil {
				return fail(j, err.Error())
			}
			if j = expect(file, next, "]"); j < 0 {
				return fail(next, "expected ]")
			}
			next = j + 1
			if j = expect(file, next, "="); j < 0 {
				return fail(next, "expected =")
			}
			next = j + 1
			if j = expect(file, next, "\"'"); j < 0 {
				return fail(next, "expected a quote")
			}
			value, next, err := readString(file, j)
			if err != nil {
				return fail(j, err.Error())
			}
			hooks[strings.ToValidUTF8(string(key), "�")] = strings.ToValidUTF8(string(value), "�")
			i = next
		default:
			return fail(i, fmt.Sprintf("unexpected %q", string(c)))
		}
	}
	return hooks
}

// expect skips whitespace and returns the index of the next byte, which
// must be one of chars; -1 otherwise.
func expect(file []byte, i int, chars string) int {
	for i < len(file) && isSpace(file[i]) {
		i++
	}
	if i >= len(file) || !strings.ContainsRune(chars, rune(file[i])) {
		return -1
	}
	return i
}
