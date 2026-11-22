package pages

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func testDownloadService(
	t *testing.T,
	execute func(context.Context, string, ...string) (inferyard.Execution, error),
	execStream func(context.Context, func(string), string, ...string) (inferyard.Execution, error),
) *core.Service {
	t.Helper()
	root := t.TempDir()
	ws := filepath.Join(root, "ws")
	models := filepath.Join(root, "models")
	results := filepath.Join(root, "results")
	for _, dir := range []string{ws, models, results} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	settingsPath := filepath.Join(root, "settings.json")
	rawSettings, _ := json.Marshal(core.UserSettings{
		WorkspaceRoot: ptr(ws),
		ModelsRoot:    ptr(models),
		ResultsRoot:   ptr(results),
		InferyardPath: ptr(os.Args[0]),
	})
	if err := os.WriteFile(settingsPath, rawSettings, 0o600); err != nil {
		t.Fatal(err)
	}
	svc, err := core.New(context.Background(), core.Options{
		SettingsPath:  settingsPath,
		HostStatePath: filepath.Join(root, "host.json"),
		Execute:       execute,
		ExecStream:    execStream,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })
	return svc
}

func modelResolvePayload(rawURL, sha string) []byte {
	platform := "huggingface"
	host := "huggingface.co"
	if strings.Contains(rawURL, "modelscope.cn") {
		platform = "modelscope"
		host = "modelscope.cn"
	}
	pinned := "https://" + host + "/Qwen/Qwen3-0.6B-GGUF/blob/" + sha + "/Qwen3-0.6B-Q8_0.gguf"
	payload, _ := json.Marshal(map[string]any{
		"command":      "model resolve",
		"status":       "resolved",
		"completeness": "complete",
		"run_id":       nil,
		"evidence_dir": nil,
		"limitations":  []string{},
		"details": map[string]any{
			"definition":          "model-resolve.v1",
			"platform":            platform,
			"repository":          "Qwen/Qwen3-0.6B-GGUF",
			"revision":            sha,
			"path":                "Qwen3-0.6B-Q8_0.gguf",
			"pinned_url":          pinned,
			"download_url":        "https://" + host + "/Qwen/Qwen3-0.6B-GGUF/resolve/" + sha + "/Qwen3-0.6B-Q8_0.gguf",
			"filename":            "Qwen3-0.6B-Q8_0.gguf",
			"bytes":               639446688,
			"already_pinned":      false,
			"model_requests_sent": 0,
		},
	})
	return payload
}

func emptyAssetsScanPayload() []byte {
	payload, _ := json.Marshal(map[string]any{
		"command":      "assets scan",
		"status":       "scanned",
		"completeness": "complete",
		"limitations":  []string{},
		"details": inferyard.AssetScan{
			Definition: inferyard.AssetScanDefinition,
			Models:     []inferyard.ScannedModel{},
			Engines:    []inferyard.ScannedEngine{},
		},
	})
	return payload
}

func waitForWorkAndDownloads(t *testing.T, svc *core.Service, s *viewState, updates <-chan func(), expectedCount int) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		for {
			select {
			case fn := <-updates:
				fn()
			default:
				goto drained
			}
		}
	drained:
		tasks := svc.Downloads()
		allDone := len(tasks) >= expectedCount
		for _, task := range tasks {
			if task.State == "downloading" {
				allDone = false
				break
			}
		}
		if !s.busy && allDone {
			s.downloads = tasks
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d downloads (busy=%v downloads=%+v)", expectedCount, s.busy, svc.Downloads())
}

