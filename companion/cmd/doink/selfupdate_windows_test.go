//go:build windows

package main

import (
	"reflect"
	"testing"
)

func TestRestartArgs(t *testing.T) {
	cases := []struct{ in, want []string }{
		{nil, []string{"--updated-from", "0.15.0"}},
		{[]string{"--config", "c.toml"}, []string{"--config", "c.toml", "--updated-from", "0.15.0"}},
		{[]string{"--updated-from", "0.14.0", "-v"}, []string{"-v", "--updated-from", "0.15.0"}},
		{[]string{"-updated-from=0.14.0"}, []string{"--updated-from", "0.15.0"}},
	}
	for _, c := range cases {
		if got := restartArgs(c.in, "0.15.0"); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%v: got %v, want %v", c.in, got, c.want)
		}
	}
}
