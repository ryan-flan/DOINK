package updates

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseVersion(t *testing.T) {
	cases := map[string][]int{
		"v0.10.0":       {0, 10, 0},
		" 0.9.5 ":       {0, 9, 5},
		"v1.0.0-beta.1": {1, 0, 0},
		"1+build":       {1},
	}
	for in, want := range cases {
		if got, ok := ParseVersion(in); !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("%q: %v %v", in, got, ok)
		}
	}
	for _, bad := range []string{"", "dev", "v", "1..2", "x1.0"} {
		if _, ok := ParseVersion(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"0.10.0", "0.9.5", true},
		{"0.10", "0.10.0", false},
		{"0.10.0", "0.10", false},
		{"v1.0.0-beta.1", "1.0.0", false},
		{"0.12.1", "0.12.0", true},
		{"0.12.0", "0.12.1", false},
		{"0.12.0", "dev", false},
		{"garbage", "0.1.0", false},
	}
	for _, c := range cases {
		if got := IsNewer(c.latest, c.current); got != c.want {
			t.Errorf("IsNewer(%q, %q) = %v", c.latest, c.current, got)
		}
	}
	if !(Info{Current: "0.9.5", Latest: "0.10.0"}).Newer() {
		t.Error("Info.Newer")
	}
}

func quiet(t *testing.T) {
	Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	t.Cleanup(func() { Log = nil })
}

func server(t *testing.T, code int, body string) (*httptest.Server, *http.Header) {
	var headers http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers = r.Header.Clone()
		w.WriteHeader(code)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &headers
}

func asset(name, url string) string {
	return `{"name":"` + name + `","browser_download_url":"` + url + `"}`
}

// releaseJSON is one list entry; extra is appended inside the object, e.g.
// `,"draft":true`.
func releaseJSON(tag, extra string, assets ...string) string {
	return `{"tag_name":"` + tag + `","html_url":"https://github.com/ryan-flan/DOINK/releases/tag/` + tag + `"` +
		extra + `,"assets":[` + strings.Join(assets, ",") + `]}`
}

func companionAssets(tag string) []string {
	zip := DownloadPrefix + tag + "/DOINK-" + tag + ".zip"
	return []string{
		asset("DOINK-"+tag+".zip", zip),
		asset("DOINK-"+tag+".zip.sha256", zip+".sha256"),
		asset("DOINK-"+tag+"-linux-amd64.tar.gz", DownloadPrefix+tag+"/DOINK-"+tag+"-linux-amd64.tar.gz"),
	}
}

func addonAssets(tag string) []string {
	return []string{
		asset("DOINK-"+tag+"-forever.zip", DownloadPrefix+tag+"/DOINK-"+tag+"-forever.zip"),
		asset("release.json", DownloadPrefix+tag+"/release.json"),
	}
}

func list(releases ...string) string { return "[" + strings.Join(releases, ",") + "]" }

func TestFetchLatest(t *testing.T) {
	quiet(t)
	srv, headers := server(t, 200, list(releaseJSON("v0.13.0", "", companionAssets("v0.13.0")...)))
	info, err := FetchLatest(srv.Client(), srv.URL, "0.12.0", "0.12.0")
	if err != nil {
		t.Fatal(err)
	}
	zip := DownloadPrefix + "v0.13.0/DOINK-v0.13.0.zip"
	if info.Current != "0.12.0" || info.Latest != "0.13.0" || info.URL != "https://github.com/ryan-flan/DOINK/releases/tag/v0.13.0" ||
		info.ZipURL != zip || info.SHA256URL != zip+".sha256" || !info.Newer() || !info.Installable() || info.CheckedAt.IsZero() {
		t.Errorf("info %+v", info)
	}
	if headers.Get("Accept") != "application/vnd.github+json" ||
		headers.Get("User-Agent") != "DOINK-companion/0.12.0 (+https://github.com/ryan-flan/DOINK)" {
		t.Errorf("headers %v", *headers)
	}
	if (Info{Current: "0.13.0", Latest: "0.13.0", ZipURL: zip, SHA256URL: zip + ".sha256"}).Installable() {
		t.Error("installable when not newer")
	}
}

func TestFetchLatestNonGitHubURL(t *testing.T) {
	quiet(t)
	assets := strings.Join(companionAssets("v0.13.0"), ",")
	srv, _ := server(t, 200, `[{"tag_name":"v0.13.0","html_url":"https://evil.example/x","assets":[`+assets+`]}]`)
	info, err := FetchLatest(srv.Client(), srv.URL, "0.12.0", "0.12.0")
	if err != nil || info.URL != ReleasesURL {
		t.Errorf("err %v info %+v", err, info)
	}
	srv, _ = server(t, 200, `[{"tag_name":"v0.13.0","assets":[`+assets+`]}]`)
	if info, _ := FetchLatest(srv.Client(), srv.URL, "0.12.0", "0.12.0"); info.URL != ReleasesURL {
		t.Errorf("info %+v", info)
	}
}