func TestPublicModelDownloadsIgnoreCustomTokenEnvAndDownloadMLXDirectory(t *testing.T) {
	sha := "23749fefcc72300e3a2ad315e1317431b06b590a"
	var mu sync.Mutex
	var resolveCalls [][]string
	var acquireCalls [][]string

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
				mu.Lock()
				resolveCalls = append(resolveCalls, slices.Clone(args))
				mu.Unlock()
				source := ""
				for i := 0; i+1 < len(args); i++ {
					if args[i] == "--source" {
						source = args[i+1]
					}
				}
				return inferyard.Execution{ExitCode: inferyard.ExitComplete, Stdout: modelResolvePayload(source, sha)}, nil
			}
			return inferyard.Execution{ExitCode: inferyard.ExitComplete}, nil
		},
		func(_ context.Context, _ func(string), _ string, args ...string) (inferyard.Execution, error) {
			if len(args) >= 2 && args[0] == "engine" && args[1] == "prepare" {
				return inferyard.Execution{
					ExitCode: inferyard.ExitComplete,
					Stdout:   []byte(`{"command":"engine prepare","status":"prepared","completeness":"complete","details":{"engine":"/tmp/llama-server","bytes":100,"sha256":"` + strings.Repeat("a", 64) + `"}}`),
				}, nil
			}
			if len(args) >= 2 && args[0] == "model" && args[1] == "acquire" {
				mu.Lock()
				acquireCalls = append(acquireCalls, slices.Clone(args))
				mu.Unlock()
				return inferyard.Execution{
					ExitCode: inferyard.ExitComplete,
					Stdout:   []byte(`{"command":"model acquire","status":"prepared","completeness":"complete","details":{"bytes":100,"sha256":"` + strings.Repeat("b", 64) + `"}}`),
				}, nil
			}
			return inferyard.Execution{ExitCode: inferyard.ExitBlocked}, nil
		},
	)

	updates := make(chan func(), 32)
	s := testState()
	s.ctx = context.Background()
	s.service = svc
	s.tokenEnv = "TEST_HF_TOKEN"
	s.updateWindow = func(fn func()) { updates <- fn }
	st := s.ensureDownloadsUI()
	st.hostPlatform = "darwin/arm64"
	tt := ui.NewTester(s.downloadsPageView, 1120, 2600)

	expectedDownloads := 0
	if core.DefaultEngineURL() != "" && core.DefaultModelURL() != "" {
		click(t, tt, "下载推荐组合")
		expectedDownloads += 2
		waitForWorkAndDownloads(t, svc, &s, updates, expectedDownloads)
		tt.Frame()
	}

	clickDownloadsTab(t, tt, "模型")
	click(t, tt, "ModelScope")

	// Row download on ModelScope must omit s.tokenEnv.
	click(t, tt, "下载模型 · model-qwen3-0.6b-q8")
	expectedDownloads++
	waitForWorkAndDownloads(t, svc, &s, updates, expectedDownloads)
	tt.Frame()

	// Drawer download on ModelScope must omit s.tokenEnv.
	click(t, tt, "模型详情 · model-qwen3-0.6b-q8")
	click(t, tt, "下载此模型 · model-qwen3-0.6b-q8")
	expectedDownloads++
	waitForWorkAndDownloads(t, svc, &s, updates, expectedDownloads)
	click(t, tt, "关闭Qwen3-0.6B-Q8_0")

	// MLX directory model on HuggingFace (darwin/arm64) must dispatch directory acquire without s.tokenEnv.
	click(t, tt, "HuggingFace")
	mlxEntry, _ := core.CatalogEntryByID("model-qwen3-0.6b-mlx")
	click(t, tt, "下载模型 · model-qwen3-0.6b-mlx")
	expectedDownloads++
	waitForWorkAndDownloads(t, svc, &s, updates, expectedDownloads)
	tt.Frame()

	mu.Lock()
	for _, args := range resolveCalls {
		if slices.Contains(args, "--token-env") {
			mu.Unlock()
			t.Fatalf("public preset resolve inherited private tokenEnv: %v", args)
		}
	}
	var sawMLXDir bool
	for _, args := range acquireCalls {
		if slices.Contains(args, "--token-env") {
			mu.Unlock()
			t.Fatalf("public preset acquire inherited private tokenEnv: %v", args)
		}
		if slices.Contains(args, mlxEntry.URL) && slices.Contains(args, "--kind") && slices.Contains(args, "directory") {
			sawMLXDir = true
		}
	}
	mu.Unlock()
	if !sawMLXDir {
		t.Fatalf("clicking MLX directory model did not invoke directory acquire; calls=%v", acquireCalls)
	}

	// Custom model download MUST pass s.tokenEnv.
	customURL := "https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/resolve/main/Qwen3-0.6B-Q8_0.gguf?download=true"
	st.customModelURL = customURL
	click(t, tt, "自定义模型地址")
	tt.Advance(500 * time.Millisecond)
	tt.Frame()
	click(t, tt, "下载自定义模型")
	expectedDownloads++
	waitForWorkAndDownloads(t, svc, &s, updates, expectedDownloads)

	mu.Lock()
	defer mu.Unlock()
	lastResolve := resolveCalls[len(resolveCalls)-1]
	lastAcquire := acquireCalls[len(acquireCalls)-1]
	if !slices.Contains(lastResolve, "--token-env") || !slices.Contains(lastResolve, "TEST_HF_TOKEN") {
		t.Fatalf("custom model resolve dropped tokenEnv: %v", lastResolve)
	}
	if !slices.Contains(lastAcquire, "--token-env") || !slices.Contains(lastAcquire, "TEST_HF_TOKEN") {
		t.Fatalf("custom model acquire dropped tokenEnv: %v", lastAcquire)
	}
}

