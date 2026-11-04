package discord

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu     sync.Mutex
	t      time.Time
	sleeps []time.Duration
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) sleep(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sleeps = append(c.sleeps, d)
	c.t = c.t.Add(d)
}

func testPool(t *testing.T, dryRun bool, log *slog.Logger) (*Pool, *fakeClock) {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	p := NewPool(nil, dryRun, "1.2.3", log)
	clock := &fakeClock{t: time.Unix(1000, 0)}
	p.now, p.sleep = clock.now, clock.sleep
	return p, clock
}

var embeds = []Embed{{"title": "x"}}

func TestSendSuccessSchedulesTwoSeconds(t *testing.T) {
	var got struct {
		path, query, ua, ctype string
		body                   map[string]any
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path, got.query = r.URL.Path, r.URL.RawQuery
		got.ua, got.ctype = r.Header.Get("User-Agent"), r.Header.Get("Content-Type")
		json.NewDecoder(r.Body).Decode(&got.body)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	p, clock := testPool(t, false, nil)
	url := srv.URL + "/api/webhooks/1/x"
	if err := p.Send(url, embeds); err != nil {
		t.Fatal(err)
	}
	if got.path != "/api/webhooks/1/x" || got.query != "wait=true" {
		t.Errorf("path %q query %q", got.path, got.query)
	}
	if got.ua != "DOINK-companion/1.2.3" || got.ctype != "application/json" {
		t.Errorf("ua %q ctype %q", got.ua, got.ctype)
	}
	if es, _ := got.body["embeds"].([]any); len(es) != 1 {
		t.Errorf("body %v", got.body)
	}
	if next := p.hooks[url].next; !next.Equal(clock.now().Add(2 * time.Second)) {
		t.Errorf("next %v", next)
	}
	// The next send waits out the schedule.
	if err := p.Send(url, embeds); err != nil {
		t.Fatal(err)
	}
	if len(clock.sleeps) != 1 || clock.sleeps[0] != 2*time.Second {
		t.Errorf("sleeps %v", clock.sleeps)
	}
}

func TestSendQueryAlreadyPresent(t *testing.T) {
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
	}))
	defer srv.Close()
	p, _ := testPool(t, false, nil)
	if err := p.Send(srv.URL+"/h?thread_id=5", embeds); err != nil {
		t.Fatal(err)
	}
	if query != "thread_id=5&wait=true" {
		t.Errorf("query %q", query)
	}
}

func TestSendResetAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset-After", "7")
	}))
	defer srv.Close()
	p, clock := testPool(t, false, nil)
	if err := p.Send(srv.URL, embeds); err != nil {
		t.Fatal(err)
	}
	if next := p.hooks[srv.URL].next; !next.Equal(clock.now().Add(7 * time.Second)) {
		t.Errorf("next %v, want +7s", next.Sub(clock.now()))
	}
}

func TestSendResetAfterIgnoredWhileRemaining(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "3")
		w.Header().Set("X-RateLimit-Reset-After", "7")
	}))
	defer srv.Close()
	p, clock := testPool(t, false, nil)
	p.Send(srv.URL, embeds)
	if next := p.hooks[srv.URL].next; !next.Equal(clock.now().Add(2 * time.Second)) {
		t.Errorf("next %v", next.Sub(clock.now()))
	}
}

func sequence(responses ...func(w http.ResponseWriter)) (*httptest.Server, *int) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := calls
		calls++
		if i >= len(responses) {
			i = len(responses) - 1
		}
		responses[i](w)
	}))
	return srv, &calls
}

func rateLimited(body string) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.WriteHeader(429)
		io.WriteString(w, body)
	}
}

func ok(w http.ResponseWriter) { w.WriteHeader(204) }

