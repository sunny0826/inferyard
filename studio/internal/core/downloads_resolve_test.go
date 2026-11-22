package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestCatalogMirrorsAndDefaultModel(t *testing.T) {
	qwen, ok := CatalogEntryByID("model-qwen3-0.6b-q8")
	if !ok {
		t.Fatal("missing model-qwen3-0.6b-q8")
	}
	if qwen.MirrorLabel != "ModelScope" || !IsResolvableModelURL(qwen.MirrorURL) {
		t.Fatalf("unexpected qwen mirror: %+v", qwen)
	}
	if DefaultModelURL() != qwen.URL {
		t.Fatalf("DefaultModelURL() = %q, want %q", DefaultModelURL(), qwen.URL)
	}
	byMirror, ok := CatalogEntryByURL(qwen.MirrorURL)
	if !ok || byMirror.ID != qwen.ID {
		t.Fatalf("CatalogEntryByURL(mirror) = %+v, %v", byMirror, ok)
	}
	q4, ok := CatalogEntryByID("model-qwen3-4b-q4km")
	if !ok || !q4.Downloadable() || q4.MirrorURL == "" || !IsResolvableModelURL(q4.MirrorURL) {
		t.Fatalf("unexpected qwen3-4b catalog entry: %+v", q4)
	}
}

func TestValidateDownloadProxyAndResolvableURL(t *testing.T) {
	for _, valid := range []string{
		"",
		"http://127.0.0.1:7890",
		"https://proxy.example.com/",
		"http://[::1]:8080/",
	} {
		if !ValidateDownloadProxy(valid) {
			t.Fatalf("expected valid proxy: %q", valid)
		}
	}
	for _, invalid := range []string{
		" http://127.0.0.1:7890",
		"socks5://127.0.0.1:1080",
		"http://user:pass@127.0.0.1:7890",
		"http://127.0.0.1:7890/path",
		"http://127.0.0.1:7890?a=1",
		"http://127.0.0.1:7890#x",
		"http://127.0.0.1:0",
		"http://127.0.0.1:70000",
		"http://127.0.0.1:",
	} {
		if ValidateDownloadProxy(invalid) {
			t.Fatalf("expected invalid proxy: %q", invalid)
		}
	}

	for _, valid := range []string{
		"https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/resolve/main/Qwen3-0.6B-Q8_0.gguf?download=true",
		"https://modelscope.cn/Qwen/Qwen3-0.6B-GGUF/resolve/master/Qwen3-0.6B-Q8_0.gguf",
		"https://modelscope.cn/models/Qwen/Qwen3-0.6B-GGUF/file/view/master/sub/m.gguf?download=1",
		"https://huggingface.co/mlx-community/Qwen3-0.6B-4bit/tree/73e3e38d981303bc594367cd910ea6eb48349da8",
		"https://huggingface.co/mlx-community/Qwen3-0.6B-4bit/tree/main",
		"https://modelscope.cn/models/Qwen/Qwen3-0.6B-4bit/tree/master",
	} {
		if !IsResolvableModelURL(valid) {
			t.Fatalf("expected resolvable url: %q", valid)
		}
		if !IsSupportedModelURL(valid) {
			t.Fatalf("expected supported url: %q", valid)
		}
	}
	for _, invalid := range []string{
		"http://huggingface.co/a/b/resolve/main/m.gguf",
		"https://example.com/a/b/resolve/main/m.gguf",
		"https://huggingface.co/a/b/resolve/main/m.gguf?token=1",
		"https://huggingface.co/a/b/resolve/main/../m.gguf",
		"https://huggingface.co/a/b/resolve/main/.gguf",
		"https://modelscope.cn/models/Qwen/tree/master",
		"https://huggingface.co/mlx-community/Qwen3-0.6B-4bit/tree/main/file.gguf",
	} {
		if IsResolvableModelURL(invalid) {
			t.Fatalf("expected non-resolvable url: %q", invalid)
		}
		if IsSupportedModelURL(invalid) {
			t.Fatalf("expected unsupported url: %q", invalid)
		}
	}

	for _, entry := range Catalog() {
		if entry.Kind != CatalogModel || !entry.Downloadable() {
			continue
		}
		if !IsResolvableModelURL(entry.URL) || !IsSupportedModelURL(entry.URL) {
			t.Fatalf("catalog model %s URL rejected by validator: %q", entry.ID, entry.URL)
		}
		if entry.MirrorURL != "" && (!IsResolvableModelURL(entry.MirrorURL) || !IsSupportedModelURL(entry.MirrorURL)) {
			t.Fatalf("catalog model %s MirrorURL rejected by validator: %q", entry.ID, entry.MirrorURL)
		}
	}
}

