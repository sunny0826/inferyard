//go:build release

package main

import (
	"bytes"
	"errors"
	"flag"
	"strings"
	"testing"
)

func TestReleaseDoesNotExposeDevelopmentFlags(t *testing.T) {
	var help bytes.Buffer
	if _, err := parseOptions([]string{"--help"}, &help); !errors.Is(err, flag.ErrHelp) {
		t.Fatal(err)
	}
	for _, token := range []string{"dev-profile", "dev-assets", "dev-cli", "dev-label", "dev-status"} {
		if strings.Contains(help.String(), token) {
			t.Fatalf("release help exposes %s: %s", token, help.String())
		}
	}
	for _, arg := range []string{"--dev-profile=/tmp/p", "--dev-cli=/tmp/cli", "--dev-status=/tmp/s.json"} {
		var out bytes.Buffer
		if _, err := parseOptions([]string{arg}, &out); err == nil {
			t.Fatalf("release accepted %s", arg)
		}
	}
}
