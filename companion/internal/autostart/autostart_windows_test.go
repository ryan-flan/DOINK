//go:build windows

package autostart

import (
	"fmt"
	"math/rand"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	name := fmt.Sprintf("DOINK-test-%d", rand.Int63())
	cmd := Command()
	t.Cleanup(func() { SetEnabled(false, name, cmd) })

	if on, err := IsEnabled(name, cmd); err != nil || on {
		t.Fatalf("before: %v %v", on, err)
	}
	if err := SetEnabled(true, name, cmd); err != nil {
		t.Fatal(err)
	}
	if on, err := IsEnabled(name, cmd); err != nil || !on {
		t.Fatalf("after on: %v %v", on, err)
	}
	if on, _ := IsEnabled(name, `"C:\elsewhere\doink.exe"`); on {
		t.Error("a stale value counts as on")
	}
	if err := SetEnabled(false, name, cmd); err != nil {
		t.Fatal(err)
	}
	if on, _ := IsEnabled(name, cmd); on {
		t.Error("still on")
	}
	if err := SetEnabled(false, name, cmd); err != nil {
		t.Errorf("deleting twice: %v", err)
	}
}
