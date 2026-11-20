//go:build !release

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDevelopmentOptionsValidation(t *testing.T) {
	var out bytes.Buffer
	if _, err := parseOptions([]string{"--dev-profile", "/tmp/p"}, &out); err == nil {
		t.Fatal("expected error when --dev-cli is missing")
	}
	if _, err := parseOptions([]string{"--dev-cli", "/tmp/cli"}, &out); err == nil {
		t.Fatal("expected error when --dev-profile is missing")
	}
	if _, err := parseOptions([]string{"--dev-label", "main"}, &out); err == nil {
		t.Fatal("expected error when --dev-label is passed alone")
	}
	opts, err := parseOptions([]string{"--dev-profile", "/tmp/p", "--dev-cli", "/tmp/cli", "--dev-label", "feat"}, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.devProfile != "/tmp/p" || opts.devCLI != "/tmp/cli" || opts.devLabel != "feat" {
		t.Fatalf("unexpected options: %+v", opts)
	}
}

func TestDevelopmentBindingsResolveCanonicalPaths(t *testing.T) {
	root := t.TempDir()
	profile := filepath.Join(root, "profile")
	assets := filepath.Join(root, "assets")
	cli := filepath.Join(root, "inferyard")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	paths, store, err := developmentBindings(startupOptions{
		devProfile: profile,
		devAssets:  assets,
		devCLI:     cli,
		devLabel:   "wave2",
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
	if _, err := os.Stat(paths.Models); err != nil {
		t.Fatalf("models dir missing: %v", err)
	}
	if _, err := os.Stat(paths.Engines); err != nil {
		t.Fatalf("engines dir missing: %v", err)
	}
}
