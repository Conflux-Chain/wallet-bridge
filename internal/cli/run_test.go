package cli

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"
)

func TestServiceCommandParsing(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		code   int
		called bool
	}{
		{[]string{"service", "run", "--data-dir", t.TempDir()}, 0, true},
		{[]string{"service", "run"}, 0, true},
		{[]string{"service", "run", "--help"}, 0, false},
		{[]string{"service", "run", "--data-dir", "relative"}, 2, false},
		{[]string{"service", "run", "--port", "1"}, 2, false},
		{[]string{"service", "run", "extra"}, 2, false},
		{[]string{"service", "stop"}, 2, false},
	} {
		var stdout, stderr bytes.Buffer
		called := false
		code := Run(tc.args, Dependencies{RunService: func(dir string) error {
			called = true
			if dir != "" && !filepath.IsAbs(dir) {
				t.Fatal("runner received relative data path")
			}
			return nil
		}}, &stdout, &stderr)
		if code != tc.code || called != tc.called {
			t.Fatalf("%v: exit=%d called=%t", tc.args, code, called)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"service", "run"}, Dependencies{RunService: func(string) error { return errors.New("port_in_use: occupied") }}, &stdout, &stderr); code != 1 || stdout.Len() != 0 {
		t.Fatal("service failure not reported on stderr")
	}
}
