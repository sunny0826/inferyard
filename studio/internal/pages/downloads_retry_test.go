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

func waitForLifecycleSignal(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func TestRetryCardsDisableAndDeduplicateWhileActive(t *testing.T) {
	qwen, ok := core.CatalogEntryByID("model-qwen3-0.6b-q8")
	if !ok {
		t.Fatal("missing model-qwen3-0.6b-q8")
	}
	donePayload := `{"command":"model acquire","status":"prepared","completeness":"complete","details":{"bytes":639446688,"sha256":"` + qwen.SHA256 + `"}}`

	t.Run("failed card", func(t *testing.T) {
		attempt1Gate := make(chan struct{})
		close(attempt1Gate)
		retryStarted := make(chan struct{}, 2)
		retryGate := make(chan struct{})

		var mu sync.Mutex
		var acquireCalls int
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
			func(ctx context.Context, _ func(string), _ string, args ...string) (inferyard.Execution, error) {
				if len(args) >= 2 && args[0] == "model" && args[1] == "acquire" {
					mu.Lock()
					acquireCalls++
					cur := acquireCalls
					mu.Unlock()
					if cur == 1 {
						<-attempt1Gate
						return inferyard.Execution{ExitCode: inferyard.ExitBlocked}, nil
					}
					retryStarted <- struct{}{}
					select {
					case <-retryGate:
					case <-ctx.Done():
						return inferyard.Execution{ExitCode: inferyard.ExitCancelled}, nil
					}
					return inferyard.Execution{ExitCode: inferyard.ExitComplete, Stdout: []byte(donePayload)}, nil
				}
				return inferyard.Execution{ExitCode: inferyard.ExitBlocked}, nil
			},
		)

		updates := make(chan func(), 32)
		s := testState()
		s.ctx = context.Background()
		s.service = svc
		s.updateWindow = func(fn func()) { updates <- fn }
		s.ensureDownloadsUI().hostPlatform = "darwin/arm64"
		tt := ui.NewTester(s.downloadsPageView, 1120, 2600)

		clickDownloadsTab(t, tt, "模型")
		click(t, tt, "ModelScope")
		click(t, tt, "下载模型 · model-qwen3-0.6b-q8")
		waitForWorkAndDownloads(t, svc, &s, updates, 1)

		origFailed := s.downloads[0]
		if origFailed.State != "failed" {
			t.Fatalf("expected initial task failed, got %+v", origFailed)
		}
		tt.Frame()

		click(t, tt, "重新创建下载任务")
		waitForLifecycleSignal(t, retryStarted, "failed retry start")
		waitUntilBusyClears(t, &s, updates)
		tt.Frame()

		if len(s.downloads) != 2 || !s.downloadRetryActive(origFailed) {
			t.Fatalf("expected active retry tracked for failed card, downloads=%+v", s.downloads)
		}
		unrelated := core.DownloadTask{TaskId: "unrelated", Kind: "model", SourceUrl: "https://example.test/unrelated.gguf", State: "failed"}
		if s.downloadRetryActive(unrelated) {
			t.Fatal("unrelated failed task falsely marked retry-active")
		}
		emptySource := core.DownloadTask{TaskId: "empty-1", Kind: "model", State: "failed"}
		s.downloads = append(s.downloads, core.DownloadTask{TaskId: "empty-2", Kind: "model", State: "downloading"})
		if s.downloadRetryActive(emptySource) {
			t.Fatal("empty SourceUrl matched synthetic downloading task")
		}
		s.downloads = svc.Downloads()
		tt.Frame()

		click(t, tt, "重新创建下载任务")
		drainWorkUpdates(updates)
		mu.Lock()
		calls := acquireCalls
		mu.Unlock()
		if len(svc.Downloads()) != 2 || calls != 2 {
			t.Fatalf("clicking failed retry card while active queued duplicate: len=%d calls=%d", len(svc.Downloads()), calls)
		}

		close(retryGate)
		waitForWorkAndDownloads(t, svc, &s, updates, 2)
		if s.downloads[0].State != "done" {
			t.Fatalf("expected retried task done, got %+v", s.downloads[0])
		}
	})

	t.Run("cancelled card", func(t *testing.T) {
		attempt1Started := make(chan struct{}, 2)
		retryStarted := make(chan struct{}, 2)
		retryGate := make(chan struct{})

		var mu sync.Mutex
		var acquireCalls int
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
			func(ctx context.Context, _ func(string), _ string, args ...string) (inferyard.Execution, error) {
				if len(args) >= 2 && args[0] == "model" && args[1] == "acquire" {
					mu.Lock()
					acquireCalls++
					cur := acquireCalls
					mu.Unlock()
					if cur == 1 {
						attempt1Started <- struct{}{}
						<-ctx.Done()
						return inferyard.Execution{ExitCode: inferyard.ExitCancelled}, nil
					}
					retryStarted <- struct{}{}
					select {
					case <-retryGate:
					case <-ctx.Done():
						return inferyard.Execution{ExitCode: inferyard.ExitCancelled}, nil
					}
					return inferyard.Execution{ExitCode: inferyard.ExitComplete, Stdout: []byte(donePayload)}, nil
				}
				return inferyard.Execution{ExitCode: inferyard.ExitBlocked}, nil
			},
		)

		updates := make(chan func(), 32)
		s := testState()
		s.ctx = context.Background()
		s.service = svc
		s.updateWindow = func(fn func()) { updates <- fn }
		s.ensureDownloadsUI().hostPlatform = "darwin/arm64"
		tt := ui.NewTester(s.downloadsPageView, 1120, 2600)

		clickDownloadsTab(t, tt, "模型")
		click(t, tt, "ModelScope")
		click(t, tt, "下载模型 · model-qwen3-0.6b-q8")
		waitForLifecycleSignal(t, attempt1Started, "initial acquire for cancel")
		waitUntilBusyClears(t, &s, updates)

		if _, err := svc.CancelDownload(s.downloads[0].TaskId); err != nil {
			t.Fatalf("CancelDownload failed: %+v", err)
		}
		waitForWorkAndDownloads(t, svc, &s, updates, 1)

		origCancelled := s.downloads[0]
		if origCancelled.State != "cancelled" {
			t.Fatalf("expected cancelled state, got %+v", origCancelled)
		}
		tt.Frame()

		click(t, tt, "继续下载")
		waitForLifecycleSignal(t, retryStarted, "cancelled retry start")
		waitUntilBusyClears(t, &s, updates)
		tt.Frame()

		if len(s.downloads) != 2 || !s.downloadRetryActive(origCancelled) {
			t.Fatalf("expected active retry tracked for cancelled card, downloads=%+v", s.downloads)
		}

		click(t, tt, "继续下载")
		drainWorkUpdates(updates)
		mu.Lock()
		calls := acquireCalls
		mu.Unlock()
		if len(svc.Downloads()) != 2 || calls != 2 {
			t.Fatalf("clicking cancelled retry card while active queued duplicate: len=%d calls=%d", len(svc.Downloads()), calls)
		}

		close(retryGate)
		waitForWorkAndDownloads(t, svc, &s, updates, 2)
		if s.downloads[0].State != "done" {
			t.Fatalf("expected cancelled-card retry done, got %+v", s.downloads[0])
		}
	})
}
