//go:build windows

package settings

import (
	_ "embed"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/webviewloader"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

//go:embed ui.html
var page string

// Log receives window messages; nil means slog.Default().
var Log *slog.Logger

func logger() *slog.Logger {
	if Log != nil {
		return Log
	}
	return slog.Default()
}

var (
	user32  = windows.NewLazySystemDLL("user32.dll")
	ole32   = windows.NewLazySystemDLL("ole32.dll")
	shell32 = windows.NewLazySystemDLL("shell32.dll")
	dwmapi  = windows.NewLazySystemDLL("dwmapi.dll")

	procSetThreadDpiAwarenessContext = user32.NewProc("SetThreadDpiAwarenessContext")
	procGetDpiForSystem              = user32.NewProc("GetDpiForSystem")
	procSendMessageW                 = user32.NewProc("SendMessageW")
	procShowWindow                   = user32.NewProc("ShowWindow")
	procSetForegroundWindow          = user32.NewProc("SetForegroundWindow")
	procCoInitializeEx               = ole32.NewProc("CoInitializeEx")
	procCoUninitialize               = ole32.NewProc("CoUninitialize")
	procCoTaskMemFree                = ole32.NewProc("CoTaskMemFree")
	procSHBrowseForFolderW           = shell32.NewProc("SHBrowseForFolderW")
	procSHGetPathFromIDListW         = shell32.NewProc("SHGetPathFromIDListW")
	procDwmSetWindowAttribute        = dwmapi.NewProc("DwmSetWindowAttribute")
)

const (
	coinitApartmentThreaded   = 0x2
	dpiPerMonitorAwareV2      = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = (HANDLE)-4
	wmSetIcon                 = 0x0080
	iconSmall, iconBig        = 0, 1
	swRestore                 = 9
	bifReturnOnlyFSDirs       = 0x0001
	bifNewDialogStyle         = 0x0040
	dwmwaUseImmersiveDarkMode = 20

	// Logical size (96 dpi); scaled to the screen.
	width, height       = 560, 800
	minWidth, minHeight = 460, 560
)

// Window is the settings window: at most one open at a time, on its own
// OS thread (a webview window quits its thread's message loop when it
// closes, which must not be the tray's).
type Window struct {
	b *Backend
	// Fallback runs when the window can't open (no WebView2 runtime):
	// open config.toml instead, as before the window existed.
	Fallback func()
	// Icon returns the window icon at a pixel size (0: none).
	Icon func(size int) uintptr

	mu      sync.Mutex
	wv      webview2.WebView
	opening bool
}

// NewWindow returns a closed window over b.
func NewWindow(b *Backend) *Window { return &Window{b: b} }

// Available reports whether the WebView2 runtime is installed (it ships
// with Windows 11 and current Windows 10).
func Available() bool {
	v, err := webviewloader.GetInstalledVersion()
	return err == nil && v != ""
}

// Show opens the window, or brings it to the front if it is open. Safe
// from any goroutine; it doesn't wait for the window.
func (w *Window) Show() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.wv != nil {
		wv := w.wv
		wv.Dispatch(func() {
			hwnd := uintptr(wv.Window())
			procShowWindow.Call(hwnd, swRestore)
			procSetForegroundWindow.Call(hwnd)
		})
		return
	}
	if w.opening {
		return
	}
	if !Available() {
		logger().Warn("settings window: the WebView2 runtime isn't installed; opening config.toml instead")
		if w.Fallback != nil {
			go w.Fallback()
		}
		return
	}
	w.opening = true
	go w.run()
}

// Refresh pushes the live status to the open window, if any.
func (w *Window) Refresh() {
	w.mu.Lock()
	wv := w.wv
	w.mu.Unlock()
	if wv == nil {
		return
	}
	live, _ := json.Marshal(w.b.Live())
	wv.Dispatch(func() { wv.Eval("window.doinkLive && window.doinkLive(" + string(live) + ")") })
}

// Close closes the window if it is open.
func (w *Window) Close() {
	w.mu.Lock()
	wv := w.wv
	w.mu.Unlock()
	if wv != nil {
		wv.Destroy()
	}
}

