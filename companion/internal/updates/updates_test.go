package updates

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
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

func TestFetchLatest(t *testing.T) {
	quiet(t)
	srv, headers := server(t, 200, `{"tag_name":"v0.13.0","html_url":"https://github.com/ryan-flan/DOINK/releases/tag/v0.13.0"}`)
	info, err := FetchLatest(srv.Client(), srv.URL, "0.12.0", "0.12.0")
	if err != nil {
		t.Fatal(err)
	}
	if info.Current != "0.12.0" || info.Latest != "0.13.0" || info.URL != "https://github.com/ryan-flan/DOINK/releases/tag/v0.13.0" ||
		!info.Newer() || info.CheckedAt.IsZero() {
		t.Errorf("info %+v", info)
	}
	if headers.Get("Accept") != "application/vnd.github+json" ||
		headers.Get("User-Agent") != "DOINK-companion/0.12.0 (+https://github.com/ryan-flan/DOINK)" {
		t.Errorf("headers %v", *headers)
	}
}

func TestFetchLatestNonGitHubURL(t *testing.T) {
	quiet(t)
	srv, _ := server(t, 200, `{"tag_name":"v0.13.0","html_url":"https://evil.example/x"}`)
	info, err := FetchLatest(srv.Client(), srv.URL, "0.12.0", "0.12.0")
	if err != nil || info.URL != ReleasesURL {
		t.Errorf("err %v info %+v", err, info)
	}
	srv, _ = server(t, 200, `{"tag_name":"v0.13.0"}`)
	if info, _ := FetchLatest(srv.Client(), srv.URL, "0.12.0", "0.12.0"); info.URL != ReleasesURL {
		t.Errorf("info %+v", info)
	}
}

func TestFetchLatestFailures(t *testing.T) {
	quiet(t)
	for name, c := range map[string]struct {
		code int
		body string
	}{
		"HTTP 500":   {500, `{}`},
		"garbage":    {200, `<html>`},
		"odd tag":    {200, `{"tag_name":"latest"}`},
		"no tag":     {200, `{"html_url":"https://github.com/x"}`},
		"number tag": {200, `{"tag_name":13}`},
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
