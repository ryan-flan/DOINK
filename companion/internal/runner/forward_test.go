package runner

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

const forwardTo = "https://bot.example/doink?token=s3cret"

// fakeForward records bodies; failAt (1-based) and later calls fail.
type fakeForward struct {
	mu     sync.Mutex
	bodies []string
	urls   []string
	calls  int
	failAt int
}

func (ff *fakeForward) send(target string, body []byte) error {
	ff.mu.Lock()
	defer ff.mu.Unlock()
	ff.calls++
	if ff.failAt > 0 && ff.calls >= ff.failAt {
		return errors.New("HTTP 503")
	}
	ff.bodies = append(ff.bodies, string(body))
	ff.urls = append(ff.urls, target)
	return nil
}

func withForward(f *fixture) *fakeForward {
	ff := &fakeForward{}
	f.cfg.ForwardURL = forwardTo
	f.forward = ff.send
	return ff
}

// rawEvent is what eventsFile writes for one seq, unescaped.
func rawEvent(seq int) string { return eventJSON(seq) }

func TestForwardSendsEachEventsExactJSONOnceInOrder(t *testing.T) {
	f := newFixture(t)
	ff := withForward(f)
	ok, s := f.run(eventsFile(2, 1, 3), nil)
	want := []string{rawEvent(1), rawEvent(2), rawEvent(3)}
	if !ok || !reflect.DeepEqual(ff.bodies, want) {
		t.Fatalf("ok %v forwarded %v", ok, ff.bodies)
	}
	if ff.urls[0] != forwardTo || len(f.sender.sent) != 1 {
		t.Errorf("urls %v discord %v", ff.urls, f.sender.titles())
	}
	// Same file again, and after a restart: nothing new.
	f.run(eventsFile(1, 2, 3), s)
	f.run(eventsFile(1, 2, 3), nil)
	if len(ff.bodies) != 3 {
		t.Errorf("forwarded again: %v", ff.bodies)
	}
	for _, r := range f.logs.recs {
		if strings.Contains(r, "s3cret") {
			t.Errorf("log shows the URL: %s", r)
		}
	}
	if !f.logs.has("INFO", "Flano-R: forwarded #1 level_up to bot.example") {
		t.Errorf("logs %v", f.logs.recs)
	}
}

func TestForwardOffByDefault(t *testing.T) {
	f := newFixture(t)
	ff := &fakeForward{}
	f.forward = ff.send
	if ok, _ := f.run(eventsFile(1), nil); !ok || ff.calls != 0 {
		t.Errorf("ok %v calls %d", ok, ff.calls)
	}
}

func TestForwardDownNeverHoldsDiscordUp(t *testing.T) {
	f := newFixture(t)
	ff := withForward(f)
	ff.failAt = 2
	// 25 events = three Discord messages; forwarding dies on the second event.
	f.cfg.MaxBacklog = 25
	ok, s := f.run(eventsFile(span(1, 25)...), nil)
	if !ok || len(f.sender.sent) != 3 || lastSeen(s, "Flano-R") != 25 {
		t.Fatalf("ok %v messages %d last %d", ok, len(f.sender.sent), lastSeen(s, "Flano-R"))
	}
	if len(ff.bodies) != 1 || ff.calls != 2 {
		t.Errorf("forwarded %d, calls %d: the rest must be skipped without trying", len(ff.bodies), ff.calls)
	}
	if !f.logs.has("WARN", "Flano-R: forwarding to bot.example failed: HTTP 503; 24 events not forwarded") || f.logs.count("WARN") != 1 {
		t.Errorf("logs %v", f.logs.recs)
	}
	if f.status.Error() != "" {
		t.Errorf("status %q: a forwarding failure isn't a DOINK problem", f.status.Error())
	}
	// Not retried later: best effort.
	ff.failAt = 0
	f.run(eventsFile(span(1, 25)...), s)
	if len(ff.bodies) != 1 {
		t.Errorf("forwarded later: %d", len(ff.bodies))
	}
}

func TestForwardNotRepeatedWhenDiscordRetries(t *testing.T) {
	f := newFixture(t)
	ff := withForward(f)
	f.sender.fail = true
	ok, s := f.run(eventsFile(1, 2), nil)
	if ok || ff.calls != 0 {
		t.Fatalf("ok %v calls %d: nothing is forwarded before Discord has it", ok, ff.calls)
	}
	f.sender.fail = false
	if ok, _ = f.run(eventsFile(1, 2), s); !ok || len(ff.bodies) != 2 {
		t.Errorf("ok %v forwarded %v", ok, ff.bodies)
	}
}

