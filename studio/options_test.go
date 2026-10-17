package main

import (
	"bytes"
	"errors"
	"flag"
	"testing"
)

func TestStartupOptions(t *testing.T) {
	for _, tc := range []struct {
		args           []string
		debug, failure bool
	}{
		{nil, false, false},
		{[]string{"--debug-unknown"}, false, true},
		{[]string{"workspace"}, false, true},
	} {
		var output bytes.Buffer
		options, err := parseOptions(tc.args, &output)
		if options.debugUI != tc.debug || (err != nil) != tc.failure {
			t.Fatalf("args %v: %+v, %v", tc.args, options, err)
		}
	}
	var help bytes.Buffer
	if _, err := parseOptions([]string{"--help"}, &help); !errors.Is(err, flag.ErrHelp) {
		t.Fatal(help.String(), err)
	}
}
