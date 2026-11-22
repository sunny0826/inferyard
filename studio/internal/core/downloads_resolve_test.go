package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/inferyard"
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
