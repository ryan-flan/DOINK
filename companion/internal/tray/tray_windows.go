//go:build windows

package tray

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/ryan-flan/DOINK/companion/assets"
	"github.com/ryan-flan/DOINK/companion/internal/autostart"
	"github.com/ryan-flan/DOINK/companion/internal/status"
	"github.com/ryan-flan/DOINK/companion/internal/updates"
)

// Log receives tray messages; nil means slog.Default().
var Log *slog.Logger

func logger() *slog.Logger {
	if Log != nil {
		return Log
	}
	return slog.Default()
}

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procGetModuleHandleW       = kernel32.NewProc("GetModuleHandleW")
	procRegisterClassW         = user32.NewProc("RegisterClassW")
	procCreateWindowExW        = user32.NewProc("CreateWindowExW")
	procDefWindowProcW         = user32.NewProc("DefWindowProcW")
	procDestroyWindow          = user32.NewProc("DestroyWindow")
	procPostMessageW           = user32.NewProc("PostMessageW")
	procPostQuitMessage        = user32.NewProc("PostQuitMessage")
	procGetMessageW            = user32.NewProc("GetMessageW")
	procTranslateMessage       = user32.NewProc("TranslateMessage")
	procDispatchMessageW       = user32.NewProc("DispatchMessageW")
	procRegisterWindowMessageW = user32.NewProc("RegisterWindowMessageW")
	procCreatePopupMenu        = user32.NewProc("CreatePopupMenu")
	procAppendMenuW            = user32.NewProc("AppendMenuW")
	procTrackPopupMenu         = user32.NewProc("TrackPopupMenu")
	procDestroyMenu            = user32.NewProc("DestroyMenu")
	procSetMenuDefaultItem     = user32.NewProc("SetMenuDefaultItem")
	procGetCursorPos           = user32.NewProc("GetCursorPos")
	procSetForegroundWindow    = user32.NewProc("SetForegroundWindow")
	procGetSystemMetrics       = user32.NewProc("GetSystemMetrics")
	procLoadImageW             = user32.NewProc("LoadImageW")
	procLoadIconW              = user32.NewProc("LoadIconW")
	procMessageBoxW            = user32.NewProc("MessageBoxW")
	procShellNotifyIconW       = shell32.NewProc("Shell_NotifyIconW")
	procShellExecuteW          = shell32.NewProc("ShellExecuteW")
	procCreateMutexW           = kernel32.NewProc("CreateMutexW")
)

const (
	wmNull       = 0x0000
	wmDestroy    = 0x0002
	wmClose      = 0x0010
	wmLButtonUp  = 0x0202
	wmRButtonUp  = 0x0205
	wmApp        = 0x8000
	wmTray       = wmApp + 1 // mouse events on the icon
	wmStatus     = wmApp + 2 // Status changed (posted from another goroutine)
	nimAdd       = 0
	nimModify    = 1
	nimDelete    = 2
	nifMessage   = 0x01
	nifIcon      = 0x02
	nifTip       = 0x04
	nifInfo      = 0x10
	niifInfo     = 0x01
	niifWarning  = 0x02
	mfString     = 0x0000
	mfGrayed     = 0x0001
	mfChecked    = 0x0008
	mfSeparator  = 0x0800
	tpmRightBtn  = 0x0002
	tpmNoNotify  = 0x0080
	tpmReturnCmd = 0x0100
	imageIcon    = 1
	lrLoadFile   = 0x0010
	smCxSmIcon   = 49
	idiApp       = 32512
	mbIconInfo   = 0x40
	mbIconError  = 0x10
	swShowNormal = 1

	errorAlreadyExists      = 183
	errorClassAlreadyExists = 1410
)

// Menu ids.
const (
	idSettings  = 1
	idLog       = 2
	idFolder    = 3
	idAutostart = 4
	idUpdates   = 5
	idQuit      = 9
)

type wndClassW struct {
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
}

type notifyIconDataW struct {
	cbSize           uint32
	hWnd             uintptr
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            uintptr
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         windows.GUID
	hBalloonIcon     uintptr
}

type point struct{ x, y int32 }

type msg struct {
	hwnd     uintptr
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	pt       point
	lPrivate uint32
}

