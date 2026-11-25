package pages

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func TestEngineUpdatesUIButtonOneClickAndNotifications(t *testing.T) {
	restore := l10n.SetTestLocale(l10n.LanguageZhHans)
	defer restore()

	s, _ := servicesP2State(t)
	s.selected = "library"
	s.libraryTab = 1

	uiQueue := make(chan func(), 8)
	s.updateWindow = func(fn func()) { uiQueue <- fn }
	drainUI := func() {
		for {
			select {
			case fn := <-uiQueue:
				fn()
			default:
				return
			}
		}
	}

	checkDone := make(chan struct{}, 1)
	s.engineUpdateCheckFn = func(context.Context) (core.EngineUpdatesStatus, *core.ApiError) {
		checkDone <- struct{}{}
		return core.EngineUpdatesStatus{
			State:          core.EngineUpdateCheckReady,
			UpdatableCount: 1,
			Updates: map[string]core.EngineUpdateInfo{
				"llama-cpp-default": {
					Engine:           "llama-cpp-default",
					Family:           "llama-cpp",
					InstalledVersion: "b4800",
					LatestVersion:    "b8500",
					DownloadURL:      "https://github.com/ggml-org/llama.cpp/releases/download/b8500/llama-b8500-bin-macos-arm64.tar.gz",
					Action:           core.EngineUpdateActionDownload,
					HasUpdate:        true,
				},
			},
		}, nil
	}
	updatedCh := make(chan string, 1)
	s.engineUpdateRunFn = func(name string) (core.DownloadTask, *core.ApiError) {
		updatedCh <- name
		return core.DownloadTask{
			TaskId:      "upd-1",
			Kind:        "engine",
			State:       "downloading",
			DisplayName: "llama.cpp · b8500",
			SourceUrl:   "https://github.com/ggml-org/llama.cpp/releases/download/b8500/llama-b8500-bin-macos-arm64.tar.gz",
		}, nil
	}

	s.engineUpdates = map[string]core.EngineUpdateInfo{
		"llama-cpp-default": {
			Engine:           "llama-cpp-default",
			Family:           "llama-cpp",
			InstalledVersion: "b4800",
			LatestVersion:    "b8500",
			DownloadURL:      "https://github.com/ggml-org/llama.cpp/releases/download/b8500/llama-b8500-bin-macos-arm64.tar.gz",
			Action:           core.EngineUpdateActionDownload,
			HasUpdate:        true,
		},
	}

	tt := newServicesTester(s)
	tt.Frame()
	texts := strings.Join(tt.Texts(), "\n")
	for _, want := range []string{"检查引擎更新", "有新版 b8500", "一键更新"} {
		if !strings.Contains(texts, want) {
			t.Fatalf("expected library engines tab to contain %q, got:\n%s", want, texts)
		}
	}

	// Click One-Click Update -> directly invokes engineUpdateRunFn without opening drawer.
	click(t, tt, "更新 llama-cpp-default")
	select {
	case updatedEngine := <-updatedCh:
		if updatedEngine != "llama-cpp-default" {
			t.Fatalf("expected one-click update for llama-cpp-default, got %q", updatedEngine)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for engineUpdateRunFn")
	}
	select {
	case fn := <-uiQueue:
		fn()
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for workDownload UI callback")
	}
	drainUI()
	tt.Frame()
	if s.downloadsUI != nil && s.downloadsUI.engineDrawerOpen {
		t.Fatal("expected one-click update not to open engine download drawer")
	}

	// Active update task renders disabled "更新中…".
	s.downloads = []core.DownloadTask{
		{
			TaskId:    "upd-1",
			Kind:      "engine",
			State:     "downloading",
			SourceUrl: "https://github.com/ggml-org/llama.cpp/releases/download/b8500/llama-b8500-bin-macos-arm64.tar.gz",
		},
	}
	tt.Frame()
	if !strings.Contains(strings.Join(tt.Texts(), "\n"), "更新中…") {
		t.Fatalf("expected active engine update to show 更新中…, got:\n%s", strings.Join(tt.Texts(), "\n"))
	}

	// Click manual Check Engine Updates button.
	click(t, tt, "检查引擎更新")
	select {
	case <-checkDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for engineUpdateCheckFn")
	}
	// Drain both applyEngineUpdatesStatus and finishWork callbacks.
	for i := 0; i < 2; i++ {
		select {
		case fn := <-uiQueue:
			fn()
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for checkEngineUpdates UI callback")
		}
	}
	drainUI()
}

func TestEngineUpdatesNotificationSummaryAndDedupe(t *testing.T) {
	restore := l10n.SetTestLocale(l10n.LanguageZhHans)
	defer restore()

	s, _ := servicesP2State(t)
	s.selected = "services"

	payload, _ := json.Marshal(map[string]any{
		"manual": false,
		"status": core.EngineUpdatesStatus{
			State:          core.EngineUpdateCheckReady,
			UpdatableCount: 1,
			Updates: map[string]core.EngineUpdateInfo{
				"mlx-lm": {
					Engine:           "mlx-lm",
					Family:           "mlx-lm",
					InstalledVersion: "0.31.0",
					LatestVersion:    "0.35.0",
					Action:           core.EngineUpdateActionVenv,
					HasUpdate:        true,
				},
			},
		},
	})
	evt := core.Event{Type: "engine.updates", Data: payload}
	s.ingestCoreEvent(evt)
	s.ingestCoreEvent(evt) // duplicate signature must not push twice

	if s.notifs == nil || len(s.notifs.items) != 1 {
		t.Fatalf("expected 1 deduplicated engine update notification, got %+v", s.notifs)
	}
	item := s.notifs.items[0]
	if item.Title != "发现引擎更新" || !strings.Contains(item.Body, "mlx-lm 0.35.0") {
		t.Fatalf("unexpected notification content: title=%q body=%q", item.Title, item.Body)
	}
	if item.Primary == nil {
		t.Fatal("expected primary action on engine update notification")
	}
	item.Primary.OnClick()
	if s.selected != "library" || s.libraryTab != 1 {
		t.Fatalf("expected primary action to navigate to library engines tab, got selected=%q tab=%d", s.selected, s.libraryTab)
	}
}
