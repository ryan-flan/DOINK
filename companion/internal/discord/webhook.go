package discord

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MaxEmbedsPerMessage is Discord's limit.
const MaxEmbedsPerMessage = 10

// minPostInterval keeps each webhook under its ~30/min limit.
const minPostInterval = 2 * time.Second

const maxAttempts = 5

// Sender posts embeds to a webhook URL.
type Sender interface {
	Send(url string, embeds []Embed) error
}

type hook struct {
	mu   sync.Mutex
	next time.Time
}

// Pool keeps one rate-limit schedule per webhook URL.
type Pool struct {
	client  *http.Client
	dryRun  bool
	version string
	log     *slog.Logger
	now     func() time.Time
	sleep   func(time.Duration)

	mu    sync.Mutex
	hooks map[string]*hook
}

// NewPool returns a Pool. A nil client gets one with a 15 s timeout.
func NewPool(client *http.Client, dryRun bool, version string, log *slog.Logger) *Pool {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	if log == nil {
		log = slog.Default()
	}
	return &Pool{
		client: client, dryRun: dryRun, version: version, log: log,
		now: time.Now, sleep: time.Sleep, hooks: map[string]*hook{},
	}
}

func (p *Pool) hook(url string) *hook {
	p.mu.Lock()
	defer p.mu.Unlock()
	h := p.hooks[url]
	if h == nil {
		h = &hook{}
		p.hooks[url] = h
	}
	return h
}

// Send posts one message with embeds to url, waiting out the webhook's
// schedule and retrying rate limits.
func (p *Pool) Send(url string, embeds []Embed) error {
	payload := map[string]any{"embeds": embeds}
	if p.dryRun {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		_ = enc.Encode(payload)
		p.log.Info("dry run, would post:\n" + strings.TrimRight(buf.String(), "\n"))
		return nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	target := url + "?wait=true" // wait=true: get real errors back
	if strings.Contains(url, "?") {
		target = url + "&wait=true"
	}

	h := p.hook(url)
	h.mu.Lock()
	defer h.mu.Unlock()
	if wait := h.next.Sub(p.now()); wait > 0 {
		p.sleep(wait)
	}
	for attempt := 0; attempt < maxAttempts; attempt++ {
		req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("network error: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "DOINK-companion/"+p.version)
		resp, err := p.client.Do(req)
		if err != nil {
			return fmt.Errorf("network error: %v", err)
		}
		detail, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			wait := minPostInterval
			if resp.Header.Get("X-RateLimit-Remaining") == "0" {
				if s, err := strconv.ParseFloat(resp.Header.Get("X-RateLimit-Reset-After"), 64); err == nil {
					wait = max(wait, time.Duration(s*float64(time.Second)))
				}
			}
			h.next = p.now().Add(wait)
			return nil
		case resp.StatusCode == http.StatusTooManyRequests:
			retry := 1.0
			var parsed struct {
				RetryAfter *float64 `json:"retry_after"`
			}
			if json.Unmarshal(detail, &parsed) == nil && parsed.RetryAfter != nil {
				retry = *parsed.RetryAfter
			}
			p.log.Info(fmt.Sprintf("rate limited; retrying in %.1fs", retry))
			p.sleep(time.Duration(retry * float64(time.Second)))
		default:
			if len(detail) > 200 {
				detail = detail[:200]
			}
			return fmt.Errorf("HTTP %d: %s", resp.StatusCode, detail)
		}
	}
	return errors.New("still rate limited after 5 attempts")
}
