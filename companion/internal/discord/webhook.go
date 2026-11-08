package discord

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/textproto"
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

// uploadTimeout covers a screenshot of several hundred KB on a slow link;
// the plain client's 15 s is for small JSON posts.
const uploadTimeout = 90 * time.Second

// ErrRejected: Discord answered 400 or 413, so the message itself is wrong
// (or too big) and sending it again can't help. Other failures are worth a
// retry.
var ErrRejected = errors.New("discord rejected the message")

// Attachment is a file posted with the message and shown as the first
// embed's image.
type Attachment struct {
	Name string // "doink.jpg"
	Data []byte
	Mime string // "image/jpeg"
}

// Sender posts embeds to a webhook URL.
type Sender interface {
	Send(url string, embeds []Embed) error
	// SendWithImage is Send with a picture on the first embed; a nil img
	// is the same as Send.
	SendWithImage(url string, embeds []Embed, img *Attachment) error
}

type hook struct {
	mu   sync.Mutex
	next time.Time
}

// Pool keeps one rate-limit schedule per webhook URL.
type Pool struct {
	client  *http.Client
	upload  *http.Client // client with a timeout that fits an upload
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
	up := *client
	if up.Timeout != 0 && up.Timeout < uploadTimeout {
		up.Timeout = uploadTimeout
	}
	return &Pool{
		client: client, upload: &up, dryRun: dryRun, version: version, log: log,
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
	return p.SendWithImage(url, embeds, nil)
}

// payload is the message's JSON. With an image, the first embed (a copy;
// the caller's embeds are left alone) points at the attached file.
func payload(embeds []Embed, img *Attachment) map[string]any {
	if img == nil || len(embeds) == 0 {
		return map[string]any{"embeds": embeds}
	}
	out := make([]Embed, len(embeds))
	copy(out, embeds)
	first := Embed{}
	for k, v := range embeds[0] {
		first[k] = v
	}
	first["image"] = map[string]any{"url": "attachment://" + img.Name}
	out[0] = first
	return map[string]any{
		"embeds":      out,
		"attachments": []map[string]any{{"id": 0, "filename": img.Name}},
	}
}

var quoteEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

// encodeBody returns the request body and its content type: JSON, or
// multipart/form-data when there is a file.
func encodeBody(embeds []Embed, img *Attachment) ([]byte, string, error) {
	body, err := json.Marshal(payload(embeds, img))
	if err != nil {
		return nil, "", err
	}
	if img == nil {
		return body, "application/json", nil
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("payload_json", string(body)); err != nil {
		return nil, "", err
	}
	part, err := w.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {fmt.Sprintf(`form-data; name="files[0]"; filename="%s"`, quoteEscaper.Replace(img.Name))},
		"Content-Type":        {img.Mime},
	})
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(img.Data); err != nil {
		return nil, "", err
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}

// SendWithImage is Send with img uploaded and shown on the first embed. If
// Discord rejects the upload (400, or 413 for a file too big) the message
// is sent once more without the picture, so the event still posts.
func (p *Pool) SendWithImage(url string, embeds []Embed, img *Attachment) error {
	if p.dryRun {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		_ = enc.Encode(payload(embeds, img))
		msg := "dry run, would post:\n" + strings.TrimRight(buf.String(), "\n")
		if img != nil {
			msg += fmt.Sprintf("\nwith %s (%.1f MB)", img.Name, float64(len(img.Data))/(1<<20))
		}
		p.log.Info(msg)
		return nil
	}
	body, ctype, err := encodeBody(embeds, img)
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
	client := p.client
	if img != nil {
		client = p.upload
	}
	err = p.post(h, target, body, ctype, client)
	if img != nil && errors.Is(err, ErrRejected) {
		p.log.Warn(fmt.Sprintf("Discord refused the screenshot (%v); posting without it", err))
		body, ctype, encErr := encodeBody(embeds, nil)
		if encErr != nil {
			return encErr
		}
		return p.post(h, target, body, ctype, p.client)
	}
	return err
}

// post sends body, retrying rate limits. h.mu must be held.
func (p *Pool) post(h *hook, target string, body []byte, ctype string, client *http.Client) error {
	for attempt := 0; attempt < maxAttempts; attempt++ {
		req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("network error: %w", err)
		}
		req.Header.Set("Content-Type", ctype)
		req.Header.Set("User-Agent", "DOINK-companion/"+p.version)
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("network error: %w", err)
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
		case resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusRequestEntityTooLarge:
			return fmt.Errorf("%w: HTTP %d: %s", ErrRejected, resp.StatusCode, head(detail))
		default:
			return fmt.Errorf("HTTP %d: %s", resp.StatusCode, head(detail))
		}
	}
	return fmt.Errorf("still rate limited after %d attempts", maxAttempts)
}

// head is the start of an error body, for a log line.
func head(detail []byte) []byte {
	if len(detail) > 200 {
		detail = detail[:200]
	}
	return detail
}