func TestSendRejected(t *testing.T) {
	srv, calls := sequence(func(w http.ResponseWriter) {
		w.WriteHeader(400)
		_, _ = io.WriteString(w, `{"message": "Invalid Form Body"}`)
	})
	defer srv.Close()
	p, _ := testPool(t, false, nil)
	err := p.Send(srv.URL, embeds)
	if !errors.Is(err, ErrRejected) || *calls != 1 || !strings.Contains(err.Error(), "Invalid Form Body") {
		t.Errorf("err %v calls %d", err, *calls)
	}
	// Only 400 means "the message is wrong"; a 5xx is worth a retry later.
	srv2, _ := sequence(func(w http.ResponseWriter) { w.WriteHeader(502) })
	defer srv2.Close()
	if err := p.Send(srv2.URL, embeds); err == nil || errors.Is(err, ErrRejected) {
		t.Errorf("502: %v", err)
	}
}

func TestSend429ThenOK(t *testing.T) {
	srv, calls := sequence(rateLimited(`{"retry_after": 0.25}`), ok)
	defer srv.Close()
	p, clock := testPool(t, false, nil)
	if err := p.Send(srv.URL, embeds); err != nil {
		t.Fatal(err)
	}
	if *calls != 2 || len(clock.sleeps) != 1 || clock.sleeps[0] != 250*time.Millisecond {
		t.Errorf("calls %d sleeps %v", *calls, clock.sleeps)
	}
}

func TestSend429NonJSONWaitsOneSecond(t *testing.T) {
	srv, calls := sequence(rateLimited(`<html>slow down</html>`), ok)
	defer srv.Close()
	p, clock := testPool(t, false, nil)
	if err := p.Send(srv.URL, embeds); err != nil {
		t.Fatal(err)
	}
	if *calls != 2 || len(clock.sleeps) != 1 || clock.sleeps[0] != time.Second {
		t.Errorf("calls %d sleeps %v", *calls, clock.sleeps)
	}
}

func TestSendStillRateLimited(t *testing.T) {
	srv, calls := sequence(rateLimited(`{"retry_after": 0.01}`))
	defer srv.Close()
	p, _ := testPool(t, false, nil)
	err := p.Send(srv.URL, embeds)
	if err == nil || err.Error() != "still rate limited after 5 attempts" || *calls != 5 {
		t.Errorf("err %v calls %d", err, *calls)
	}
}

func TestSendHTTPError(t *testing.T) {
	srv, calls := sequence(func(w http.ResponseWriter) {
		w.WriteHeader(403)
		io.WriteString(w, `{"message": "Invalid Webhook Token"}`+strings.Repeat("x", 300))
	})
	defer srv.Close()
	p, _ := testPool(t, false, nil)
	err := p.Send(srv.URL, embeds)
	if err == nil || !strings.HasPrefix(err.Error(), `HTTP 403: {"message": "Invalid Webhook Token"}`) || *calls != 1 {
		t.Fatalf("err %v calls %d", err, *calls)
	}
	if len(err.Error()) != len("HTTP 403: ")+200 {
		t.Errorf("body not cut at 200 bytes: %d", len(err.Error()))
	}
}

func TestSendNetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	p, _ := testPool(t, false, nil)
	err := p.Send(url, embeds)
	if err == nil || !strings.HasPrefix(err.Error(), "network error: ") {
		t.Errorf("err %v", err)
	}
}

func TestDryRunMakesNoRequest(t *testing.T) {
	srv, calls := sequence(ok)
	defer srv.Close()
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	p, clock := testPool(t, true, log)
	if err := p.Send(srv.URL, []Embed{{"title": "Flano <looted> & ran"}}); err != nil {
		t.Fatal(err)
	}
	if *calls != 0 || len(p.hooks) != 0 || len(clock.sleeps) != 0 {
		t.Errorf("calls %d hooks %d", *calls, len(p.hooks))
	}
	if out := buf.String(); !strings.Contains(out, "dry run, would post:") || !strings.Contains(out, "Flano <looted> & ran") {
		t.Errorf("log %q", out)
	}
}
