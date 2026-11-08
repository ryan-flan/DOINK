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

var photo = &Attachment{Name: "doink.jpg", Data: []byte("\xff\xd8not really a jpeg"), Mime: "image/jpeg"}

// recorded is what a test webhook saw of one request.
type recorded struct {
	ctype    string
	payload  map[string]any
	filename string
	fileType string
	file     []byte
}

// recordingServer answers each request from responses (the last repeats)
// and records what it was sent, JSON or multipart.
func recordingServer(t *testing.T, responses ...func(w http.ResponseWriter)) (*httptest.Server, *[]recorded) {
	t.Helper()
	var got []recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := recorded{ctype: r.Header.Get("Content-Type")}
		if strings.HasPrefix(rec.ctype, "multipart/form-data") {
			if err := r.ParseMultipartForm(32 << 20); err != nil {
				t.Errorf("parse multipart: %v", err)
			}
			if err := json.Unmarshal([]byte(r.MultipartForm.Value["payload_json"][0]), &rec.payload); err != nil {
				t.Errorf("payload_json: %v", err)
			}
			if fh := r.MultipartForm.File["files[0]"]; len(fh) == 1 {
				rec.filename, rec.fileType = fh[0].Filename, fh[0].Header.Get("Content-Type")
				f, _ := fh[0].Open()
				rec.file, _ = io.ReadAll(f)
				f.Close()
			}
		} else {
			_ = json.NewDecoder(r.Body).Decode(&rec.payload)
		}
		i := min(len(got), len(responses)-1)
		got = append(got, rec)
		responses[i](w)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func status(code int) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) { w.WriteHeader(code) }
}

func firstEmbed(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	list, _ := payload["embeds"].([]any)
	if len(list) == 0 {
		t.Fatalf("no embeds in %v", payload)
	}
	return list[0].(map[string]any)
}

func TestSendWithImageMultipart(t *testing.T) {
	srv, got := recordingServer(t, ok)
	p, clock := testPool(t, false, nil)
	in := []Embed{{"title": "x"}, {"title": "y"}}
	if err := p.SendWithImage(srv.URL, in, photo); err != nil {
		t.Fatal(err)
	}
	if len(*got) != 1 {
		t.Fatalf("%d requests", len(*got))
	}
	r := (*got)[0]
	if !strings.HasPrefix(r.ctype, "multipart/form-data; boundary=") {
		t.Errorf("content type %q", r.ctype)
	}
	img, _ := firstEmbed(t, r.payload)["image"].(map[string]any)
	if img["url"] != "attachment://doink.jpg" {
		t.Errorf("embed image %v", img)
	}
	atts, _ := r.payload["attachments"].([]any)
	if len(atts) != 1 || atts[0].(map[string]any)["filename"] != "doink.jpg" {
		t.Errorf("attachments %v", r.payload["attachments"])
	}
	if r.filename != "doink.jpg" || r.fileType != "image/jpeg" || !bytes.Equal(r.file, photo.Data) {
		t.Errorf("file %q %q %q", r.filename, r.fileType, r.file)
	}
	if second := r.payload["embeds"].([]any)[1].(map[string]any); second["image"] != nil {
		t.Errorf("second embed got an image: %v", second)
	}
	if _, set := in[0]["image"]; set {
		t.Error("the caller's embed was modified")
	}
	if next := p.hook(srv.URL).next.Sub(clock.now()); next != 2*time.Second {
		t.Errorf("next post in %v, want 2s", next)
	}
}

func TestSendWithImageNilIsPlainJSON(t *testing.T) {
	srv, got := recordingServer(t, ok)
	p, _ := testPool(t, false, nil)
	if err := p.SendWithImage(srv.URL, embeds, nil); err != nil {
		t.Fatal(err)
	}
	r := (*got)[0]
	if r.ctype != "application/json" || r.payload["attachments"] != nil || firstEmbed(t, r.payload)["image"] != nil {
		t.Errorf("%q %v", r.ctype, r.payload)
	}
}

