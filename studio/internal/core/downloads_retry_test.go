package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"strings"
	"sync"
	"testing"
	"time"
)

func retryTestResolvePayload(revision, pinnedURL string, sizeBytes int64) inferyard.Execution {
	payload, _ := json.Marshal(map[string]any{
		"command":      "model resolve",
		"status":       "resolved",
		"completeness": "complete",
		"run_id":       nil,
		"evidence_dir": nil,
		"limitations":  []string{},
		"details": map[string]any{
			"definition":          "model-resolve.v1",
			"platform":            "modelscope",
			"repository":          "Qwen/Qwen3-0.6B-GGUF",
			"revision":            revision,
			"path":                "Qwen3-0.6B-Q8_0.gguf",
			"pinned_url":          pinnedURL,
			"download_url":        "https://modelscope.cn/api/v1/models/Qwen/Qwen3-0.6B-GGUF/repo?Revision=" + revision + "&FilePath=Qwen3-0.6B-Q8_0.gguf",
			"filename":            "Qwen3-0.6B-Q8_0.gguf",
			"bytes":               sizeBytes,
			"already_pinned":      false,
			"model_requests_sent": 0,
		},
	})
	return inferyard.Execution{ExitCode: inferyard.ExitComplete, Stdout: payload}
}

func waitForSignal(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func TestRetryDownloadDeduplicatesActiveRetry(t *testing.T) {
	qwen, ok := CatalogEntryByID("model-qwen3-0.6b-q8")
	if !ok {
		t.Fatal("missing model-qwen3-0.6b-q8")
	}
	pinnedMS := "https://modelscope.cn/Qwen/Qwen3-0.6B-GGUF/blob/" + qwen.Revision + "/Qwen3-0.6B-Q8_0.gguf"
	donePayload := `{"command":"model acquire","status":"prepared","completeness":"complete","details":{"bytes":639446688,"sha256":"` + qwen.SHA256 + `"}}`

	t.Run("failed", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		retryStarted := make(chan struct{}, 4)
		retryGate := make(chan struct{})
		doneStarted := make(chan struct{}, 2)

		var mu sync.Mutex
		var attempts int
		s := testService(t, func(ctx context.Context, _ string, args ...string) (inferyard.Execution, error) {
			if v, ok := version(args); ok {
				return v, nil
			}
			if len(args) >= 2 && args[0] == "model" && args[1] == "resolve" {
				return retryTestResolvePayload(qwen.Revision, pinnedMS, qwen.Bytes), nil
			}
			if len(args) >= 2 && args[0] == "model" && args[1] == "acquire" {
				mu.Lock()
				attempts++
				cur := attempts
				mu.Unlock()
				switch cur {
				case 1:
					return result(inferyard.ExitBlocked), nil
				case 2:
					retryStarted <- struct{}{}
					select {
					case <-retryGate:
					case <-ctx.Done():
						return result(inferyard.ExitCancelled), nil
					}
					return result(inferyard.ExitBlocked), nil
				default:
					doneStarted <- struct{}{}
					return inferyard.Execution{ExitCode: inferyard.ExitComplete, Stdout: []byte(donePayload)}, nil
				}
			}
			return result(inferyard.ExitBlocked), nil
		})
		models := t.TempDir()
		s.settings.ModelsRoot = &models

		first, apiErr := s.CreateOrResolveModelDownload(context.Background(), CreateDownloadRequest{SourceUrl: qwen.MirrorURL}, "")
		if apiErr != nil {
			t.Fatalf("CreateOrResolveModelDownload failed: %+v", apiErr)
		}
		wait(t, func() bool { return s.Downloads()[0].State == "failed" })

		const callers = 4
		tasks := make([]DownloadTask, callers)
		errs := make([]*ApiError, callers)
		var wg sync.WaitGroup
		for i := range callers {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				tasks[idx], errs[idx] = s.RetryDownload(first.TaskId)
			}(i)
		}
		wg.Wait()
		for i := range callers {
			if errs[i] != nil {
				t.Fatalf("concurrent RetryDownload[%d] error: %+v", i, errs[i])
			}
			if tasks[i].TaskId == first.TaskId || tasks[i].TaskId != tasks[0].TaskId {
				t.Fatalf("concurrent RetryDownload[%d] = %+v, want single new task %q", i, tasks[i], tasks[0].TaskId)
			}
		}
		waitForSignal(t, retryStarted, "first failed-card retry")

		seqDup, seqErr := s.RetryDownload(first.TaskId)
		if seqErr != nil || seqDup.TaskId != tasks[0].TaskId {
			t.Fatalf("sequential RetryDownload while active = %+v, %+v", seqDup, seqErr)
		}
		if got := s.Downloads(); len(got) != 2 || got[0].State != "downloading" || got[1].State != "failed" {
			t.Fatalf("unexpected download list during active retry: %+v", got)
		}

		// After the retry itself fails, retrying the original failed card starts a fresh task.
		close(retryGate)
		wait(t, func() bool {
			got := s.Downloads()
			return len(got) == 2 && got[0].State == "failed"
		})
		nextRetry, nextErr := s.RetryDownload(first.TaskId)
		if nextErr != nil || nextRetry.TaskId == first.TaskId || nextRetry.TaskId == tasks[0].TaskId {
			t.Fatalf("fresh retry after failed retry = %+v, %+v", nextRetry, nextErr)
		}
		waitForSignal(t, doneStarted, "second retry")
		wait(t, func() bool {
			got := s.Downloads()
			return len(got) == 3 && got[0].State == "done"
		})
	})

	t.Run("cancelled", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		attempt1Started := make(chan struct{}, 2)
		retryStarted := make(chan struct{}, 4)
		retryGate := make(chan struct{})

		var mu sync.Mutex
		var attempts int
		s := testService(t, func(ctx context.Context, _ string, args ...string) (inferyard.Execution, error) {
			if v, ok := version(args); ok {
				return v, nil
			}
			if len(args) >= 2 && args[0] == "model" && args[1] == "resolve" {
				return retryTestResolvePayload(qwen.Revision, pinnedMS, qwen.Bytes), nil
			}
			if len(args) >= 2 && args[0] == "model" && args[1] == "acquire" {
				mu.Lock()
				attempts++
				cur := attempts
				mu.Unlock()
				if cur == 1 {
					attempt1Started <- struct{}{}
					<-ctx.Done()
					return result(inferyard.ExitCancelled), nil
				}
				retryStarted <- struct{}{}
				select {
				case <-retryGate:
				case <-ctx.Done():
					return result(inferyard.ExitCancelled), nil
				}
				return inferyard.Execution{ExitCode: inferyard.ExitComplete, Stdout: []byte(donePayload)}, nil
			}
			return result(inferyard.ExitBlocked), nil
		})
		models := t.TempDir()
		s.settings.ModelsRoot = &models

		first, apiErr := s.CreateOrResolveModelDownload(context.Background(), CreateDownloadRequest{SourceUrl: qwen.MirrorURL}, "")
		if apiErr != nil {
			t.Fatalf("CreateOrResolveModelDownload failed: %+v", apiErr)
		}
		waitForSignal(t, attempt1Started, "initial cancelled test attempt")
		if _, cancelErr := s.CancelDownload(first.TaskId); cancelErr != nil {
			t.Fatalf("CancelDownload failed: %+v", cancelErr)
		}
		wait(t, func() bool { return s.Downloads()[0].State == "cancelled" })

		const callers = 4
		tasks := make([]DownloadTask, callers)
		errs := make([]*ApiError, callers)
		var wg sync.WaitGroup
		for i := range callers {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				tasks[idx], errs[idx] = s.RetryDownload(first.TaskId)
			}(i)
		}
		wg.Wait()
		for i := range callers {
			if errs[i] != nil {
				t.Fatalf("concurrent cancelled RetryDownload[%d] error: %+v", i, errs[i])
			}
			if tasks[i].TaskId == first.TaskId || tasks[i].TaskId != tasks[0].TaskId {
				t.Fatalf("concurrent cancelled RetryDownload[%d] = %+v, want single %q", i, tasks[i], tasks[0].TaskId)
			}
		}
		waitForSignal(t, retryStarted, "cancelled-card retry")

		seqDup, seqErr := s.RetryDownload(first.TaskId)
		if seqErr != nil || seqDup.TaskId != tasks[0].TaskId {
			t.Fatalf("sequential cancelled RetryDownload while active = %+v, %+v", seqDup, seqErr)
		}
		if got := s.Downloads(); len(got) != 2 || got[0].State != "downloading" || got[1].State != "cancelled" {
			t.Fatalf("unexpected download list during active cancelled retry: %+v", got)
		}

		close(retryGate)
		wait(t, func() bool {
			got := s.Downloads()
			return len(got) == 2 && got[0].State == "done"
		})
		if got := s.Downloads()[0].RequestedSourceUrl; got != qwen.MirrorURL {
			t.Fatalf("retried cancelled task RequestedSourceUrl = %q, want %q", got, qwen.MirrorURL)
		}
	})

	t.Run("different pinned revision from same mirror", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		shaA := qwen.Revision
		shaB := strings.Repeat("b", 40)
		pinnedA := "https://modelscope.cn/Qwen/Qwen3-0.6B-GGUF/blob/" + shaA + "/Qwen3-0.6B-Q8_0.gguf"
		pinnedB := "https://modelscope.cn/Qwen/Qwen3-0.6B-GGUF/blob/" + shaB + "/Qwen3-0.6B-Q8_0.gguf"

		bStarted := make(chan struct{}, 2)
		retryAStarted := make(chan struct{}, 2)
		gateB := make(chan struct{})
		gateRetryA := make(chan struct{})

		var mu sync.Mutex
		var resolveCount int
		var acquireCount int
		s := testService(t, func(ctx context.Context, _ string, args ...string) (inferyard.Execution, error) {
			if v, ok := version(args); ok {
				return v, nil
			}
			if len(args) >= 2 && args[0] == "model" && args[1] == "resolve" {
				mu.Lock()
				resolveCount++
				n := resolveCount
				mu.Unlock()
				if n == 1 {
					return retryTestResolvePayload(shaA, pinnedA, qwen.Bytes), nil
				}
				return retryTestResolvePayload(shaB, pinnedB, qwen.Bytes), nil
			}
			if len(args) >= 2 && args[0] == "model" && args[1] == "acquire" {
				src := ""
				for i := 0; i+1 < len(args); i++ {
					if args[i] == "--source" {
						src = args[i+1]
						break
					}
				}
				mu.Lock()
				acquireCount++
				cur := acquireCount
				mu.Unlock()
				if cur == 1 {
					return result(inferyard.ExitBlocked), nil
				}
				if src == pinnedB {
					bStarted <- struct{}{}
					select {
					case <-gateB:
					case <-ctx.Done():
						return result(inferyard.ExitCancelled), nil
					}
					return inferyard.Execution{ExitCode: inferyard.ExitComplete, Stdout: []byte(donePayload)}, nil
				}
				retryAStarted <- struct{}{}
				select {
				case <-gateRetryA:
				case <-ctx.Done():
					return result(inferyard.ExitCancelled), nil
				}
				return inferyard.Execution{ExitCode: inferyard.ExitComplete, Stdout: []byte(donePayload)}, nil
			}
			return result(inferyard.ExitBlocked), nil
		})
		models := t.TempDir()
		s.settings.ModelsRoot = &models

		taskA, apiErr := s.CreateOrResolveModelDownload(context.Background(), CreateDownloadRequest{SourceUrl: qwen.MirrorURL}, "")
		if apiErr != nil {
			t.Fatalf("CreateOrResolveModelDownload A failed: %+v", apiErr)
		}
		wait(t, func() bool { return s.Downloads()[0].State == "failed" })

		taskB, apiErr := s.CreateOrResolveModelDownload(context.Background(), CreateDownloadRequest{SourceUrl: qwen.MirrorURL}, "")
		if apiErr != nil {
			t.Fatalf("CreateOrResolveModelDownload B failed: %+v", apiErr)
		}
		waitForSignal(t, bStarted, "revision B acquire")

		retryA, retryErr := s.RetryDownload(taskA.TaskId)
		if retryErr != nil {
			t.Fatalf("RetryDownload(taskA) while B downloading failed: %+v", retryErr)
		}
		if retryA.TaskId == taskA.TaskId || retryA.TaskId == taskB.TaskId {
			t.Fatalf("RetryDownload(taskA) returned %+v instead of fresh retry of revision A", retryA)
		}
		if retryA.SourceUrl != pinnedA || retryA.RequestedSourceUrl != qwen.MirrorURL {
			t.Fatalf("retryA URLs = (%q, %q), want (%q, %q)", retryA.SourceUrl, retryA.RequestedSourceUrl, pinnedA, qwen.MirrorURL)
		}
		waitForSignal(t, retryAStarted, "revision A retry acquire")

		dupRetryA, dupErr := s.RetryDownload(taskA.TaskId)
		if dupErr != nil || dupRetryA.TaskId != retryA.TaskId {
			t.Fatalf("second RetryDownload(taskA) = %+v, %+v, want reuse %q", dupRetryA, dupErr, retryA.TaskId)
		}
		if got := s.Downloads(); len(got) != 3 {
			t.Fatalf("expected 3 tasks (retryA + B + failed A), got %+v", got)
		}

		close(gateB)
		close(gateRetryA)
		wait(t, func() bool {
			got := s.Downloads()
			return len(got) == 3 && got[0].State == "done" && got[1].State == "done"
		})
	})
}
