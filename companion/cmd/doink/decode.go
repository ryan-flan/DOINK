package main

import (
	"fmt"
	"io"

	"github.com/ryan-flan/DOINK/companion/internal/savedvars"
	"github.com/ryan-flan/DOINK/companion/internal/shot"
)

// decodeScreenshots prints what each screenshot's realtime code says. It
// never posts, deletes or touches state: it's for checking that the
// addon's screenshots read correctly. 1 if any file couldn't be read.
func decodeScreenshots(paths []string, w io.Writer) int {
	code := 0
	for _, path := range paths {
		m, ok, err := shot.DecodeFile(path)
		switch {
		case err != nil:
			fmt.Fprintf(w, "%s: can't read: %v\n", path, err)
			code = 1
		case !ok:
			fmt.Fprintf(w, "%s: no DOINK code found\n", path)
		case m.Hello:
			fmt.Fprintf(w, "%s: hello %s\n", path, m.Payload)
		default:
			if e, err := savedvars.DecodeEvent(m.Payload); err == nil {
				fmt.Fprintf(w, "%s: event #%d %s %s\n  %s\n", path, e.Seq, e.Type, e.Key(), m.Payload)
			} else {
				fmt.Fprintf(w, "%s: code found, but not an event (%v)\n  %s\n", path, err, m.Payload)
			}
		}
	}
	return code
}
