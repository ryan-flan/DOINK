//go:build windows

package tray

import (
	"fmt"
	"math/rand"
	"testing"
	"time"
	"unsafe"

	"github.com/ryan-flan/DOINK/companion/internal/status"
)

func TestStructSizes(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) == 8 {
		if s := unsafe.Sizeof(notifyIconDataW{}); s != 976 {
			t.Errorf("NOTIFYICONDATAW is %d bytes, want 976", s)
		}
		if s := unsafe.Sizeof(msg{}); s != 48 {
			t.Errorf("MSG is %d bytes, want 48", s)
		}
		if s := unsafe.Sizeof(wndClassW{}); s != 72 {
			t.Errorf("WNDCLASSW is %d bytes, want 72", s)
		}
	}
}

func TestSingleInstance(t *testing.T) {
	name := fmt.Sprintf(`Local\DOINK-test-%d`, rand.Int63())
	if !AcquireSingleInstance(name) {
		t.Fatal("first acquire failed")
	}
	if AcquireSingleInstance(name) {
		t.Error("second acquire succeeded while the first is held")
	}
}

func TestRunNotifyQuit(t *testing.T) {
	st := status.New(nil)
	tr := New(st, "test", Hooks{})
	done := make(chan error, 1)
	go func() { done <- tr.Run() }()
	deadline := time.Now().Add(5 * time.Second)
	for tr.hwnd.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if tr.hwnd.Load() == 0 {
		select {
		case err := <-done:
			t.Fatalf("Run returned early: %v", err)
		default:
			t.Fatal("no window after 5 s")
		}
	}
	tr.Notify("x")
	tr.Inform("y")
	tr.Quit()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run didn't return within 5 s of Quit")
	}
}

func TestCopyUTF16(t *testing.T) {
	var buf [4]uint16
	copyUTF16(buf[:], "abcdef")
	if buf != [4]uint16{'a', 'b', 'c', 0} {
		t.Errorf("got %v", buf)
	}
}
