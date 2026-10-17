//go:build !ui_debug || release

package main

import (
	"bytes"
	"errors"
	"flag"
	"strings"
	"testing"
)

func TestReleaseDoesNotExposeDebugUI(t *testing.T) {
	var help bytes.Buffer
	if _, err := parseOptions([]string{"--help"}, &help); !errors.Is(err, flag.ErrHelp) {
		t.Fatal(err)
	}
	if strings.Contains(help.String(), "debug-ui") {
		t.Fatal("release help exposes debug UI", help.String())
	}
	for _, arg := range []string{"--debug-ui", "--debug-ui=false"} {
		var output bytes.Buffer
		if _, err := parseOptions([]string{arg}, &output); err == nil {
			t.Fatalf("release accepted %s", arg)
		}
	}
}
