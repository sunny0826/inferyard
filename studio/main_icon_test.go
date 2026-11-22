package main

import (
	"bytes"
	"image/png"
	"testing"
)

func TestEmbeddedAppIconIs1024PNG(t *testing.T) {
	cfg, err := png.DecodeConfig(bytes.NewReader(appIconPNG))
	if err != nil {
		t.Fatalf("decode embedded icon.png: %v", err)
	}
	if cfg.Width != 1024 || cfg.Height != 1024 {
		t.Fatalf("embedded icon size = %dx%d, want 1024x1024", cfg.Width, cfg.Height)
	}
}
