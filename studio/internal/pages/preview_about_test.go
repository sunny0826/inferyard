//go:build ui_debug && !release

package pages

import (
	"context"
	"testing"
)

func TestPreviewSettingsAboutTabStaysInMemory(t *testing.T) {
	home := isolatePreviewHome(t)

	p := newPreviewSession(context.Background())
	p.state.selected = "settings"
	tt := previewTester(p, 1120, 1800)
	tt.Frame()

	click(t, tt, "关于")
	for _, want := range []string{
		"关于 InferYard Studio",
		"v0.0.17",
		"2026-11-26 08:00:00 UTC",
		"0123456789abcdef0123456789abcdef01234567",
	} {
		if !tt.HasText(want) {
			t.Fatalf("preview about tab missing %q: %v", want, tt.Texts())
		}
	}

	click(t, tt, "复制版本信息")
	if got := tt.Clipboard(); got != "" {
		t.Fatalf("preview about copy must not touch real clipboard, got %q", got)
	}

	assertPreviewHomeClean(t, home)
}