func TestSendWithImageFallsBackWhenRejected(t *testing.T) {
	for _, code := range []int{400, 413} {
		srv, got := recordingServer(t, status(code), ok)
		var buf bytes.Buffer
		p, _ := testPool(t, false, slog.New(slog.NewTextHandler(&buf, nil)))
		if err := p.SendWithImage(srv.URL, embeds, photo); err != nil {
			t.Fatalf("%d: %v", code, err)
		}
		if len(*got) != 2 {
			t.Fatalf("%d: %d requests, want 2", code, len(*got))
		}
		second := (*got)[1]
		if second.ctype != "application/json" || second.payload["attachments"] != nil || firstEmbed(t, second.payload)["image"] != nil {
			t.Errorf("%d: the retry still has the picture: %q %v", code, second.ctype, second.payload)
		}
		if !strings.Contains(buf.String(), "posting without it") {
			t.Errorf("%d: log %q", code, buf.String())
		}
	}
}

func TestSendWithImageRejectedTwice(t *testing.T) {
	srv, got := recordingServer(t, status(400))
	p, _ := testPool(t, false, nil)
	if err := p.SendWithImage(srv.URL, embeds, photo); !errors.Is(err, ErrRejected) || len(*got) != 2 {
		t.Errorf("err %v, %d requests", err, len(*got))
	}
}

func TestSendWithImage429ThenOK(t *testing.T) {
	srv, got := recordingServer(t, rateLimited(`{"retry_after": 0.5}`), ok)
	p, clock := testPool(t, false, nil)
	if err := p.SendWithImage(srv.URL, embeds, photo); err != nil {
		t.Fatal(err)
	}
	if len(*got) != 2 || !strings.HasPrefix((*got)[0].ctype, "multipart/") || !strings.HasPrefix((*got)[1].ctype, "multipart/") {
		t.Errorf("requests %+v", *got)
	}
	if !bytes.Equal((*got)[1].file, photo.Data) {
		t.Error("the retry lost the file")
	}
	if len(clock.sleeps) != 1 || clock.sleeps[0] != 500*time.Millisecond {
		t.Errorf("sleeps %v", clock.sleeps)
	}
}

func TestSendWithImageServerErrorIsNotRejected(t *testing.T) {
	srv, got := recordingServer(t, status(502))
	p, _ := testPool(t, false, nil)
	if err := p.SendWithImage(srv.URL, embeds, photo); err == nil || errors.Is(err, ErrRejected) || len(*got) != 1 {
		t.Errorf("err %v, %d requests", err, len(*got))
	}
}

func TestSendRejectedByPayloadTooLarge(t *testing.T) {
	srv, _ := recordingServer(t, status(413))
	p, _ := testPool(t, false, nil)
	if err := p.Send(srv.URL, embeds); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "413") {
		t.Errorf("err %v", err)
	}
}

func TestSendWithImageDryRun(t *testing.T) {
	srv, got := recordingServer(t, ok)
	var buf bytes.Buffer
	p, _ := testPool(t, true, slog.New(slog.NewTextHandler(&buf, nil)))
	if err := p.SendWithImage(srv.URL, embeds, photo); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if len(*got) != 0 || !strings.Contains(out, "attachment://doink.jpg") || !strings.Contains(out, "with doink.jpg (0.0 MB)") {
		t.Errorf("requests %d, log %q", len(*got), out)
	}
	if strings.Contains(out, "not really a jpeg") {
		t.Error("the log has the file's bytes")
	}
}

func TestUploadClientKeepsALongTimeout(t *testing.T) {
	p := NewPool(nil, false, "1", nil)
	if p.client.Timeout != 15*time.Second || p.upload.Timeout != uploadTimeout {
		t.Errorf("timeouts %v / %v", p.client.Timeout, p.upload.Timeout)
	}
}
