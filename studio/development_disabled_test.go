//go:build !studio_dev || release

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestDevelopmentFlagsRejected(t *testing.T) {
	for _, name := range []string{"--dev-manifest", "--dev-check", "--dev-stop", "--dev-recovery-controller", "--dev-recovery-target"} {
		if _, err := parseOptions([]string{name, "/tmp/manifest.json"}, &bytes.Buffer{}); err == nil {
			t.Fatal(name, "accepted outside development build")
		}
	}
	var help bytes.Buffer
	parseOptions([]string{"--help"}, &help)
	if strings.Contains(help.String(), "dev-manifest") {
		t.Fatal(help.String())
	}
}
