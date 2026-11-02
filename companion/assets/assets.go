// Package assets embeds the tray icons. make_icon.py draws them (a one-off
// needing any Python with Pillow); winres.json uses doink.ico for the exe.
package assets

import _ "embed"

// Icon is the normal tray icon.
//
//go:embed doink.ico
var Icon []byte

// UpdateIcon carries an orange dot: a newer release is out.
//
//go:embed doink-update.ico
var UpdateIcon []byte
