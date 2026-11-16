// Package settings is the companion's settings window (Windows) and the
// config.toml editing behind it (every platform, so it is tested
// everywhere).
package settings

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// KV is one top-level setting. Value is a string, bool, int, int64 or
// float64; "" or nil removes the key.
type KV struct {
	Key   string
	Value any
}

func (kv KV) remove() bool { return kv.Value == nil || kv.Value == "" }

// literal is kv's value as TOML.
func (kv KV) literal() string {
	switch v := kv.Value.(type) {
	case bool:
		return strconv.FormatBool(v)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		f := strconv.FormatFloat(v, 'f', -1, 64)
		if !strings.ContainsAny(f, ".eEn") {
			f += ".0" // TOML floats need a fraction
		}
		return f
	case string:
		return Quote(v)
	}
	return Quote(fmt.Sprint(kv.Value))
}

// decoded is the value as the TOML decoder returns it, for verify.
func (kv KV) decoded() any {
	switch v := kv.Value.(type) {
	case int:
		return int64(v)
	}
	return kv.Value
}

var tableHeader = regexp.MustCompile(`^\s*\[`)

// Set writes top-level keys into the config.toml at path, keeping
// everything else as it was: comments, other keys, their order, the line
// endings. An existing `key = ...` line is replaced in place (or removed
// for ""), a new key goes after the last top-level line. The result is
// parsed before it is written; if it doesn't read back as intended, the
// file is left alone and an error says so. The write is atomic (temp file,
// then rename), so the companion's live reload never sees half a file.
func Set(path string, values []KV) error {
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	text := string(old)
	eol := "\n"
	if strings.Contains(text, "\r\n") {
		eol = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1] // the final newline, put back below
	}

	for _, kv := range values {
		lines = setLine(lines, kv)
	}

	out := strings.Join(lines, eol)
	if out != "" {
		out += eol
	}
	if err := verify(out, values); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(out), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// section is where kv's line belongs: the top level (before the first
// [table]), or for "table.key" the body of an existing [table], else the
// top level as a dotted key. name is the key as written in that section.
func section(lines []string, kv KV) (from, to int, name string) {
	to = len(lines)
	for i, line := range lines {
		if tableHeader.MatchString(line) {
			to = i
			break
		}
	}
	table, sub, dotted := strings.Cut(kv.Key, ".")
	if !dotted {
		return 0, to, kv.Key
	}
	header := regexp.MustCompile(`^\s*\[\s*` + regexp.QuoteMeta(table) + `\s*\]\s*(#.*)?$`)
	for i, line := range lines {
		if !header.MatchString(line) {
			continue
		}
		end := len(lines)
		for j := i + 1; j < len(lines); j++ {
			if tableHeader.MatchString(lines[j]) {
				end = j
				break
			}
		}
		return i + 1, end, sub
	}
	return 0, to, kv.Key
}

func setLine(lines []string, kv KV) []string {
	from, end, name := section(lines, kv)
	key := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(name) + `\s*=`)
	for i := from; i < end; i++ {
		if key.MatchString(lines[i]) {
			if kv.remove() {
				return append(lines[:i:i], lines[i+1:]...)
			}
			lines[i] = name + " = " + kv.literal()
			return lines
		}
	}
	if kv.remove() {
		return lines
	}
	// After the section's last non-blank line, so it lands above any table.
	at := end
	for at > from && strings.TrimSpace(lines[at-1]) == "" {
		at--
	}
	line := name + " = " + kv.literal()
	return append(lines[:at:at], append([]string{line}, lines[at:]...)...)
}

// Quote is a TOML string: a literal string ('...', no escapes, so Windows
// paths stay readable) when possible, else a basic string with escapes.
func Quote(s string) string {
	literal := true
	for _, r := range s {
		if r == '\'' || r < 0x20 || r == 0x7f {
			literal = false
			break
		}
	}
	if literal {
		return "'" + s + "'"
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func verify(out string, values []KV) error {
	raw := map[string]any{}
	if _, err := toml.Decode(out, &raw); err != nil {
		return fmt.Errorf("config.toml couldn't be updated safely (%w); edit it by hand", err)
	}
	for _, kv := range values {
		got, present := lookup(raw, kv.Key)
		if kv.remove() {
			if present {
				return fmt.Errorf("config.toml couldn't be updated safely (%s is still set); edit it by hand", kv.Key)
			}
			continue
		}
		if got != kv.decoded() {
			return fmt.Errorf("config.toml couldn't be updated safely (%s didn't read back); edit it by hand", kv.Key)
		}
	}
	return nil
}

// lookup finds "a.b" in decoded TOML (nested tables).
func lookup(raw map[string]any, key string) (any, bool) {
	head, rest, dotted := strings.Cut(key, ".")
	v, ok := raw[head]
	if !ok || !dotted {
		return v, ok
	}
	table, ok := v.(map[string]any)
	if !ok {
		return nil, false
	}
	return lookup(table, rest)
}
