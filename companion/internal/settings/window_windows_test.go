//go:build windows

package settings

import (
	"runtime"
	"testing"
)

func TestBrowseInfoLayout(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("sizes asserted for amd64")
	}
	if browseInfoSize != 64 {
		t.Errorf("BROWSEINFOW is %d bytes, Windows expects 64", browseInfoSize)
	}
}

func TestDataPathIsLocal(t *testing.T) {
	if p := dataPath(); p == "" {
		t.Error("empty data path")
	}
}
