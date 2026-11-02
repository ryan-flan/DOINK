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
	"strings"

	"github.com/BurntSushi/toml"
)

// KV is one top-level string setting. Value "" removes the key.
type KV struct{ Key, Value string }

var tableHeader = regexp.MustCompile(`^\s*\[`)

// Set writes top-level string keys into the config.toml at path, keeping
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
		os.Remove(tmp)
		return err
	}
	return nil
}

func setLine(lines []string, kv KV) []string {
	key := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(kv.Key) + `\s*=`)
	end := len(lines) // end of the top-level section: the first [table]
	for i, line := range lines {
		if tableHeader.MatchString(line) {
			end = i
			break
		}
	}
	for i := 0; i < end; i++ {
		if key.MatchString(lines[i]) {
			if kv.Value == "" {
				return append(lines[:i:i], lines[i+1:]...)
			}
			lines[i] = kv.Key + " = " + Quote(kv.Value)
			return lines
		}
	}
	if kv.Value == "" {
		return lines
	}
	// After the last non-blank top-level line, so it lands above any table.
	at := end
	for at > 0 && strings.TrimSpace(lines[at-1]) == "" {
		at--
	}
	line := kv.Key + " = " + Quote(kv.Value)
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
		return fmt.Errorf("config.toml couldn't be updated safely (%v); edit it by hand", err)
	}
	for _, kv := range values {
		got, present := raw[kv.Key]
		if kv.Value == "" {
			if present {
				return fmt.Errorf("config.toml couldn't be updated safely (%s is still set); edit it by hand", kv.Key)
			}
			continue
		}
		if got != kv.Value {
			return fmt.Errorf("config.toml couldn't be updated safely (%s didn't read back); edit it by hand", kv.Key)
		}
	}
	return nil
}
