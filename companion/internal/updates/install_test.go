package updates

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type entry struct{ name, body string }

func makeZip(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(e.body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sumOf(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// release serves zipData at /zip and sumText at /sha, counting requests.
func release(t *testing.T, zipData []byte, sumText string) (*Installer, *Info, *atomic.Int32, string) {
	t.Helper()
	quiet(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/zip":
			w.Write(zipData)
		case "/sha":
			w.Write([]byte(sumText))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	in := &Installer{Dir: dir, Prefix: srv.URL + "/", Version: "0.1.0"}
	info := &Info{Current: "0.1.0", Latest: "9.9.9", ZipURL: srv.URL + "/zip", SHA256URL: srv.URL + "/sha"}
	return in, info, &hits, dir
}

const exeBody = "MZ fake exe"

func goodZip(t *testing.T) []byte {
	return makeZip(t,
		entry{"DOINK-v9.9.9/AddOns/DOINK/DOINK.toc", "x"},
		entry{"DOINK-v9.9.9/Companion/doink.exe", exeBody},
		entry{"DOINK-v9.9.9/README.md", "r"})
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(path string) bool { _, err := os.Stat(path); return err == nil }

func TestDownloadGood(t *testing.T) {
	z := goodZip(t)
	in, info, _, dir := release(t, z, sumOf(z)+"  DOINK-v9.9.9.zip") // no newline, like release.yml
	var lines []string
	if err := in.Download(info, func(s string) { lines = append(lines, s) }); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(dir, NewName)); got != exeBody {
		t.Errorf("staged %q", got)
	}
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "Downloading v9.9.9") {
		t.Errorf("progress %v", lines)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "doink-update-*")); len(left) != 0 {
		t.Errorf("temp files left: %v", left)
	}
}

func TestDownloadBackslashNames(t *testing.T) {
	z := makeZip(t, entry{`DOINK-v9.9.9\Companion\doink.exe`, exeBody})
	in, info, _, dir := release(t, z, strings.ToUpper(sumOf(z)))
	if err := in.Download(info, nil); err != nil {
		t.Fatal(err)
	}
	if readFile(t, filepath.Join(dir, NewName)) != exeBody {
		t.Error("not staged")
	}
}

func TestDownloadRejects(t *testing.T) {
	good := goodZip(t)
	cases := []struct {
		name string
		zip  []byte
		sum  string
		want string
	}{
		{"sha mismatch", good, sumOf([]byte("other")), "checksum"},
		{"sha not hex", good, strings.Repeat("z", 64), "checksum"},
		{"empty sha file", good, "", "checksum"},
		{"no companion exe", makeZip(t, entry{"DOINK-v9.9.9/README.md", "r"}), "", "no doink.exe"},
		{"exe only under AddOns", makeZip(t, entry{"DOINK-v9.9.9/AddOns/DOINK/doink.exe", exeBody}), "", "no doink.exe"},
		{"two exes", makeZip(t,
			entry{"A/Companion/doink.exe", exeBody},
			entry{"B/Companion/doink.exe", exeBody}), "", "no doink.exe"},
		{"not an exe", makeZip(t, entry{"A/Companion/doink.exe", "#!/bin/sh"}), "", "isn't a Windows program"},
		{"not a zip", []byte("plain text"), "", "couldn't be read"},
	}
	for _, c := range cases {
		sum := c.sum
		if sum == "" && c.want != "checksum" {
			sum = sumOf(c.zip)
		}
		in, info, _, dir := release(t, c.zip, sum)
		err := in.Download(info, nil)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err %v, want %q", c.name, err, c.want)
		}
		if exists(filepath.Join(dir, NewName)) {
			t.Errorf("%s: .new left behind", c.name)
		}
	}
}

func TestDownloadTooBig(t *testing.T) {
	old := maxDownload
	maxDownload = 1000
	t.Cleanup(func() { maxDownload = old })

	// 5 KB of 'x' compresses to well under the limit: the zip passes,
	// the exe inside it doesn't.
	z := makeZip(t, entry{"A/Companion/doink.exe", "MZ" + strings.Repeat("x", 5000)})
	if len(z) >= 1000 {
		t.Fatalf("test zip is %d bytes", len(z))
	}
	in, info, _, dir := release(t, z, sumOf(z))
	if err := in.Download(info, nil); err == nil || !strings.Contains(err.Error(), "far larger") {
		t.Errorf("exe too big: %v", err)
	}
	if exists(filepath.Join(dir, NewName)) {
		t.Error(".new left behind")
	}

	// The download itself over the limit.
	big := bytes.Repeat([]byte{0}, 2000)
	in, info, _, _ = release(t, big, sumOf(big))
	if err := in.Download(info, nil); err == nil || !strings.Contains(err.Error(), "far larger") {
		t.Errorf("zip too big: %v", err)
	}
}

func TestDownloadProgressIsThrottled(t *testing.T) {
	z := goodZip(t) // well under 0.1 MB: one line
	in, info, _, _ := release(t, z, sumOf(z))
	lines := 0
	if err := in.Download(info, func(string) { lines++ }); err != nil {
		t.Fatal(err)
	}
	if lines > 2 { // zip line, then "Verified. Unpacking…"
		t.Errorf("%d progress lines", lines)
	}
}

func TestDownloadStalls(t *testing.T) {
	old := stallTimeout
	stallTimeout = 100 * time.Millisecond
	t.Cleanup(func() { stallTimeout = old })
	quiet(t)
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("PK"))
		w.(http.Flusher).Flush()
		select { // then nothing
		case <-done:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(done); srv.Close() })
	in := &Installer{Dir: t.TempDir(), Prefix: srv.URL + "/", Version: "0.1.0"}
	info := &Info{Current: "0.1.0", Latest: "9.9.9", ZipURL: srv.URL + "/zip", SHA256URL: srv.URL + "/sha"}
	if err := in.Download(info, nil); err == nil || !strings.Contains(err.Error(), "no data for") {
		t.Errorf("err %v", err)
	}
}

