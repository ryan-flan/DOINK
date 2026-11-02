// Package updates checks GitHub Releases for a newer companion. It only
// checks: nothing is downloaded or installed.
package updates

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Info is the result of one check.
type Info struct {
	Current, Latest, URL string
	CheckedAt            time.Time
}

// Newer reports whether Latest is newer than Current.
func (i Info) Newer() bool { return IsNewer(i.Latest, i.Current) }

var versionRE = regexp.MustCompile(`^v?(\d+(\.\d+)*)([-+].*)?$`)

// ParseVersion turns "v0.10.0" into [0 10 0]. A pre-release suffix
// ("v1.0.0-beta.1") is ignored. ok is false for anything else.
func ParseVersion(s string) ([]int, bool) {
	m := versionRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return nil, false
	}
	var out []int
	for _, part := range strings.Split(m[1], ".") {
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

// IsNewer compares zero-padded versions; false if either doesn't parse.
func IsNewer(latest, current string) bool {
	a, okA := ParseVersion(latest)
	b, okB := ParseVersion(current)
	if !okA || !okB {
		return false
	}
	for i := 0; i < max(len(a), len(b)); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return x > y
		}
	}
	return false
}
