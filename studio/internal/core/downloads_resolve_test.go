package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"os"
	"sync"
	"testing"
)

func TestAutoNamedDownloadDeduplicatesActiveRequests(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	qwen, ok := CatalogEntryByID("model-qwen3-0.6b-q8")
	if !ok {
		t.Fatal("missing model-qwen3-0.6b-q8")
	}
	engEntry, ok := CatalogEntryByID("engine-macos-arm64")
	if !ok {
		t.Fatal("missing engine-macos-arm64")
	}
	pinnedMS := "https://modelscope.cn/Qwen/Qwen3-0.6B-GGUF/blob/" + qwen.Revision + "/Qwen3-0.6B-Q8_0.gguf"

	modelStarted := make(chan struct{}, 4)
	engineStarted := make(chan struct{}, 4)
	modelGate := make(chan struct{})
	engineGate := make(chan struct{})

	s := testService(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			return v, nil
		}
		if len(args) >= 2 && args[0] == "model" && args[1] == "resolve" {
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
					"revision":            qwen.Revision,
					"path":                "Qwen3-0.6B-Q8_0.gguf",
					"pinned_url":          pinnedMS,
					"download_url":        "https://modelscope.cn/api/v1/models/Qwen/Qwen3-0.6B-GGUF/repo?Revision=" + qwen.Revision + "&FilePath=Qwen3-0.6B-Q8_0.gguf",
					"filename":            "Qwen3-0.6B-Q8_0.gguf",
					"bytes":               qwen.Bytes,
					"already_pinned":      false,
					"model_requests_sent": 0,
				},
			})
			return inferyard.Execution{ExitCode: inferyard.ExitComplete, Stdout: payload}, nil
		}
		if len(args) >= 2 && args[0] == "model" && args[1] == "acquire" {
			modelStarted <- struct{}{}
			<-modelGate
			payload := `{"command":"model acquire","status":"prepared","completeness":"complete","details":{"bytes":639446688,"sha256":"` + qwen.SHA256 + `"}}`
			return inferyard.Execution{ExitCode: inferyard.ExitComplete, Stdout: []byte(payload)}, nil
		}
		if len(args) >= 2 && args[0] == "engine" && args[1] == "prepare" {
			engineStarted <- struct{}{}
			<-engineGate
			payload := `{"command":"engine prepare","status":"prepared","completeness":"complete","details":{"engine":"/tmp/llama-server","bytes":100,"sha256":"` + engEntry.SHA256 + `"}}`
			return inferyard.Execution{ExitCode: inferyard.ExitComplete, Stdout: []byte(payload)}, nil
		}
		return result(inferyard.ExitBlocked), nil
	})
	root := t.TempDir()
	ws := root + "/ws"
	_ = os.MkdirAll(ws+"/models", 0o700)
	s.settings.WorkspaceRoot = &ws

	var wg sync.WaitGroup
	modelTasks := make([]DownloadTask, 4)
	modelErrs := make([]*ApiError, 4)
	for i := range 4 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			modelTasks[idx], modelErrs[idx] = s.CreateOrResolveModelDownload(context.Background(), CreateDownloadRequest{SourceUrl: qwen.MirrorURL}, "")
		}(i)
	}
	wg.Wait()
	for i := range 4 {
		if modelErrs[i] != nil {
			t.Fatalf("concurrent CreateOrResolveModelDownload[%d] error: %+v", i, modelErrs[i])
		}
		if modelTasks[i].TaskId != modelTasks[0].TaskId {
			t.Fatalf("concurrent auto-named model requests created distinct tasks: %q vs %q", modelTasks[i].TaskId, modelTasks[0].TaskId)
		}
	}
	<-modelStarted

	// Subsequent request with pinned URL of same task also returns the active task.
	dupPinned, apiErr := s.CreateOrResolveModelDownload(context.Background(), CreateDownloadRequest{SourceUrl: pinnedMS}, "")
	if apiErr != nil || dupPinned.TaskId != modelTasks[0].TaskId {
		t.Fatalf("pinned duplicate did not reuse active task: task=%+v err=%+v", dupPinned, apiErr)
	}

	eng1, e1 := s.CreateEngineDownload(engEntry.URL, "")
	eng2, e2 := s.CreateEngineDownload(engEntry.URL, "")
	if e1 != nil || e2 != nil || eng2.TaskId != eng1.TaskId {
		t.Fatalf("engine auto-named dedup failed: t1=%+v e1=%+v t2=%+v e2=%+v", eng1, e1, eng2, e2)
	}
	<-engineStarted

	if len(s.Downloads()) != 2 {
		t.Fatalf("expected exactly 1 model + 1 engine active tasks, got %+v", s.Downloads())
	}

	close(modelGate)
	close(engineGate)
	wait(t, func() bool {
		tasks := s.Downloads()
		return len(tasks) == 2 && tasks[0].State == "done" && tasks[1].State == "done"
	})

	// Once done, a new auto-named request creates a fresh task.
	nextModel, errNext := s.CreateOrResolveModelDownload(context.Background(), CreateDownloadRequest{SourceUrl: qwen.MirrorURL}, "")
	if errNext != nil || nextModel.TaskId == modelTasks[0].TaskId {
		t.Fatalf("completed task blocked subsequent download: next=%+v err=%+v", nextModel, errNext)
	}
	<-modelStarted
	wait(t, func() bool { return s.Downloads()[0].State == "done" })
}
