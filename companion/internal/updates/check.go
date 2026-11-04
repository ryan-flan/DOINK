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
	// ReleaseListURL is GitHub's API for the 20 newest releases, newest
	// first. The newest one carrying the companion zip is the latest
	// companion; addon-only releases (no DOINK-<tag>.zip) are skipped.
	ReleaseListURL = "https://api.github.com/repos/ryan-flan/DOINK/releases?per_page=20"
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

// FetchLatest asks GitHub for the newest companion release: the first
// entry of the release list (newest first) that is neither a draft nor a
// prerelease, whose tag is a version, and that carries DOINK-<tag>.zip
// and its .sha256 on github.com. Releases without them (addon-only) are
// skipped, whatever the Latest badge says. A nil client gets one with a
// 10 s timeout. Failures are logged at INFO and returned.
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
	var releases []struct {
		TagName    string `json:"tag_name"`
		HTMLURL    string `json:"html_url"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &releases); err != nil {
		return fail(errors.New("unexpected answer from GitHub"))
	}
	for _, r := range releases {
		if r.Draft || r.Prerelease {
			continue
		}
		tag := strings.TrimSpace(r.TagName)
		if _, ok := ParseVersion(tag); !ok {
			continue
		}
		var zip, sha string
		for _, a := range r.Assets {
			if !strings.HasPrefix(a.URL, DownloadPrefix) {
				continue
			}
			switch a.Name {
			case "DOINK-" + tag + ".zip":
				zip = a.URL
			case "DOINK-" + tag + ".zip.sha256":
				sha = a.URL
			}
		}
		if zip == "" || sha == "" {
			continue
		}
		url := r.HTMLURL
		if !strings.HasPrefix(url, "https://github.com/") {
			url = ReleasesURL
		}
		return &Info{Current: current, Latest: strings.TrimPrefix(tag, "v"), URL: url,
			ZipURL: zip, SHA256URL: sha, CheckedAt: time.Now()}, nil
	}
	return fail(errors.New("no companion release found"))
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
