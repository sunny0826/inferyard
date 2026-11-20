//go:build !release

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
)

func TestDevelopmentOptionsValidation(t *testing.T) {
	var out bytes.Buffer
	for _, args := range [][]string{
		{"--dev-profile", "/tmp/p"},
		{"--dev-cli", "/tmp/cli"},
		{"--dev-label", "main"},
		{"--dev-profile=", "--dev-cli="},
		{"--dev-profile=", "--dev-cli=", "--dev-label="},
		{"--dev-profile", "/tmp/p", "--dev-cli", "/tmp/cli", "--dev-status="},
	} {
		out.Reset()
		if _, err := parseOptions(args, &out); err == nil {
			t.Fatalf("expected error for args %v", args)
		}
	}
	opts, err := parseOptions(
		[]string{"--dev-profile", "/tmp/p", "--dev-cli", "/tmp/cli", "--dev-label", "feat", "--dev-status", "/tmp/s.json"},
		&out,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.devProfile != "/tmp/p" || opts.devCLI != "/tmp/cli" || opts.devLabel != "feat" || opts.devStatus != "/tmp/s.json" {
		t.Fatalf("unexpected options: %+v", opts)
	}
}

func TestDevelopmentBindingsAndSymlinkProtection(t *testing.T) {
	root := t.TempDir()
	profile := filepath.Join(root, "profile")
	assets := filepath.Join(root, "assets")
	cli := filepath.Join(root, "inferyard")
	statusFile := filepath.Join(root, "status.json")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	paths, store, reporter, err := developmentBindings(startupOptions{
		devProfile: profile,
		devAssets:  assets,
		devCLI:     cli,
		devLabel:   "wave2",
		devStatus:  statusFile,
	})
	if err != nil {
		t.Fatalf("developmentBindings failed: %v", err)
	}
	if paths == nil || store == nil {
		t.Fatalf("expected non-empty paths and window state store")
	}
	if paths.SourceLabel != "wave2" || paths.WindowTitle != "InferYard Studio · DEV · wave2" {
		t.Fatalf("unexpected labels: %+v", paths)
	}
	reporter.report("ready", "")
	raw, err := os.ReadFile(statusFile)
	if err != nil {
		t.Fatalf("status file missing: %v", err)
	}
	var status map[string]any
	if err := json.Unmarshal(raw, &status); err != nil || status["state"] != "ready" {
		t.Fatalf("unexpected status payload: %s", string(raw))
	}

	// Verify window-state.json refuses writing through a symlink outside profile.
	outside := filepath.Join(root, "outside.txt")
	if err := os.WriteFile(outside, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	wsPath := filepath.Join(profile, "window-state.json")
	if err := os.Symlink(outside, wsPath); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveWindowStates(map[string]mygo.WindowState{}); err == nil {
		t.Fatal("expected SaveWindowStates to reject symlink target")
	}
	got, err := os.ReadFile(outside)
	if err != nil || string(got) != "original" {
		t.Fatalf("outside file was modified: %q", string(got))
	}
}
