package updates

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// File names used by the swap, all inside the app folder.
const (
	ExeName = "doink.exe"
	NewName = "doink.exe.new"
	OldName = "doink.exe.old"
	// MaxDownload caps the zip and the exe inside it (doink.exe is ~15 MB).
	MaxDownload = 64 << 20
)

// maxDownload is MaxDownload; a variable so a test can shrink it.
var maxDownload int64 = MaxDownload

// stallTimeout is how long a download may go without receiving a byte.
// There is no limit on the total time: a slow link still finishes.
var stallTimeout = 30 * time.Second

var errTooBig = errors.New("the download is far larger than a DOINK release")

var sha256RE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Installer downloads and stages a release, then swaps it in. A running
// exe can be renamed on Windows (not overwritten), so the swap is two
// renames; the caller starts the new exe and quits.
type Installer struct {
	Client  *http.Client // nil: limits on connecting only (see stallTimeout)
	Dir     string       // the app folder
	Prefix  string       // DownloadPrefix; tests use an httptest URL
	Version string       // for the User-Agent
}

func (in *Installer) client() *http.Client {
	if in.Client != nil {
		return in.Client
	}
	return &http.Client{Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 15 * time.Second}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}}
}

// get downloads url (which must start with Prefix) into memory.
func (in *Installer) get(url, what string, progress func(string)) ([]byte, error) {
	if !strings.HasPrefix(url, in.Prefix) {
		return nil, errors.New("refusing to download from anywhere but GitHub")
	}
	// Cancelled when no data arrives for stallTimeout; every read resets it.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stall := time.AfterFunc(stallTimeout, cancel)
	defer stall.Stop()
	fail := func(err error) ([]byte, error) {
		if ctx.Err() != nil {
			err = fmt.Errorf("no data for %d s", int(stallTimeout.Seconds()))
		}
		return nil, fmt.Errorf("download failed: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fail(err)
	}
	req.Header.Set("User-Agent", userAgent(in.Version))
	resp, err := in.client().Do(req)
	if err != nil {
		return fail(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}
	var buf bytes.Buffer
	chunk := make([]byte, 256<<10)
	last := ""
	for {
		n, err := resp.Body.Read(chunk)
		stall.Reset(stallTimeout)
		buf.Write(chunk[:n])
		if int64(buf.Len()) > maxDownload {
			return nil, errTooBig
		}
		if n > 0 && progress != nil {
			// Only when the shown text changes: reads are often 16 KB.
			if line := fmt.Sprintf("Downloading %s… %.1f MB", what, float64(buf.Len())/(1<<20)); line != last {
				last = line
				progress(line)
			}
		}
		if err == io.EOF {
			return buf.Bytes(), nil
		}
		if err != nil {
			return fail(err)
		}
	}
}

// Download fetches info's zip and checksum, verifies them and writes
// Dir/doink.exe.new. progress (may be nil) gets short status lines. Every
// error's text is meant to be shown to the user as it is.
func (in *Installer) Download(info *Info, progress func(string)) error {
	if info == nil || !info.Installable() {
		return errors.New("this release has no companion download")
	}
	if !strings.HasPrefix(info.ZipURL, in.Prefix) || !strings.HasPrefix(info.SHA256URL, in.Prefix) {
		return errors.New("refusing to download from anywhere but GitHub")
	}
	data, err := in.get(info.ZipURL, "v"+info.Latest, progress)
	if err != nil {
		return err
	}
	sumFile, err := in.get(info.SHA256URL, "checksum", nil)
	if err != nil {
		return err
	}
	want := ""
	if f := strings.Fields(string(sumFile)); len(f) > 0 {
		want = strings.ToLower(f[0])
	}
	got := sha256.Sum256(data)
	if !sha256RE.MatchString(want) || want != hex.EncodeToString(got[:]) {
		return errors.New("the download's checksum doesn't match the one published with the release, so it wasn't installed")
	}
	if progress != nil {
		progress("Verified. Unpacking…")
	}
	exe, err := extractExe(data)
	if err != nil {
		return err
	}
	if err := in.writeNew(exe); err != nil {
		return err
	}
	logger().Info(fmt.Sprintf("update: staged v%s (%d bytes)", info.Latest, len(exe)))
	return nil
}

// extractExe returns <top>/Companion/doink.exe from the release zip.
func extractExe(data []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("the release zip couldn't be read")
	}
	var found *zip.File
	for _, f := range zr.File {
		parts := strings.Split(strings.ReplaceAll(f.Name, "\\", "/"), "/")
		if len(parts) != 3 || parts[1] != "Companion" || parts[2] != ExeName || f.FileInfo().IsDir() {
			continue
		}
		if found != nil {
			return nil, errors.New("the release zip has no doink.exe in it")
		}
		found = f
	}
	if found == nil {
		return nil, errors.New("the release zip has no doink.exe in it")
	}
	if int64(found.UncompressedSize64) > maxDownload {
		return nil, errTooBig
	}
	rc, err := found.Open()
	if err != nil {
		return nil, errors.New("the release zip couldn't be read")
	}
	defer rc.Close()
	exe, err := io.ReadAll(io.LimitReader(rc, maxDownload+1))
	if err != nil {
		return nil, errors.New("the release zip couldn't be read")
	}
	if int64(len(exe)) > maxDownload {
		return nil, errTooBig
	}
	if !bytes.HasPrefix(exe, []byte("MZ")) {
		return nil, errors.New("the release's doink.exe isn't a Windows program")
	}
	return exe, nil
}

// writeNew writes exe to doink.exe.new through a temp file in the same
// folder; nothing is left behind on failure.
func (in *Installer) writeNew(exe []byte) error {
	tmp, err := os.CreateTemp(in.Dir, "doink-update-*.tmp")
	if err != nil {
		return fmt.Errorf("couldn't write to %s: %w", in.Dir, err)
	}
	name := tmp.Name()
	_, werr := tmp.Write(exe)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(name)
		return fmt.Errorf("couldn't write the update: %w", errors.Join(werr, cerr))
	}
	dst := filepath.Join(in.Dir, NewName)
	_ = os.Remove(dst)
	if err := os.Rename(name, dst); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("couldn't write the update: %w", err)
	}
	return nil
}