func TestForwardAlsoGetsWhatDiscordRejected(t *testing.T) {
	f := newFixture(t)
	ff := withForward(f)
	f.sender.rejectNext = 1
	if ok, _ := f.run(eventsFile(1), nil); !ok || len(ff.bodies) != 1 {
		t.Errorf("ok %v forwarded %v", ok, ff.bodies)
	}
}

func TestForwardOnlyNeedsNoWebhook(t *testing.T) {
	f := newFixture(t)
	ff := withForward(f)
	f.cfg.WebhookURL = ""
	ok, s := f.run(eventsFile(1, 2), nil)
	if !ok || len(ff.bodies) != 2 || len(f.sender.sent) != 0 || lastSeen(s, "Flano-R") != 2 {
		t.Fatalf("ok %v forwarded %v discord %v", ok, ff.bodies, f.sender.titles())
	}
	if f.logs.count("ERROR") != 0 || f.status.Error() != "" {
		t.Errorf("logs %v status %q", f.logs.recs, f.status.Error())
	}
	// A webhook set later doesn't post the old ones.
	ok, _ = f.run(webhooksBlock(map[string]string{"*": gameHook})+eventsFile(1, 2), s)
	if !ok || len(f.sender.sent) != 0 || len(ff.bodies) != 2 {
		t.Errorf("ok %v discord %v forwarded %v", ok, f.sender.titles(), ff.bodies)
	}
}

func TestForwardOnlyRetriesWhatFailed(t *testing.T) {
	f := newFixture(t)
	ff := withForward(f)
	f.cfg.WebhookURL = ""
	ff.failAt = 2
	ok, s := f.run(eventsFile(1, 2, 3), nil)
	if ok || len(ff.bodies) != 1 {
		t.Fatalf("ok %v forwarded %v", ok, ff.bodies)
	}
	if !f.logs.has("ERROR", "Flano-R: forwarding to bot.example failed: HTTP 503") ||
		f.status.Error() != "forwarding to bot.example failed: HTTP 503" {
		t.Errorf("logs %v status %q", f.logs.recs, f.status.Error())
	}
	ff.failAt = 0
	ok, _ = f.run(eventsFile(1, 2, 3), s)
	if !ok || !reflect.DeepEqual(ff.bodies, []string{rawEvent(1), rawEvent(2), rawEvent(3)}) {
		t.Errorf("ok %v forwarded %v", ok, ff.bodies)
	}
}

func TestForwardBadWebhookIsStillAnError(t *testing.T) {
	f := newFixture(t)
	ff := withForward(f)
	ok, _ := f.run(webhooksBlock(map[string]string{"*": "https://evil.example/x"})+eventsFile(1), nil)
	if ok || ff.calls != 0 {
		t.Errorf("ok %v calls %d", ok, ff.calls)
	}
}

func TestForwardDryRunSendsNothing(t *testing.T) {
	f := newFixture(t)
	ff := withForward(f)
	f.cfg.DryRun = true
	ok, _ := f.run(eventsFile(1), nil)
	if !ok || ff.calls != 0 || !f.logs.has("INFO", "Flano-R: would forward #1 level_up to bot.example: "+rawEvent(1)) {
		t.Errorf("ok %v calls %d logs %v", ok, ff.calls, f.logs.recs)
	}
}

func TestForwardFromAScreenshot(t *testing.T) {
	sf := newShotFixture(t)
	ff := withForward(sf.fixture)
	sf.write("WoWScrnShot_110226_230648.jpg", pngOf(t, eventJSON(5), 0))
	s := sf.state()
	sf.scan(s)
	if !reflect.DeepEqual(ff.bodies, []string{eventJSON(5)}) || len(sf.sender.sent) != 1 {
		t.Fatalf("forwarded %v discord %v", ff.bodies, sf.sender.titles())
	}
	// The file pass after /reload must not forward it again.
	sf.run("DOINKDB = {\n"+eventsFile(5)+"\n}", s)
	if len(ff.bodies) != 1 {
		t.Errorf("forwarded twice: %v", ff.bodies)
	}
}