func TestResolveAndCreateOrResolveModelDownload(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sha := "23749fefcc72300e3a2ad315e1317431b06b590a"
	rawURL := "https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/resolve/main/Qwen3-0.6B-Q8_0.gguf?download=true"
	pinnedURL := "https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/blob/" + sha + "/Qwen3-0.6B-Q8_0.gguf"

	var mu sync.Mutex
	calls := [][]string{}
	s := testService(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			return v, nil
		}
		mu.Lock()
		calls = append(calls, slices.Clone(args))
		mu.Unlock()
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
					"platform":            "huggingface",
					"repository":          "Qwen/Qwen3-0.6B-GGUF",
					"revision":            sha,
					"path":                "Qwen3-0.6B-Q8_0.gguf",
					"pinned_url":          pinnedURL,
					"download_url":        "https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/resolve/" + sha + "/Qwen3-0.6B-Q8_0.gguf",
					"filename":            "Qwen3-0.6B-Q8_0.gguf",
					"bytes":               639446688,
					"already_pinned":      false,
					"model_requests_sent": 0,
				},
			})
			return inferyard.Execution{ExitCode: inferyard.ExitComplete, Stdout: payload}, nil
		}
		if len(args) >= 2 && args[0] == "model" && args[1] == "acquire" {
			payload := `{"command":"model acquire","status":"prepared","completeness":"complete","details":{"bytes":639446688,"sha256":"` + strings.Repeat("a", 64) + `"}}`
			return inferyard.Execution{ExitCode: inferyard.ExitComplete, Stdout: []byte(payload)}, nil
		}
		return result(inferyard.ExitBlocked), nil
	})
	models := t.TempDir()
	s.settings.ModelsRoot = &models

	resolved, apiErr := s.ResolveModelURL(context.Background(), rawURL, "HF_TOKEN")
	if apiErr != nil {
		t.Fatalf("ResolveModelURL error: %+v", apiErr)
	}
	if resolved.Revision != sha || resolved.PinnedURL != pinnedURL {
		t.Fatalf("unexpected resolved: %+v", resolved)
	}

	task, apiErr := s.CreateOrResolveModelDownload(context.Background(), CreateDownloadRequest{SourceUrl: rawURL}, "HF_TOKEN")
	if apiErr != nil {
		t.Fatalf("CreateOrResolveModelDownload error: %+v", apiErr)
	}
	if task.SourceUrl != pinnedURL {
		t.Fatalf("task.SourceUrl = %q, want %q", task.SourceUrl, pinnedURL)
	}
	wait(t, func() bool { return s.Downloads()[0].State == "done" })

	mu.Lock()
	defer mu.Unlock()
	if len(calls) < 3 {
		t.Fatalf("expected resolve, resolve, acquire calls; got %v", calls)
	}
	if !slices.Equal(calls[0], []string{"model", "resolve", "--source", rawURL, "--token-env", "HF_TOKEN"}) {
		t.Fatalf("resolve args = %v", calls[0])
	}
	if !slices.Contains(calls[2], pinnedURL) {
		t.Fatalf("acquire did not receive pinned URL: %v", calls[2])
	}
}

func TestCreateOrResolvePreservesRequestedSourceAcrossRetry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	qwen, ok := CatalogEntryByID("model-qwen3-0.6b-q8")
	if !ok {
		t.Fatal("missing model-qwen3-0.6b-q8")
	}
	pinnedURL := "https://modelscope.cn/Qwen/Qwen3-0.6B-GGUF/blob/" + qwen.Revision + "/Qwen3-0.6B-Q8_0.gguf"

	var mu sync.Mutex
	var acquireAttempt int
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
					"pinned_url":          pinnedURL,
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
			mu.Lock()
			acquireAttempt++
			attempt := acquireAttempt
			mu.Unlock()
			if attempt == 1 {
				return result(inferyard.ExitBlocked), nil
			}
			payload := `{"command":"model acquire","status":"prepared","completeness":"complete","details":{"bytes":639446688,"sha256":"` + qwen.SHA256 + `"}}`
			return inferyard.Execution{ExitCode: inferyard.ExitComplete, Stdout: []byte(payload)}, nil
		}
		return result(inferyard.ExitBlocked), nil
	})
	models := t.TempDir()
	s.settings.ModelsRoot = &models

	task, apiErr := s.CreateOrResolveModelDownload(context.Background(), CreateDownloadRequest{SourceUrl: qwen.MirrorURL}, "")
	if apiErr != nil {
		t.Fatalf("CreateOrResolveModelDownload failed: %+v", apiErr)
	}
	if task.SourceUrl != pinnedURL || task.RequestedSourceUrl != qwen.MirrorURL {
		t.Fatalf("unexpected task URLs: source=%q requested=%q", task.SourceUrl, task.RequestedSourceUrl)
	}
	wait(t, func() bool { return s.Downloads()[0].State == "failed" })

	retried, apiErr := s.RetryDownload(task.TaskId)
	if apiErr != nil {
		t.Fatalf("RetryDownload failed: %+v", apiErr)
	}
	if retried.SourceUrl != pinnedURL || retried.RequestedSourceUrl != qwen.MirrorURL {
		t.Fatalf("retry dropped requested source: source=%q requested=%q", retried.SourceUrl, retried.RequestedSourceUrl)
	}
	wait(t, func() bool { return s.Downloads()[0].State == "done" })
	if got := s.Downloads()[0].RequestedSourceUrl; got != qwen.MirrorURL {
		t.Fatalf("completed retry RequestedSourceUrl = %q, want %q", got, qwen.MirrorURL)
	}
}

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
	models := root + "/models"
	ws := root + "/ws"
	_ = os.MkdirAll(models, 0o700)
	_ = os.MkdirAll(ws, 0o700)
	s.settings.ModelsRoot = &models
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