func TestDownloadSlowButSteadyFinishes(t *testing.T) {
	old := stallTimeout
	stallTimeout = 300 * time.Millisecond
	t.Cleanup(func() { stallTimeout = old })
	quiet(t)
	z := goodZip(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sha" {
			w.Write([]byte(sumOf(z)))
			return
		}
		// Longer in total than stallTimeout, but never silent for that long.
		for i := 0; i < len(z); i += len(z)/4 + 1 {
			end := min(i+len(z)/4+1, len(z))
			w.Write(z[i:end])
			w.(http.Flusher).Flush()
			time.Sleep(150 * time.Millisecond)
		}
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	in := &Installer{Dir: dir, Prefix: srv.URL + "/", Version: "0.1.0"}
	info := &Info{Current: "0.1.0", Latest: "9.9.9", ZipURL: srv.URL + "/zip", SHA256URL: srv.URL + "/sha"}
	if err := in.Download(info, nil); err != nil {
		t.Fatal(err)
	}
	if readFile(t, filepath.Join(dir, NewName)) != exeBody {
		t.Error("not staged")
	}
}

func TestDownloadRefusesOtherHosts(t *testing.T) {
	z := goodZip(t)
	in, info, hits, _ := release(t, z, sumOf(z))
	info.ZipURL = "https://evil.example/DOINK-v9.9.9.zip"
	if err := in.Download(info, nil); err == nil || !strings.Contains(err.Error(), "anywhere but GitHub") {
		t.Errorf("err %v", err)
	}
	info.ZipURL = in.Prefix + "zip"
	info.SHA256URL = "https://evil.example/x.sha256"
	if err := in.Download(info, nil); err == nil || !strings.Contains(err.Error(), "anywhere but GitHub") {
		t.Errorf("sha url: %v", err)
	}
	if hits.Load() != 0 {
		t.Errorf("%d requests made", hits.Load())
	}
}

func TestDownloadHTTPError(t *testing.T) {
	z := goodZip(t)
	in, info, _, _ := release(t, z, sumOf(z))
	info.ZipURL = in.Prefix + "missing"
	if err := in.Download(info, nil); err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("err %v", err)
	}
}

func TestDownloadNotInstallable(t *testing.T) {
	in := &Installer{Dir: t.TempDir(), Prefix: "x/"}
	for _, info := range []*Info{nil, {Current: "1", Latest: "2"}, {Current: "2", Latest: "2", ZipURL: "x/a", SHA256URL: "x/b"}} {
		if err := in.Download(info, nil); err == nil || !strings.Contains(err.Error(), "no companion download") {
			t.Errorf("%+v: %v", info, err)
		}
	}
}

func put(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSwapAndRollback(t *testing.T) {
	dir := t.TempDir()
	in := &Installer{Dir: dir}
	put(t, dir, ExeName, "old")
	put(t, dir, NewName, "new")
	if err := in.Swap(); err != nil {
		t.Fatal(err)
	}
	if readFile(t, filepath.Join(dir, ExeName)) != "new" || readFile(t, filepath.Join(dir, OldName)) != "old" || exists(filepath.Join(dir, NewName)) {
		t.Error("swap result wrong")
	}
	if err := in.Rollback(); err != nil {
		t.Fatal(err)
	}
	if readFile(t, filepath.Join(dir, ExeName)) != "old" || readFile(t, filepath.Join(dir, NewName)) != "new" || exists(filepath.Join(dir, OldName)) {
		t.Error("rollback result wrong")
	}
}

func TestSwapWithoutNew(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, ExeName, "old")
	if err := (&Installer{Dir: dir}).Swap(); err == nil {
		t.Fatal("no error")
	}
	if readFile(t, filepath.Join(dir, ExeName)) != "old" || exists(filepath.Join(dir, OldName)) {
		t.Error("doink.exe was touched")
	}
}

func TestSwapReplacesStaleOld(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, ExeName, "old")
	put(t, dir, NewName, "new")
	put(t, dir, OldName, "ancient")
	if err := (&Installer{Dir: dir}).Swap(); err != nil {
		t.Fatal(err)
	}
	if readFile(t, filepath.Join(dir, OldName)) != "old" {
		t.Error(".old not replaced")
	}
}

func TestCleanup(t *testing.T) {
	dir := t.TempDir()
	if err := Cleanup(dir); err != nil {
		t.Errorf("empty folder: %v", err)
	}
	put(t, dir, ExeName, "keep")
	put(t, dir, OldName, "x")
	put(t, dir, NewName, "y")
	if err := Cleanup(dir); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(dir, OldName)) || exists(filepath.Join(dir, NewName)) || !exists(filepath.Join(dir, ExeName)) {
		t.Error("cleanup removed the wrong files")
	}
}
