package pages

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func drainWorkUpdates(updates <-chan func()) {
	for {
		select {
		case fn := <-updates:
			fn()
		default:
			return
		}
	}
}

func waitUntilBusyClears(t *testing.T, s *viewState, updates <-chan func()) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		drainWorkUpdates(updates)
		if !s.busy {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for s.busy to clear")
}

func TestModelScopeActivePresetBlocksDuplicateQueueAndRetryPreservesState(t *testing.T) {
	qwen, ok := core.CatalogEntryByID("model-qwen3-0.6b-q8")
	if !ok {
		t.Fatal("missing model-qwen3-0.6b-q8")
	}
	pinnedMS := "https://modelscope.cn/Qwen/Qwen3-0.6B-GGUF/blob/" + qwen.Revision + "/Qwen3-0.6B-Q8_0.gguf"

	acquireStarted1 := make(chan struct{})
	acquireStarted2 := make(chan struct{})
	acquireGate1 := make(chan struct{})
	acquireGate2 := make(chan struct{})

	var mu sync.Mutex
	var attempt int

	svc := testDownloadService(
		t,
		func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
			if len(args) > 0 && args[0] == "--version" {
				return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 1.0 source=release\n")}, nil
			}
			if len(args) >= 2 && args[0] == "assets" && args[1] == "scan" {
				return inferyard.Execution{ExitCode: 0, Stdout: emptyAssetsScanPayload()}, nil
			}
			if len(args) >= 2 && args[0] == "model" && args[1] == "resolve" {
				return inferyard.Execution{ExitCode: inferyard.ExitComplete, Stdout: modelResolvePayload(qwen.MirrorURL, qwen.Revision)}, nil
			}
			return inferyard.Execution{ExitCode: inferyard.ExitComplete}, nil
		},
		func(_ context.Context, _ func(string), _ string, args ...string) (inferyard.Execution, error) {
			if len(args) >= 2 && args[0] == "model" && args[1] == "acquire" {
				mu.Lock()
				attempt++
				cur := attempt
				mu.Unlock()
				if cur == 1 {
					close(acquireStarted1)
					<-acquireGate1
					return inferyard.Execution{ExitCode: inferyard.ExitBlocked}, nil
				}
				close(acquireStarted2)
				<-acquireGate2
				return inferyard.Execution{
					ExitCode: inferyard.ExitComplete,
					Stdout:   []byte(`{"command":"model acquire","status":"prepared","completeness":"complete","details":{"bytes":639446688,"sha256":"` + qwen.SHA256 + `"}}`),
				}, nil
			}
			return inferyard.Execution{ExitCode: inferyard.ExitBlocked}, nil
		},
	)

	updates := make(chan func(), 32)
	s := testState()
	s.ctx = context.Background()
	s.service = svc
	s.updateWindow = func(fn func()) { updates <- fn }
	st := s.ensureDownloadsUI()
	st.hostPlatform = "darwin/arm64"
	tt := ui.NewTester(s.downloadsPageView, 1120, 2600)

	clickDownloadsTab(t, tt, "模型")
	click(t, tt, "ModelScope")
	click(t, tt, "下载模型 · model-qwen3-0.6b-q8")

	select {
	case <-acquireStarted1:
	case <-time.After(3 * time.Second):
		drainWorkUpdates(updates)
		t.Fatalf("timed out waiting for acquire #1 (failure=%+v)", s.failure)
	}
	waitUntilBusyClears(t, &s, updates)
	tt.Frame()

	if len(s.downloads) != 1 {
		t.Fatalf("workDownload did not immediately sync s.downloads: %+v", s.downloads)
	}
	if s.downloads[0].RequestedSourceUrl != qwen.MirrorURL || s.downloads[0].SourceUrl != pinnedMS {
		t.Fatalf("unexpected task URLs: %+v", s.downloads[0])
	}
	if !s.isModelEntryDownloading(qwen) {
		t.Fatal("ModelScope preset was not marked downloading after URL pinning")
	}

	// Simulate pre-refresh UI lag: even if s.downloads is stale/empty, 5 repeated submits must not queue duplicates.
	s.downloads = nil
	for range 5 {
		s.acquireModelSource(qwen.MirrorURL, "")
		waitUntilBusyClears(t, &s, updates)
	}
	if len(svc.Downloads()) != 1 {
		t.Fatalf("duplicate ModelScope download created during stale UI window: %+v", svc.Downloads())
	}
	tt.Frame()

	// Clicking the disabled row/drawer download controls must not queue a duplicate task.
	click(t, tt, "下载模型 · model-qwen3-0.6b-q8")
	click(t, tt, "模型详情 · model-qwen3-0.6b-q8")
	click(t, tt, "下载此模型 · model-qwen3-0.6b-q8")
	click(t, tt, "关闭Qwen3-0.6B-Q8_0")
	drainWorkUpdates(updates)
	if len(svc.Downloads()) != 1 {
		t.Fatalf("duplicate ModelScope download queued while active: %+v", svc.Downloads())
	}

	// Fail task #1, retry via UI button, and verify RequestedSourceUrl + downloading state survive retry.
	close(acquireGate1)
	waitForWorkAndDownloads(t, svc, &s, updates, 1)
	tt.Frame()
	if s.downloads[0].State != "failed" {
		t.Fatalf("expected failed state after blocked acquire, got %+v", s.downloads[0])
	}

	click(t, tt, "重新创建下载任务")
	select {
	case <-acquireStarted2:
	case <-time.After(3 * time.Second):
		drainWorkUpdates(updates)
		t.Fatalf("timed out waiting for acquire #2 (failure=%+v)", s.failure)
	}
	waitUntilBusyClears(t, &s, updates)
	tt.Frame()

	if len(s.downloads) != 2 || s.downloads[0].State != "downloading" {
		t.Fatalf("expected active retried task, got %+v", s.downloads)
	}
	if s.downloads[0].RequestedSourceUrl != qwen.MirrorURL {
		t.Fatalf("retry lost RequestedSourceUrl: %+v", s.downloads[0])
	}
	if !s.isModelEntryDownloading(qwen) {
		t.Fatal("retried ModelScope preset was not marked downloading")
	}

	close(acquireGate2)
	waitForWorkAndDownloads(t, svc, &s, updates, 2)
}