// Swap renames doink.exe to doink.exe.old and doink.exe.new to doink.exe.
// If the second rename fails the first is undone.
func (in *Installer) Swap() error {
	exe := filepath.Join(in.Dir, ExeName)
	newer := filepath.Join(in.Dir, NewName)
	old := filepath.Join(in.Dir, OldName)
	if _, err := os.Stat(newer); err != nil {
		return fmt.Errorf("the downloaded update is missing: %w", err)
	}
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return fmt.Errorf("couldn't replace doink.exe (is the folder writable?): %w", err)
	}
	if err := os.Rename(newer, exe); err != nil {
		_ = os.Rename(old, exe) // best effort: put the old exe back
		return fmt.Errorf("couldn't replace doink.exe: %w", err)
	}
	return nil
}

// Rollback undoes Swap: doink.exe back to doink.exe.new, doink.exe.old
// back to doink.exe.
func (in *Installer) Rollback() error {
	exe := filepath.Join(in.Dir, ExeName)
	newer := filepath.Join(in.Dir, NewName)
	old := filepath.Join(in.Dir, OldName)
	if err := os.Rename(exe, newer); err != nil {
		return err
	}
	if err := os.Rename(old, exe); err != nil {
		_ = os.Rename(newer, exe) // keep some doink.exe in place
		return err
	}
	return nil
}

// Cleanup removes doink.exe.old and a stale doink.exe.new; files that
// aren't there are fine.
func Cleanup(dir string) error {
	var errs []error
	for _, name := range []string{OldName, NewName} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