// The first usable release wins; everything ahead of it is skipped, and
// alone each of those is "no companion release found".
func TestFetchLatestPicksCompanionRelease(t *testing.T) {
	quiet(t)
	good := releaseJSON("v0.13.0", "", companionAssets("v0.13.0")...)
	zip14 := DownloadPrefix + "v0.14.0/DOINK-v0.14.0.zip"
	skipped := map[string]string{
		"addon-only release":    releaseJSON("v0.14.0", "", addonAssets("v0.14.0")...),
		"draft":                 releaseJSON("v0.14.0", `,"draft":true`, companionAssets("v0.14.0")...),
		"prerelease":            releaseJSON("v0.14.0-beta.1", `,"prerelease":true`, companionAssets("v0.14.0-beta.1")...),
		"zip from another host": releaseJSON("v0.14.0", "", asset("DOINK-v0.14.0.zip", "https://evil.example/DOINK-v0.14.0.zip"), asset("DOINK-v0.14.0.zip.sha256", zip14+".sha256")),
		"zip without sha256":    releaseJSON("v0.14.0", "", asset("DOINK-v0.14.0.zip", zip14)),
		"odd tag":               releaseJSON("latest", "", companionAssets("latest")...),
		"no assets":             `{"tag_name":"v0.14.0"}`,
	}
	for name, first := range skipped {
		srv, _ := server(t, 200, list(first, good))
		info, err := FetchLatest(srv.Client(), srv.URL, "0.12.0", "0.12.0")
		if err != nil || info.Latest != "0.13.0" || !info.Installable() {
			t.Errorf("%s then good: err %v info %+v", name, err, info)
		}
		srv, _ = server(t, 200, list(first))
		if info, err := FetchLatest(srv.Client(), srv.URL, "0.12.0", "0.12.0"); err == nil || err.Error() != "no companion release found" || info != nil {
			t.Errorf("%s alone: err %v info %+v", name, err, info)
		}
	}
	// Newest first: an older companion release further down never wins.
	srv, _ := server(t, 200, list(good, releaseJSON("v0.12.1", "", companionAssets("v0.12.1")...)))
	if info, err := FetchLatest(srv.Client(), srv.URL, "0.12.0", "0.12.0"); err != nil || info.Latest != "0.13.0" {
		t.Errorf("two companion releases: err %v info %+v", err, info)
	}
}

func TestFetchLatestFailures(t *testing.T) {
	quiet(t)
	for name, c := range map[string]struct {
		code int
		body string
	}{
		"HTTP 500":       {500, `[]`},
		"garbage":        {200, `<html>`},
		"single release": {200, `{"tag_name":"v0.13.0"}`}, // the old releases/latest shape
		"number tag":     {200, `[{"tag_name":14,"assets":[]}]`},
		"odd assets":     {200, `[{"tag_name":"v0.14.0","assets":"nope"}]`},
		"empty list":     {200, `[]`},
		"addon only":     {200, list(releaseJSON("v0.14.0", "", addonAssets("v0.14.0")...))},
	} {
		srv, _ := server(t, c.code, c.body)
		if info, err := FetchLatest(srv.Client(), srv.URL, "0.12.0", "0.12.0"); err == nil || info != nil {
			t.Errorf("%s: err %v info %+v", name, err, info)
		}
	}
	srv, _ := server(t, 200, "")
	srv.Close()
	if _, err := FetchLatest(nil, srv.URL, "0.12.0", "0.12.0"); err == nil {
		t.Error("connection refused: no error")
	}
}

func TestCheckerTiming(t *testing.T) {
	var mu sync.Mutex
	var at []time.Time
	calls := 0
	fetch := func() (*Info, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		at = append(at, time.Now())
		if calls == 1 {
			return nil, errors.New("offline")
		}
		return &Info{Current: "1", Latest: "2"}, nil
	}
	results := make(chan *Info, 10)
	c := NewChecker(fetch, func(i *Info) { results <- i }, 20*time.Millisecond, time.Hour, 30*time.Millisecond)
	start := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)
	select {
	case info := <-results:
		if info.Latest != "2" {
			t.Errorf("info %+v", info)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no result")
	}
	mu.Lock()
	if at[0].Sub(start) < 20*time.Millisecond || at[1].Sub(at[0]) < 30*time.Millisecond {
		t.Errorf("first %v, retry %v", at[0].Sub(start), at[1].Sub(at[0]))
	}
	mu.Unlock()
	// The next check is an hour away: none within a few retries' time.
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	if calls != 2 {
		t.Errorf("calls %d", calls)
	}
	mu.Unlock()

	if info, err := c.CheckNow(); err != nil || info.Latest != "2" || len(results) != 1 {
		t.Errorf("CheckNow: %+v %v, results %d", info, err, len(results))
	}
}
