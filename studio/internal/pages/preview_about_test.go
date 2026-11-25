//go:build ui_debug && !release

package pages

import (
	"context"
	"io/fs"
	"path/filepath"
	"testing"
)

func TestPreviewSettingsAboutTabStaysInMemory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	p := newPreviewSession(context.Background())
	p.state.selected = "settings"
	tt := previewTester(p, 1120, 1800)
	tt.Frame()

	click(t, tt, "关于")
	for _, want := range []string{
		"关于 InferYard Studio",
		"v0.0.14",
		"2026-11-25 03:30:00 UTC",
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

	var files []string
	if err := filepath.WalkDir(home, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("preview about tab wrote real files: %v", files)
	}
}
