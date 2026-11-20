//go:build ui_debug && !release

package main

import (
	"bytes"
	"testing"
)

func TestDebugUIRejectsEmptyDevelopmentFlags(t *testing.T) {
	var out bytes.Buffer
	for _, args := range [][]string{
		{"--debug-ui", "--dev-profile=", "--dev-cli="},
		{"--debug-ui", "--dev-label="},
		{"--debug-ui", "--dev-status="},
	} {
		out.Reset()
		if _, err := parseOptions(args, &out); err == nil {
			t.Fatalf("expected error for mutually exclusive flags %v", args)
		}
	}
}
