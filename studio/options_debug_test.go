//go:build ui_debug && !release

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestDebugUIOption(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		arg := "--debug-ui=false"
		if enabled {
			arg = "--debug-ui"
		}
		var output bytes.Buffer
		options, err := parseOptions([]string{arg}, &output)
		if err != nil || options.debugUI != enabled {
			t.Fatalf("%s: %+v, %v", arg, options, err)
		}
	}
	var help bytes.Buffer
	_, _ = parseOptions([]string{"--help"}, &help)
	if !strings.Contains(help.String(), "debug-ui") {
		t.Fatal("preview switch absent from development help", help.String())
	}
}
