package updates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Log receives update-check messages; nil means slog.Default().
var Log *slog.Logger

func logger() *slog.Logger {
	if Log != nil {
		return Log
	}
	return slog.Default()
}

const (
	// LatestURL is GitHub's API for the newest release.
	LatestURL = "https://api.github.com/repos/ryan-flan/DOINK/releases/latest"
	// ReleasesURL is the download page used when the API's link looks odd.
	ReleasesURL = "https://github.com/ryan-flan/DOINK/releases"
	// DownloadPrefix is where a release's files must come from.
	DownloadPrefix = "https://github.com/ryan-flan/DOINK/releases/download/"
	// FirstCheck, Interval and Retry time the background checks.
	FirstCheck = 30 * time.Second
	Interval   = 24 * time.Hour
	Retry      = time.Hour
)

func userAgent(version string) string {
	return "DOINK-companion/" + version + " (+https://github.com/ryan-flan/DOINK)"
}

// FetchLatest asks GitHub for the latest release. A nil client gets one
// with a 10 s timeout. Failures are logged at INFO and returned.
func FetchLatest(client *http.Client, apiURL, current, version string) (*Info, error) {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	fail := func(err error) (*Info, error) {
		logger().Info("update check failed: " + err.Error())
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return fail(err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent(version))
	resp, err := client.Do(req)
	if err != nil {
		return fail(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fail(fmt.Errorf("GitHub answered HTTP %d", resp.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fail(err)
	}
	var data struct {
		TagName any `json:"tag_name"`
		HTMLURL any `json:"html_url"`
		Assets  any `json:"assets"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return fail(errors.New("unexpected answer from GitHub"))
	}
	tag, _ := data.TagName.(string)
	if _, ok := ParseVersion(tag); !ok {
		return fail(errors.New("unexpected answer from GitHub"))
	}
	url, _ := data.HTMLURL.(string)
	if !strings.HasPrefix(url, "https://github.com/") {
		url = ReleasesURL
	}
	tag = strings.TrimSpace(tag)
	info := &Info{Current: current, Latest: strings.TrimPrefix(tag, "v"), URL: url, CheckedAt: time.Now()}
	assets, _ := data.Assets.([]any)
	for _, a := range assets {
		m, _ := a.(map[string]any)
		name, _ := m["name"].(string)
		link, _ := m["browser_download_url"].(string)
		if !strings.HasPrefix(link, DownloadPrefix) {
			continue
		}
		switch name {
		case "DOINK-" + tag + ".zip":
			info.ZipURL = link
		case "DOINK-" + tag + ".zip.sha256":
			info.SHA256URL = link
		}
	}
	return info, nil
}

// Checker runs checks in the background: the first after `first`, then
// every `interval`, or after `retry` when one failed.
type Checker struct {
	fetch                  func() (*Info, error)
	onResult               func(*Info)
	first, interval, retry time.Duration
}

// NewChecker returns a Checker; onResult hears every successful check.
func NewChecker(fetch func() (*Info, error), onResult func(*Info), first, interval, retry time.Duration) *Checker {
	return &Checker{fetch: fetch, onResult: onResult, first: first, interval: interval, retry: retry}
}

// Start runs the checks on a goroutine until ctx is done.
func (c *Checker) Start(ctx context.Context) {
	go func() {
		wait := c.first
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			if _, err := c.CheckNow(); err != nil {
				wait = c.retry
			} else {
				wait = c.interval
			}
		}
	}()
}

// CheckNow checks synchronously; onResult hears a success.
func (c *Checker) CheckNow() (*Info, error) {
	info, err := c.fetch()
	if err != nil {
		return nil, err
	}
	c.onResult(info)
	return info, nil
}