func TestOutOfOrderModelResolveIgnoresStaleResponse(t *testing.T) {
	urlA := "https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/resolve/main/Qwen3-0.6B-Q8_0.gguf?download=true"
	urlB := "https://huggingface.co/Qwen/Qwen3-4B-GGUF/resolve/main/Qwen3-4B-Q4_K_M.gguf?download=true"
	sha1 := "1111111111111111111111111111111111111111"
	sha2 := "2222222222222222222222222222222222222222"

	started1 := make(chan struct{})
	started2 := make(chan struct{})
	gate1 := make(chan struct{})
	gate2 := make(chan struct{})

	var mu sync.Mutex
	var resolveAttempt int
	var acquireCalls [][]string

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
				mu.Lock()
				resolveAttempt++
				attempt := resolveAttempt
				mu.Unlock()
				switch attempt {
				case 1:
					close(started1)
					<-gate1
					return inferyard.Execution{ExitCode: inferyard.ExitComplete, Stdout: modelResolvePayload(urlA, sha1)}, nil
				case 2:
					close(started2)
					<-gate2
					return inferyard.Execution{ExitCode: inferyard.ExitComplete, Stdout: modelResolvePayload(urlA, sha2)}, nil
				}
			}
			return inferyard.Execution{ExitCode: inferyard.ExitBlocked}, nil
		},
		func(_ context.Context, _ func(string), _ string, args ...string) (inferyard.Execution, error) {
			if len(args) >= 2 && args[0] == "model" && args[1] == "acquire" {
				mu.Lock()
				acquireCalls = append(acquireCalls, slices.Clone(args))
				mu.Unlock()
				return inferyard.Execution{
					ExitCode: inferyard.ExitComplete,
					Stdout:   []byte(`{"command":"model acquire","status":"prepared","completeness":"complete","details":{"bytes":100,"sha256":"` + strings.Repeat("b", 64) + `"}}`),
				}, nil
			}
			return inferyard.Execution{ExitCode: inferyard.ExitBlocked}, nil
		},
	)

	updates := make(chan func(), 16)
	s := testState()
	s.ctx = context.Background()
	s.service = svc
	s.updateWindow = func(fn func()) { updates <- fn }
	st := s.ensureDownloadsUI()
	st.customModelURL = urlA

	tt := ui.NewTester(s.downloadsPageView, 1120, 2600)
	clickDownloadsTab(t, tt, "模型")
	click(t, tt, "自定义模型地址")
	tt.Advance(500 * time.Millisecond)
	tt.Frame()

	// Launch request #1 for urlA.
	click(t, tt, "解析地址")
	<-started1

	// Switch to urlB, then back to urlA, and launch request #2.
	st.customModelURL = urlB
	tt.Frame()
	st.customModelURL = urlA
	tt.Frame()
	click(t, tt, "解析地址")
	<-started2

	// Complete stale request #1 while request #2 is still running.
	close(gate1)
	time.Sleep(50 * time.Millisecond)
	tt.Frame()
	if tt.HasText(sha1) {
		t.Fatalf("stale response sha1 leaked into UI: %v", tt.Texts())
	}
	if !tt.HasText("正在解析远端模型版本…") {
		t.Fatalf("expected active resolving indicator while #2 is pending: %v", tt.Texts())
	}

	// Complete request #2 and verify sha2 wins.
	close(gate2)
	select {
	case fn := <-updates:
		fn()
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for resolve #2 update")
	}
	tt.Frame()
	if !tt.HasText("已解析版本："+sha2) || tt.HasText(sha1) {
		t.Fatalf("expected sha2 and no sha1 after #2 completed: %v", tt.Texts())
	}

	click(t, tt, "下载自定义模型")
	waitForWorkAndDownloads(t, svc, &s, updates, 1)

	mu.Lock()
	defer mu.Unlock()
	if len(acquireCalls) != 1 || !slices.Contains(acquireCalls[0], "https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/blob/"+sha2+"/Qwen3-0.6B-Q8_0.gguf") {
		t.Fatalf("download submitted wrong pinned revision: %v", acquireCalls)
	}
}