func utf16Ptr(s string) *uint16 {
	p, err := windows.UTF16PtrFromString(s)
	if err != nil { // a NUL inside: drop it rather than fail
		p, _ = windows.UTF16PtrFromString(strings.ReplaceAll(s, "\x00", ""))
	}
	return p
}

// copyUTF16 fills dst with s, cut to fit with a terminating NUL.
func copyUTF16(dst []uint16, s string) {
	u := utf16.Encode([]rune(s))
	if len(u) > len(dst)-1 {
		u = u[:len(dst)-1]
	}
	n := copy(dst, u)
	dst[n] = 0
}

// Hooks are the menu's actions that live outside the tray.
type Hooks struct {
	OnQuit         func()
	OnCheckUpdates func() (*updates.Info, error)
	OpenConfig     func()
	OpenLog        func()
	OpenFolder     func()
}

// Tray is the icon. Run it on one goroutine; the other methods are safe
// from any goroutine.
type Tray struct {
	st      *status.Status
	version string
	hooks   Hooks

	hwnd     atomic.Uintptr
	quitting atomic.Bool

	mu           sync.Mutex
	balloon      string
	balloonFlags uint32

	icon, updateIcon uintptr
	taskbarCreated   uintptr
}

var (
	procOnce sync.Once
	wndProc  uintptr
	windowsM sync.Mutex
	trays    = map[uintptr]*Tray{}
)

// New returns a Tray; nothing is shown until Run.
func New(st *status.Status, version string, hooks Hooks) *Tray {
	nop := func() {}
	if hooks.OnQuit == nil {
		hooks.OnQuit = nop
	}
	if hooks.OpenConfig == nil {
		hooks.OpenConfig = nop
	}
	if hooks.OpenLog == nil {
		hooks.OpenLog = nop
	}
	if hooks.OpenFolder == nil {
		hooks.OpenFolder = nop
	}
	return &Tray{st: st, version: version, hooks: hooks}
}

// Notify is Status's onChange: refresh the tooltip, and pop a warning
// for a new problem.
func (t *Tray) Notify(newError string) {
	if newError != "" {
		t.mu.Lock()
		t.balloon, t.balloonFlags = newError, niifWarning
		t.mu.Unlock()
	}
	t.post(wmStatus)
}

// Inform is Status's OnNotice: a plain notification.
func (t *Tray) Inform(text string) {
	t.mu.Lock()
	t.balloon, t.balloonFlags = text, niifInfo
	t.mu.Unlock()
	t.post(wmStatus)
}

// Quit removes the icon and makes Run return.
func (t *Tray) Quit() {
	t.quitting.Store(true)
	t.post(wmClose)
}

func (t *Tray) post(message uintptr) {
	if hwnd := t.hwnd.Load(); hwnd != 0 {
		procPostMessageW.Call(hwnd, message, 0, 0)
	}
}

// Run registers the window class "DOINKTray", creates a hidden top-level
// window (only those hear "TaskbarCreated" when Explorer restarts), adds
// the icon and pumps messages until Quit. It locks the calling goroutine
// to its OS thread: Win32 windows belong to the thread that made them.
func (t *Tray) Run() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	procOnce.Do(func() { wndProc = syscall.NewCallback(dispatch) })
	hinst, _, _ := procGetModuleHandleW.Call(0)
	class := utf16Ptr("DOINKTray")
	wc := wndClassW{lpfnWndProc: wndProc, hInstance: hinst, lpszClassName: class}
	if atom, _, err := procRegisterClassW.Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
		if errno, ok := err.(syscall.Errno); !ok || errno != errorClassAlreadyExists {
			return fmt.Errorf("RegisterClassW: %v", err)
		}
	}
	t.taskbarCreated, _, _ = procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(utf16Ptr("TaskbarCreated"))))
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(class)),
		uintptr(unsafe.Pointer(utf16Ptr("DOINK"))), 0, 0, 0, 0, 0, 0, 0, hinst, 0)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowExW: %v", err)
	}
	windowsM.Lock()
	trays[hwnd] = t
	windowsM.Unlock()
	defer func() {
		windowsM.Lock()
		delete(trays, hwnd)
		windowsM.Unlock()
	}()
	t.icon = loadIcon("doink.ico", assets.Icon)
	t.updateIcon = loadIcon("doink-update.ico", assets.UpdateIcon)
	t.hwnd.Store(hwnd)
	t.show(nimAdd)
	if t.quitting.Load() { // Quit came before the window existed
		t.post(wmClose)
	}

	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 { // 0: WM_QUIT, -1: error
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	t.hwnd.Store(0)
	return nil
}

