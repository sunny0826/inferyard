//go:build ui_debug && !release

package pages

import (
	"context"
	"testing"
	"time"

	"inferyard/studio/internal/core"
)

func TestPreviewUpdateScenesAndTransitionsStayInMemory(t *testing.T) {
	home := isolatePreviewHome(t)

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
	if !tt.HasText("新版本 v0.10.0") || !tt.HasText("立即更新") || !tt.HasText("前往 Release") {
		t.Fatalf("preview available update scene missing badge or action: %v", tt.Texts())
	}
	if tt.HasText("Highlights") || tt.HasText("automatic update discovery") {
		t.Fatalf("preview available update scene should not render raw notes: %v", tt.Texts())
	}
	click(t, tt, "前往 Release")
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

	// Scene 3: Ready-to-apply scene shows floating prompt and transitions cleanly in memory.
	p.loadScene(previewSceneIndex(t, "更新 · 待重启生效"))
	tt.Frame()
	if !tt.HasText("版本 0.10.0 已可更新，重启后生效。") || !tt.HasText("查看更新日志") || !tt.HasText("更新并重启") {
		t.Fatalf("preview ready-to-apply scene missing floating prompt: %v", tt.Texts())
	}
	click(t, tt, "查看更新日志")
	if len(tt.OpenedURLs()) != 0 {
		t.Fatalf("preview mode called OpenURLThen from ready prompt: %v", tt.OpenedURLs())
	}
	click(t, tt, "更新并重启")
	p.advance(600 * time.Millisecond)
	tt.Frame()
	if p.state.updateState.State != core.UpdateUpToDate {
		t.Fatalf("preview ready prompt apply did not finish cleanly: %+v", p.state.updateState)
	}

	// Scene 4: Error scene renders fallback actions safely in-memory and supports Retry.
	p.loadScene(previewSceneIndex(t, "更新 · 更新失败"))
	tt.Frame()
	if !tt.HasText("打开安装包") || !tt.HasText("前往 Release") || !tt.HasText("重试") {
		t.Fatalf("preview error scene missing fallback actions: %v", tt.Texts())
	}
	if tt.HasText("Highlights") || tt.HasText("automatic update discovery") {
		t.Fatalf("preview error scene should not render raw notes: %v", tt.Texts())
	}
	click(t, tt, "打开安装包")
	if len(tt.OpenedURLs()) != 0 {
		t.Fatalf("preview mode called OpenURLThen: %v", tt.OpenedURLs())
	}

	// Verify zero filesystem writes in HOME.
	assertPreviewHomeClean(t, home)
}