func TestModelPresetStatusRequiresMatchingSourceURL(t *testing.T) {
	qwen, ok := core.CatalogEntryByID("model-qwen3-0.6b-q8")
	if !ok {
		t.Fatal("missing model-qwen3-0.6b-q8")
	}
	s := testState()
	s.ensureDownloadsUI().hostPlatform = "darwin/arm64"
	s.registry.Models = []core.ModelAsset{{Name: "Qwen3-0.6B-Q8_0.gguf", Available: true}}
	s.downloads = []core.DownloadTask{{
		TaskId:      "other-commit",
		Kind:        "model",
		DisplayName: "other-repo · Qwen3-0.6B-Q8_0.gguf",
		OutName:     "Qwen3-0.6B-Q8_0",
		State:       "downloading",
		SourceUrl:   "https://huggingface.co/other/repo/blob/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/Qwen3-0.6B-Q8_0.gguf",
	}}
	if s.isModelEntryDownloading(qwen) || s.isModelEntryDownloaded(qwen) {
		t.Fatal("filename-only collision falsely marked catalog preset as downloading/downloaded")
	}

	tt := ui.NewTester(s.downloadsPageView, 1120, 2600)
	clickDownloadsTab(t, tt, "模型")
	if tt.HasText("已下载") {
		t.Fatalf("unexpected 已下载 badge for filename-only local model: %v", tt.Texts())
	}
	btn, ok := tt.Find("下载模型 · " + qwen.ID)
	if !ok || btn.W <= 0 {
		t.Fatalf("download button missing when only unrelated commit is downloading: %v", tt.Texts())
	}

	s.downloads = append(s.downloads, core.DownloadTask{
		TaskId:    "exact-qwen",
		Kind:      "model",
		State:     "downloading",
		SourceUrl: qwen.URL,
		OutDir:    "/models/Qwen3-0.6B-Q8_0",
	})
	if !s.isModelEntryDownloading(qwen) {
		t.Fatal("exact SourceUrl downloading task did not match")
	}
	s.downloads[1].State = "done"
	// Asset at an unrelated OutDir must not mark exact-qwen as downloaded.
	s.registry.Models = []core.ModelAsset{{Name: "Qwen3-0.6B-Q8_0.gguf", Path: "/models/other/Qwen3-0.6B-Q8_0.gguf", Available: true}}
	if s.isModelEntryDownloaded(qwen) || s.isModelEntryDownloading(qwen) {
		t.Fatal("unrelated asset path falsely marked exact done task as downloaded")
	}
	// Matching OutDir asset marks downloaded, and removing the asset clears it despite historical done task.
	s.registry.Models = []core.ModelAsset{{Name: "Qwen3-0.6B-Q8_0.gguf", Path: "/models/Qwen3-0.6B-Q8_0/Qwen3-0.6B-Q8_0.gguf", Available: true}}
	if !s.isModelEntryDownloaded(qwen) || s.isModelEntryDownloading(qwen) {
		t.Fatal("exact SourceUrl done task with live asset did not match downloaded state")
	}
	s.registry.Models = nil
	if s.isModelEntryDownloaded(qwen) {
		t.Fatal("downloaded badge persisted after removing asset from registry")
	}
}