// Icon is DOINK's icon at a pixel size, for other windows (0 on failure).
func Icon(size int) uintptr { return loadIconSize("doink.ico", assets.Icon, uintptr(size)) }

// loadIcon writes the embedded icon to %TEMP%\doink-<name> (if absent or
// different) and loads it at the small-icon size; the stock application
// icon if that fails.
func loadIcon(name string, data []byte) uintptr {
	size, _, _ := procGetSystemMetrics.Call(smCxSmIcon)
	icon := loadIconSize(name, data, size)
	if icon == 0 {
		icon, _, _ = procLoadIconW.Call(0, idiApp)
	}
	return icon
}

func loadIconSize(name string, data []byte, size uintptr) uintptr {
	path := filepath.Join(os.TempDir(), "doink-"+name)
	if old, err := os.ReadFile(path); err != nil || !bytes.Equal(old, data) {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			logger().Warn(fmt.Sprintf("couldn't write %s: %v", path, err))
		}
	}
	icon, _, _ := procLoadImageW.Call(0, uintptr(unsafe.Pointer(utf16Ptr(path))), imageIcon, size, size, lrLoadFile)
	return icon
}

func (t *Tray) show(action uintptr) {
	nid := notifyIconDataW{
		hWnd:             t.hwnd.Load(),
		uID:              1,
		uFlags:           nifMessage | nifIcon | nifTip,
		uCallbackMessage: wmTray,
		hIcon:            t.icon,
	}
	nid.cbSize = uint32(unsafe.Sizeof(nid))
	if u := t.st.Update(); u != nil && u.Newer() {
		nid.hIcon = t.updateIcon
	}
	copyUTF16(nid.szTip[:], t.st.Summary())
	t.mu.Lock()
	if t.balloon != "" && action != nimDelete {
		nid.uFlags |= nifInfo
		copyUTF16(nid.szInfoTitle[:], "DOINK")
		copyUTF16(nid.szInfo[:], t.balloon)
		nid.dwInfoFlags = t.balloonFlags
		t.balloon = ""
	}
	t.mu.Unlock()
	if ok, _, _ := procShellNotifyIconW.Call(action, uintptr(unsafe.Pointer(&nid))); ok == 0 && action == nimAdd {
		logger().Warn("couldn't add the tray icon (no taskbar yet?)")
	}
}

func dispatch(hwnd, message, wParam, lParam uintptr) uintptr {
	windowsM.Lock()
	t := trays[hwnd]
	windowsM.Unlock()
	if t != nil {
		if r, handled := t.proc(hwnd, message, lParam); handled {
			return r
		}
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, message, wParam, lParam)
	return r
}

func (t *Tray) proc(hwnd, message, lParam uintptr) (result uintptr, handled bool) {
	defer func() {
		if p := recover(); p != nil {
			logger().Error(fmt.Sprintf("tray message %#x failed: %v", message, p))
			result, handled = 0, true
		}
	}()
	switch {
	case message == wmTray:
		switch lParam & 0xFFFF {
		case wmLButtonUp:
			t.hooks.OpenLog()
		case wmRButtonUp:
			t.menu()
		}
	case message == wmStatus:
		t.show(nimModify)
	case t.taskbarCreated != 0 && message == t.taskbarCreated: // Explorer restarted
		t.show(nimAdd)
	case message == wmClose:
		t.show(nimDelete)
		procDestroyWindow.Call(hwnd)
	case message == wmDestroy:
		procPostQuitMessage.Call(0)
	default:
		return 0, false
	}
	return 0, true
}

func appendMenu(menu uintptr, flags, id uintptr, text string) {
	var p uintptr
	if text != "" {
		p = uintptr(unsafe.Pointer(utf16Ptr(text)))
	}
	procAppendMenuW.Call(menu, flags, id, p)
}

