//go:build ui_debug && !release

package pages

import (
	"context"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"inferyard/studio/internal/core"
)

func TestPreviewUpdateScenesAndTransitionsStayInMemory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	origOpen := openUpdatePathFunc
	t.Cleanup(func() { openUpdatePathFunc = origOpen })
	openUpdatePathFunc = func(path string) error {
		t.Fatalf("preview mode opened OS path %q", path)
		return nil
	}

	p := newPreviewSession(context.Background())
	tt := previewTester(p, 1120, 2200)

	// Scene 1: Available update opens dialog and runs simulated download -> verify -> apply without network or FS IO.
	p.loadScene(previewSceneIndex(t, "更新 · 发现新版本"))
	tt.Frame()
	if !tt.HasText("新版本 v0.10.0") || !tt.HasText("立即更新") {
		t.Fatalf("preview available update scene missing badge or action: %v", tt.Texts())
	}
	click(t, tt, "打开发布页")
	if len(tt.OpenedURLs()) != 0 {
		t.Fatalf("preview mode called OpenURLThen: %v", tt.OpenedURLs())
	}

	click(t, tt, "立即更新")
	p.advance(500 * time.Millisecond)
	tt.Frame()
	if p.state.updateState.State != core.UpdateDownloading {
		t.Fatalf("expected downloading state, got %+v", p.state.updateState)
	}
	p.advance(3 * time.Second)
	tt.Frame()
	if p.state.updateState.State != core.UpdateUpToDate || p.state.updateDialogOpen {
		t.Fatalf("expected simulated one-click update to complete cleanly, got %+v dialog=%v", p.state.updateState, p.state.updateDialogOpen)
	}

	// Scene 2: Downloading scene advances and supports Cancel.
	p.loadScene(previewSceneIndex(t, "更新 · 下载中"))
	tt.Frame()
	if !tt.HasText("取消下载") {
		t.Fatalf("preview downloading scene missing cancel: %v", tt.Texts())
	}
	click(t, tt, "取消下载")
	tt.Frame()
	if p.state.updateState.State != core.UpdateAvailable {
		t.Fatalf("cancel did not return preview to available: %+v", p.state.updateState)
	}

	// Scene 3: Error scene renders fallback actions safely in-memory and supports Retry.
	p.loadScene(previewSceneIndex(t, "更新 · 更新失败"))
	tt.Frame()
	if !tt.HasText("打开安装包") || !tt.HasText("打开发布页") || !tt.HasText("重试") {
		t.Fatalf("preview error scene missing fallback actions: %v", tt.Texts())
	}
	click(t, tt, "打开安装包")
	if len(tt.OpenedURLs()) != 0 {
		t.Fatalf("preview mode called OpenURLThen: %v", tt.OpenedURLs())
	}

	// Verify zero filesystem writes in HOME.
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
		t.Fatalf("preview mode wrote real files: %v", files)
	}
}