func TestHTTPForwarder(t *testing.T) {
	old := forwardPause
	forwardPause = 0
	now := time.Now()
	forwardNow = func() time.Time { return now }
	defer func() { forwardPause, forwardNow = old, time.Now }()

	var mu sync.Mutex
	var got []string
	status := []int{http.StatusBadGateway, http.StatusNoContent}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		got = append(got, r.Method+" "+r.Header.Get("Content-Type")+" "+r.Header.Get("User-Agent")+" "+string(body))
		code := status[0]
		if len(status) > 1 {
			status = status[1:]
		}
		w.WriteHeader(code)
	}))
	defer srv.Close()

	fwd := HTTPForwarder(srv.Client(), "1.2.3")
	if err := fwd(srv.URL+"/in?token=s3cret", []byte(`{"seq":1}`)); err != nil {
		t.Fatalf("second attempt should deliver: %v", err)
	}
	want := `POST application/json DOINK-companion/1.2.3 {"seq":1}`
	if len(got) != 2 || got[1] != want {
		t.Errorf("got %v", got)
	}

	status = []int{http.StatusInternalServerError}
	got = nil
	err := fwd(srv.URL+"/in?token=s3cret", []byte(`{}`))
	if err == nil || err.Error() != "HTTP 500" || len(got) != forwardAttempts {
		t.Errorf("err %v after %d attempts", err, len(got))
	}

	// Cooling down: fails at once, without a request.
	err = fwd(srv.URL+"/in?token=s3cret", []byte(`{}`))
	if err == nil || err.Error() != "HTTP 500 (not tried again yet)" || len(got) != forwardAttempts {
		t.Errorf("err %v, %d requests", err, len(got))
	}
	// Another address isn't held back, and after a minute this one is tried again.
	status = []int{http.StatusOK}
	if err := fwd(srv.URL+"/other", []byte(`{}`)); err != nil {
		t.Errorf("other address: %v", err)
	}
	now = now.Add(forwardCoolDown + time.Second)
	if err := fwd(srv.URL+"/in?token=s3cret", []byte(`{}`)); err != nil {
		t.Errorf("after the cool-down: %v", err)
	}

	srv.Close()
	err = fwd(srv.URL+"/in?token=s3cret", []byte(`{}`))
	if err == nil || strings.Contains(err.Error(), "s3cret") {
		t.Errorf("err %v: must fail without showing the URL", err)
	}
}

func TestHTTPForwarderDoesNotFollowRedirects(t *testing.T) {
	old := forwardPause
	forwardPause = 0
	defer func() { forwardPause = old }()
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path == "/in" {
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
		}
	}))
	defer srv.Close()
	err := HTTPForwarder(nil, "1.2.3")(srv.URL+"/in", []byte(`{}`))
	if err == nil || err.Error() != "HTTP 302" || hits != forwardAttempts {
		t.Errorf("err %v, %d requests", err, hits)
	}
}

func TestForwardWithoutAForwarderLosesNothing(t *testing.T) {
	f := newFixture(t)
	f.cfg.ForwardURL, f.cfg.WebhookURL = forwardTo, ""
	ok, s := f.run(eventsFile(1), nil)
	if ok || lastSeen(s, "Flano-R") != -1 {
		t.Errorf("ok %v last %d: must not be recorded as delivered", ok, lastSeen(s, "Flano-R"))
	}
}

// Logins and logouts (record-only) are forwarded and recorded, never posted.
func TestRecordOnlyEventsAreForwardedNotPosted(t *testing.T) {
	session := func(seq int, st string) string {
		return fmt.Sprintf(`"{\"char\":\"Flano\",\"class\":\"MAGE\",\"data\":{\"state\":\"%s\"},\"realm\":\"R\",\"seq\":%d,\"ts\":1790870764,\"type\":\"session\"}",`, st, seq)
	}
	file := "[\"events\"] = {\n" + session(1, "login") + "\n" +
		strings.TrimSuffix(strings.TrimPrefix(eventsFile(2), "[\"events\"] = {\n"), "\n},") + "\n" +
		session(3, "logout") + "\n},"

	f := newFixture(t)
	ff := withForward(f)
	ok, s := f.run(file, nil)
	if !ok {
		t.Fatalf("logs %v", f.logs.recs)
	}
	if got := f.sender.titles(); len(got) != 1 || !strings.Contains(got[0], "level 2") {
		t.Errorf("discord got %v, want only the level-up", got)
	}
	if len(ff.bodies) != 3 || !strings.Contains(ff.bodies[0], `"state":"login"`) || !strings.Contains(ff.bodies[2], `"state":"logout"`) {
		t.Errorf("forwarded %v", ff.bodies)
	}
	if n := lastSeen(s, "Flano-R"); n != 3 {
		t.Errorf("last seen %d, want 3: a login must not wait for anything", n)
	}

	// Without forward_url they are recorded and dropped, still never posted.
	g := newFixture(t)
	if ok, s := g.run(file, nil); !ok || len(g.sender.sent) != 1 || lastSeen(s, "Flano-R") != 3 {
		t.Errorf("ok %v discord %v", ok, g.sender.titles())
	}

	// forward_url and no webhook: everything is forwarded, nothing posted.
	h := newFixture(t)
	h.cfg.WebhookURL = ""
	hf := withForward(h)
	if ok, s := h.run(file, nil); !ok || len(hf.bodies) != 3 || len(h.sender.sent) != 0 || lastSeen(s, "Flano-R") != 3 {
		t.Errorf("forward-only: ok %v forwarded %d discord %v", ok, len(hf.bodies), h.sender.titles())
	}
}