func (t *Tray) menu() {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	update := t.st.Update()
	newer := update != nil && update.Newer()
	title := "DOINK v" + t.version
	if newer {
		title += "  (v" + update.Latest + " available)"
	}
	appendMenu(menu, mfGrayed, 0, title)
	appendMenu(menu, mfGrayed, 0, strings.TrimPrefix(t.st.Summary(), "DOINK: "))
	appendMenu(menu, mfSeparator, 0, "")
	appendMenu(menu, mfString, idSettings, "Settings")
	procSetMenuDefaultItem.Call(menu, idSettings, 0) // bold
	appendMenu(menu, mfString, idLog, "Open log")
	appendMenu(menu, mfString, idFolder, "Open DOINK folder")
	if autostart.Available() {
		var checked uintptr
		if on, _ := autostart.IsEnabled(autostart.Name, autostart.Command()); on {
			checked = mfChecked
		}
		appendMenu(menu, mfString|checked, idAutostart, "Start with Windows")
	}
	if newer {
		appendMenu(menu, mfString, idUpdates, "v"+update.Latest+" available: open download page")
	} else {
		appendMenu(menu, mfString, idUpdates, "Check for updates")
	}
	appendMenu(menu, mfSeparator, 0, "")
	appendMenu(menu, mfString, idQuit, "Quit")

	hwnd := t.hwnd.Load()
	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWindow.Call(hwnd) // else the menu won't close on click-away
	cmd, _, _ := procTrackPopupMenu.Call(menu, tpmRightBtn|tpmNoNotify|tpmReturnCmd,
		uintptr(pt.x), uintptr(pt.y), 0, hwnd, 0)
	procPostMessageW.Call(hwnd, wmNull, 0, 0)
	procDestroyMenu.Call(menu)

	switch cmd {
	case idSettings:
		t.hooks.OpenConfig()
	case idLog:
		t.hooks.OpenLog()
	case idFolder:
		t.hooks.OpenFolder()
	case idAutostart:
		on, _ := autostart.IsEnabled(autostart.Name, autostart.Command())
		if err := autostart.SetEnabled(!on, autostart.Name, autostart.Command()); err != nil {
			logger().Warn("start with Windows: " + err.Error())
		} else if on {
			logger().Info("start with Windows: off")
		} else {
			logger().Info("start with Windows: on")
		}
	case idUpdates:
		if newer {
			if err := OpenPath(update.URL); err != nil {
				logger().Warn(fmt.Sprintf("couldn't open %s: %v", update.URL, err))
			}
		} else {
			go t.checkUpdates()
		}
	case idQuit:
		logger().Info("quit from tray")
		t.hooks.OnQuit()
	}
}

func (t *Tray) checkUpdates() {
	if t.hooks.OnCheckUpdates == nil {
		return
	}
	info, err := t.hooks.OnCheckUpdates()
	switch {
	case err != nil:
		t.Inform("Couldn't reach GitHub. Try again later.")
	case info.Newer():
		t.Inform("v" + info.Latest + " is available. Opening the download page…")
		if err := OpenPath(info.URL); err != nil {
			logger().Warn(fmt.Sprintf("couldn't open %s: %v", info.URL, err))
		}
	default:
		t.Inform("You have the latest version (v" + t.version + ").")
	}
}

// AcquireSingleInstance creates the named mutex; false if another process
// holds it. The handle is kept for the life of the process.
func AcquireSingleInstance(name string) bool {
	h, _, err := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(utf16Ptr(name))))
	if h == 0 {
		return true // can't tell; don't refuse to start
	}
	if errno, ok := err.(syscall.Errno); ok && errno == errorAlreadyExists {
		windows.CloseHandle(windows.Handle(h))
		return false
	}
	heldMutexes = append(heldMutexes, h)
	return true
}

var heldMutexes []uintptr

// MessageBox shows text in a "DOINK" box.
func MessageBox(text string, isError bool) {
	var flags uintptr = mbIconInfo
	if isError {
		flags = mbIconError
	}
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(utf16Ptr(text))), uintptr(unsafe.Pointer(utf16Ptr("DOINK"))), flags)
}

// OpenPath opens a file, folder or URL with its default program.
func OpenPath(path string) error {
	r, _, _ := procShellExecuteW.Call(0, uintptr(unsafe.Pointer(utf16Ptr("open"))),
		uintptr(unsafe.Pointer(utf16Ptr(path))), 0, 0, swShowNormal)
	if r <= 32 { // ShellExecute's documented failure range
		return fmt.Errorf("ShellExecute failed (%d)", r)
	}
	return nil
}
