package runner

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/ryan-flan/DOINK/companion/internal/savedvars"
)

// Forwarder POSTs one event's JSON to forward_url.
type Forwarder func(target string, body []byte) error

const forwardAttempts = 2

// forwardPause is the wait between the two attempts (a var for the tests).
var forwardPause = time.Second

// forwardCoolDown: after a failed delivery, calls to the same address fail
// at once for this long. Each PostEvents call gives up at its first
// failure, but realtime makes one call per screenshot; without this an
// endpoint that swallows connections would cost every one of them two
// timeouts.
const forwardCoolDown = time.Minute

// HTTPForwarder forwards with client (nil: a 5 s timeout, redirects not
// followed: a redirected POST arrives as a GET without the event). Any
// 2xx answer counts as delivered; anything else is tried once more.
func HTTPForwarder(client *http.Client, version string) Forwarder {
	if client == nil {
		client = &http.Client{
			Timeout:       5 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	var mu sync.Mutex
	var downURL string
	var downUntil time.Time
	var downErr error
	return func(target string, body []byte) error {
		mu.Lock()
		defer mu.Unlock()
		if target == downURL && forwardNow().Before(downUntil) {
			return fmt.Errorf("%w (not tried again yet)", downErr)
		}
		var err error
		for attempt := range forwardAttempts {
			if attempt > 0 {
				time.Sleep(forwardPause)
			}
			if err = forwardOnce(client, version, target, body); err == nil {
				downURL = ""
				return nil
			}
		}
		downURL, downUntil, downErr = target, forwardNow().Add(forwardCoolDown), err
		return err
	}
}

// forwardNow is time.Now (a var for the tests).
var forwardNow = time.Now

func forwardOnce(client *http.Client, version, target string, body []byte) error {
	req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return errors.New("bad forward_url")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "DOINK-companion/"+version)
	resp, err := client.Do(req)
	if err != nil {
		// A *url.Error spells out the whole URL, which may hold a token.
		var ue *url.Error
		if errors.As(err, &ue) {
			return ue.Err
		}
		return err
	}
	defer resp.Body.Close() //nolint:errcheck // nothing to do about it
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

// forwardHost is the part of forward_url that is safe to log.
func forwardHost(target string) string {
	if u, err := url.Parse(target); err == nil && u.Host != "" {
		return u.Host
	}
	return "forward_url"
}

// forwardEvents sends events to forward_url in order and stops at the
// first failure. It returns how many were delivered. In a dry run it only
// says what it would send.
func forwardEvents(d Deps, key string, events []savedvars.Event) (int, error) {
	target := d.Config.ForwardURL
	host := forwardHost(target)
	for i, e := range events {
		if len(e.Raw) == 0 {
			continue // not read from the game: nothing to pass on
		}
		if !d.Config.DryRun && d.Forward == nil {
			return i, errors.New("forwarding isn't set up")
		}
		if d.Config.DryRun {
			d.Log.Info(fmt.Sprintf("%s: would forward #%d %s to %s: %s", key, e.Seq, e.Type, host, e.Raw))
			continue
		}
		if err := d.Forward(target, e.Raw); err != nil {
			return i, err
		}
		d.Log.Info(fmt.Sprintf("%s: forwarded #%d %s to %s", key, e.Seq, e.Type, host))
	}
	return len(events), nil
}