func (w *Window) run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer func() {
		w.mu.Lock()
		w.wv, w.opening = nil, false
		w.mu.Unlock()
	}()

	// WebView2 needs a single-threaded COM apartment on its thread.
	if r, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded); int32(r) >= 0 {
		defer procCoUninitialize.Call()
	}
	// Sharp text on scaled displays, for this thread's windows only.
	if procSetThreadDpiAwarenessContext.Find() == nil {
		procSetThreadDpiAwarenessContext.Call(dpiPerMonitorAwareV2)
	}
	scale := func(n int) int { return n }
	if procGetDpiForSystem.Find() == nil {
		if dpi, _, _ := procGetDpiForSystem.Call(); dpi > 0 {
			scale = func(n int) int { return n * int(dpi) / 96 }
		}
	}

	wv := webview2.NewWithOptions(webview2.WebViewOptions{
		DataPath:  dataPath(),
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  "DOINK settings",
			Width:  uint(scale(width)),
			Height: uint(scale(height)),
			Center: true,
		},
	})
	if wv == nil {
		logger().Warn("settings window: WebView2 didn't start; opening config.toml instead")
		if w.Fallback != nil {
			go w.Fallback()
		}
		return
	}
	hwnd := uintptr(wv.Window())
	wv.SetSize(scale(minWidth), scale(minHeight), webview2.HintMin)
	if w.Icon != nil {
		if icon := w.Icon(scale(16)); icon != 0 {
			procSendMessageW.Call(hwnd, wmSetIcon, iconSmall, icon)
		}
		if icon := w.Icon(scale(32)); icon != 0 {
			procSendMessageW.Call(hwnd, wmSetIcon, iconBig, icon)
		}
	}
	if darkMode() && procDwmSetWindowAttribute.Find() == nil {
		on := int32(1)
		procDwmSetWindowAttribute.Call(hwnd, dwmwaUseImmersiveDarkMode, uintptr(unsafe.Pointer(&on)), 4)
	}

	w.bind(wv, hwnd)
	wv.SetHtml(page)

	w.mu.Lock()
	w.wv, w.opening = wv, false
	w.mu.Unlock()
	logger().Info("settings window opened")
	wv.Run()
	logger().Info("settings window closed")
}

func (w *Window) bind(wv webview2.WebView, hwnd uintptr) {
	b := w.b
	must := func(err error) {
		if err != nil {
			logger().Warn("settings window: " + err.Error())
		}
	}
	must(wv.Bind("doinkLoad", func() View { return b.View() }))
	must(wv.Bind("doinkSave", func(webhook, wowDir string) SaveResult {
		r := b.Save(webhook, wowDir)
		if r.Error == "" {
			logger().Info("settings saved from the settings window")
		}
		return r
	}))
	// Posting can take seconds (rate limits, retries): answer later, so
	// the window never freezes.
	must(wv.Bind("doinkTest", func(webhook string) {
		go func() {
			msg := b.Test(webhook)
			reply, _ := json.Marshal(msg)
			wv.Dispatch(func() { wv.Eval("window.doinkTested(" + string(reply) + ")") })
		}()
	}))
	must(wv.Bind("doinkBrowse", func() string { return browseFolder(hwnd) }))
	must(wv.Bind("doinkAutostart", func(on bool) string { return b.SetAutostart(on) }))
	must(wv.Bind("doinkOpen", func(what string) {
		if b.Open != nil {
			go b.Open(what)
		}
	}))
	must(wv.Bind("doinkClose", func() { wv.Destroy() }))
}

// dataPath is where WebView2 keeps its browser profile:
// %LOCALAPPDATA%\DOINK\WebView2 (the library default would be
// %APPDATA%\doink.exe).
func dataPath() string {
	base, err := os.UserCacheDir() // %LOCALAPPDATA% on Windows
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "DOINK", "WebView2")
}

// darkMode reports Windows' app theme (Settings → Personalization → Colors).
func darkMode() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("AppsUseLightTheme")
	return err == nil && v == 0
}

// browseInfo is BROWSEINFOW.
type browseInfo struct {
	owner       uintptr
	root        uintptr
	displayName *uint16
	title       *uint16
	flags       uint32
	callback    uintptr
	lParam      uintptr
	image       int32
}

// browseFolder shows the folder picker; "" if cancelled.
func browseFolder(owner uintptr) string {
	name := make([]uint16, windows.MAX_PATH)
	title, _ := windows.UTF16PtrFromString("Choose your World of Warcraft folder (the one that contains _forever_)")
	bi := browseInfo{
		owner:       owner,
		displayName: &name[0],
		title:       title,
		flags:       bifReturnOnlyFSDirs | bifNewDialogStyle,
	}
	pidl, _, _ := procSHBrowseForFolderW.Call(uintptr(unsafe.Pointer(&bi)))
	if pidl == 0 {
		return ""
	}
	defer procCoTaskMemFree.Call(pidl)
	path := make([]uint16, windows.MAX_PATH)
	if ok, _, _ := procSHGetPathFromIDListW.Call(pidl, uintptr(unsafe.Pointer(&path[0]))); ok == 0 {
		return ""
	}
	return windows.UTF16ToString(path)
}

// For the Windows test: BROWSEINFOW's size as Windows expects it.
var browseInfoSize = unsafe.Sizeof(browseInfo{})
