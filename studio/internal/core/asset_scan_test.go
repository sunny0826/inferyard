package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"

	"inferyard/studio/internal/inferyard"
)

func TestNewAndSettingsChangeTriggerAssetAndDeviceRefresh(t *testing.T) {
	root1 := t.TempDir()
	root2 := t.TempDir()
	m1 := filepath.Join(root1, "first.gguf")
	m2 := filepath.Join(root2, "second-dir")

	var mu sync.Mutex
	var assetRoots []string
	var deviceRoots []string
	firstRelease := make(chan struct{})
	firstEntered := make(chan struct{}, 1)

	execFn := func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			return v, nil
		}
		flagVal := func(name string) string {
			for i := 0; i+1 < len(args); i++ {
				if args[i] == name {
					return args[i+1]
				}
			}
			return ""
		}
		if len(args) >= 2 && args[0] == "assets" && args[1] == "scan" {
			mr := flagVal("--models-root")
			mu.Lock()
			assetRoots = append(assetRoots, mr)
			mu.Unlock()
			if mr == root1 {
				select {
				case firstEntered <- struct{}{}:
				default:
				}
				<-firstRelease
				payload, _ := json.Marshal(map[string]any{
					"command":      "assets scan",
					"status":       "scanned",
					"completeness": "complete",
					"run_id":       nil,
					"evidence_dir": nil,
					"limitations":  []string{},
					"details": map[string]any{
						"definition": "asset-scan.v1",
						"models": []map[string]any{{
							"path": m1, "name": "first", "format": "gguf",
							"size_bytes": 2048, "architecture": "llama",
							"context_length": 4096, "available": true, "reason": nil,
						}},
						"engines":             []any{},
						"model_requests_sent": 0,
					},
				})
				return inferyard.Execution{ExitCode: 0, Stdout: payload}, nil
			}
			payload, _ := json.Marshal(map[string]any{
				"command":      "assets scan",
				"status":       "scanned",
				"completeness": "complete",
				"run_id":       nil,
				"evidence_dir": nil,
				"limitations":  []string{},
				"details": map[string]any{
					"definition": "asset-scan.v1",
					"models": []map[string]any{{
						"path": m2, "name": "second-dir", "format": "directory",
						"size_bytes": 8192, "architecture": "qwen2",
						"context_length": 32768, "available": false,
						"reason": "chat_template_unavailable",
					}},
					"engines":             []any{},
					"model_requests_sent": 0,
				},
			})
			return inferyard.Execution{ExitCode: 0, Stdout: payload}, nil
		}
		if args[0] == "device-check" {
			mr := flagVal("--models-root")
			mu.Lock()
			deviceRoots = append(deviceRoots, mr)
			mu.Unlock()
			payload, _ := json.Marshal(map[string]any{
				"command":      "device-check",
				"status":       "inspected",
				"completeness": "complete",
				"limitations":  []string{},
				"details": map[string]any{
					"kind":                  "device_preflight.v1",
					"hardware":              map[string]any{},
					"models":                []any{},
					"recommendation":        map[string]any{},
					"platform_capabilities": map[string]any{},
				},
			})
			return inferyard.Execution{ExitCode: 0, Stdout: payload}, nil
		}
		return inferyard.Execution{}, nil
	}

	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	raw, _ := json.Marshal(UserSettings{InferyardPath: ptr(os.Args[0]), ModelsRoot: &root1})
	if err := os.WriteFile(settingsPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := New(ctx, Options{SettingsPath: settingsPath, Execute: execFn})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Shutdown(context.Background())

	select {
	case <-firstEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("initial RefreshAssets did not start")
	}

	// Change models_root while first scan is still in flight; stale snapshot must be ignored.
	_, unsubscribe, cursor := s.Bus.Subscribe()
	defer unsubscribe()
	patchRoot2, _ := json.Marshal(root2)
	if _, apiErr := s.UpdateSettings(map[string]json.RawMessage{"models_root": patchRoot2}); apiErr != nil {
		t.Fatal(apiErr)
	}
	close(firstRelease)

	wait(t, func() bool {
		a := s.Assets()
		return !a.AssetsRefreshing && a.Parsed != nil && len(a.Parsed.Models) == 1 && a.Parsed.Models[0].Path == m2
	})

	reg := s.Registry()
	if len(reg.Models) != 1 {
		t.Fatalf("unexpected models count: %+v", reg.Models)
	}
	got := reg.Models[0]
	if got.Format != "directory" || got.Architecture != "qwen2" || got.SizeBytes == nil || *got.SizeBytes != 8192 ||
		got.ContextLength == nil || *got.ContextLength != 32768 || got.Available || got.Reason != "chat_template_unavailable" {
		t.Fatalf("unexpected mapped model: %+v", got)
	}

	events, _, _ := s.Bus.After(cursor)
	sawAssetsUpdated := false
	for _, ev := range events {
		if ev.Type == "assets.updated" {
			sawAssetsUpdated = true
		}
	}
	if !sawAssetsUpdated {
		t.Fatal("expected assets.updated event on bus")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(assetRoots) < 2 || len(deviceRoots) < 2 {
		t.Fatalf("expected both asset and device refreshes on New and UpdateSettings: assets=%v device=%v", assetRoots, deviceRoots)
	}
}

func TestProvisionTriggersAssetAndDeviceRefresh(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	var mu sync.Mutex
	var sawAssets, sawDevice bool
	s := testService(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			return v, nil
		}
		mu.Lock()
		if len(args) >= 2 && args[0] == "assets" && args[1] == "scan" {
			sawAssets = true
			mu.Unlock()
			return inferyard.Execution{Stdout: []byte(`{"command":"assets scan","status":"scanned","completeness":"complete","run_id":null,"evidence_dir":null,"limitations":[],"details":{"definition":"asset-scan.v1","models":[],"engines":[],"model_requests_sent":0}}`)}, nil
		}
		if args[0] == "device-check" {
			sawDevice = true
			mu.Unlock()
			return inferyard.Execution{Stdout: []byte(`{"command":"device-check","status":"inspected","completeness":"complete","limitations":[],"details":{"kind":"device_preflight.v1","hardware":{},"models":[],"recommendation":{},"platform_capabilities":{}}}`)}, nil
		}
		mu.Unlock()
		return inferyard.Execution{}, nil
	})
	defer s.Shutdown(context.Background())

	if _, apiErr := s.Provision(); apiErr != nil {
		t.Fatal(apiErr)
	}
	wait(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return sawAssets && sawDevice
	})
}

func TestOutdatedCLIFallsBackToDeviceSnapshotAndDirectoryScan(t *testing.T) {
	modelsRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dirModel := filepath.Join(modelsRoot, "dir-qwen")
	if err := os.MkdirAll(dirModel, 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"config.json":       "{}",
		"tokenizer.json":    "{}",
		"model.safetensors": "weights",
	} {
		if err := os.WriteFile(filepath.Join(dirModel, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	ggufNoTpl := filepath.Join(modelsRoot, "no-tpl.gguf")
	if err := os.WriteFile(ggufNoTpl, []byte("gguf"), 0644); err != nil {
		t.Fatal(err)
	}

	s := testService(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			return v, nil
		}
		if len(args) >= 2 && args[0] == "assets" && args[1] == "scan" {
			// Legacy CLI exits 2 without asset-scan.v1.
			return inferyard.Execution{
				ExitCode: 2,
				Stdout:   []byte(`{"command":"unknown","status":"blocked","completeness":"incomplete","limitations":["invalid_input"]}`),
			}, nil
		}
		if args[0] == "device-check" {
			payload, _ := json.Marshal(map[string]any{
				"command":      "device-check",
				"status":       "inspected",
				"completeness": "complete",
				"limitations":  []string{},
				"details": map[string]any{
					"kind":                  "device_preflight.v1",
					"hardware":              map[string]any{},
					"recommendation":        map[string]any{},
					"platform_capabilities": map[string]any{},
					"models": []map[string]any{{
						"path":                    ggufNoTpl,
						"size_bytes":              4,
						"status":                  "available",
						"chat_template_available": false,
					}},
				},
			})
			return inferyard.Execution{ExitCode: 0, Stdout: payload}, nil
		}
		return inferyard.Execution{}, nil
	})
	defer s.Shutdown(context.Background())

	patch, _ := json.Marshal(modelsRoot)
	if _, apiErr := s.UpdateSettings(map[string]json.RawMessage{"models_root": patch}); apiErr != nil {
		t.Fatal(apiErr)
	}

	wait(t, func() bool {
		return !s.Registry().Refreshing && len(s.Registry().Models) == 2
	})

	reg := s.Registry()
	if reg.ModelError != nil {
		t.Fatalf("outdated CLI leaked loud error: %+v", reg.ModelError)
	}
	byName := map[string]ModelAsset{}
	for _, m := range reg.Models {
		byName[m.Name] = m
	}
	if m, ok := byName["dir-qwen"]; !ok || m.Format != "directory" || !m.Available {
		t.Fatalf("missing fallback directory model: %+v", byName)
	}
	if m, ok := byName["no-tpl.gguf"]; !ok || m.Format != "gguf" || m.Available || m.Reason != "chat_template_unavailable" {
		t.Fatalf("fallback GGUF chat_template_available=false was not mapped: %+v", byName)
	}
}

func TestScanDirectoryModelsAllowsHFCacheSymlinksAndRejectsUnsafeSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlinks test")
	}
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// 1. HF snapshot root where files symlink into ../../blobs/<digest>.
	repo := filepath.Join(tmp, "hub", "models--org--demo")
	blobs := filepath.Join(repo, "blobs")
	snap := filepath.Join(repo, "snapshots", "rev1")
	if err := os.MkdirAll(blobs, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(snap, 0755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"b1": "{}", "b2": "{}", "b3": "weights"} {
		if err := os.WriteFile(filepath.Join(blobs, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("../../blobs/b1", filepath.Join(snap, "config.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../blobs/b2", filepath.Join(snap, "tokenizer.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../blobs/b3", filepath.Join(snap, "model.safetensors")); err != nil {
		t.Fatal(err)
	}

	entries, apiErr := scanDirectoryModels(snap)
	if apiErr != nil || len(entries) != 1 || entries[0].path != snap {
		t.Fatalf("expected HF snapshot symlink model, got entries=%+v err=%+v", entries, apiErr)
	}

	// 2. Escaping symlink outside canonical ancestor/HF cache must be rejected.
	outside := filepath.Join(tmp, "outside")
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "evil.safetensors"), []byte("evil"), 0644); err != nil {
		t.Fatal(err)
	}
	escapeRoot := filepath.Join(tmp, "escape-models")
	escapeDir := filepath.Join(escapeRoot, "m1")
	if err := os.MkdirAll(escapeDir, 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(escapeDir, "config.json"), []byte("{}"), 0644)
	_ = os.WriteFile(filepath.Join(escapeDir, "tokenizer.json"), []byte("{}"), 0644)
	_ = os.Symlink(filepath.Join(outside, "evil.safetensors"), filepath.Join(escapeDir, "model.safetensors"))

	entries, _ = scanDirectoryModels(escapeRoot)
	if len(entries) != 0 {
		t.Fatalf("escaping symlink was accepted: %+v", entries)
	}

	// 3. Symlink to directory and symlink loop must be rejected.
	loopRoot := filepath.Join(tmp, "loop-models")
	loopDir := filepath.Join(loopRoot, "m2")
	if err := os.MkdirAll(loopDir, 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(loopDir, "config.json"), []byte("{}"), 0644)
	_ = os.WriteFile(filepath.Join(loopDir, "tokenizer.json"), []byte("{}"), 0644)
	_ = os.WriteFile(filepath.Join(loopDir, "model.safetensors"), []byte("w"), 0644)
	_ = os.Symlink("loop", filepath.Join(loopDir, "loop"))
	entries, _ = scanDirectoryModels(loopRoot)
	if len(entries) != 0 {
		t.Fatalf("directory with symlink loop was accepted: %+v", entries)
	}

	dirLinkRoot := filepath.Join(tmp, "dirlink-models")
	dirLinkModel := filepath.Join(dirLinkRoot, "m3")
	if err := os.MkdirAll(dirLinkModel, 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dirLinkModel, "config.json"), []byte("{}"), 0644)
	_ = os.WriteFile(filepath.Join(dirLinkModel, "tokenizer.json"), []byte("{}"), 0644)
	_ = os.WriteFile(filepath.Join(dirLinkModel, "model.safetensors"), []byte("w"), 0644)
	_ = os.Symlink(outside, filepath.Join(dirLinkModel, "dir_link"))
	entries, _ = scanDirectoryModels(dirLinkRoot)
	if len(entries) != 0 {
		t.Fatalf("directory with dir symlink was accepted: %+v", entries)
	}

	// 4. Symlink to FIFO (non-regular) must be rejected, while symlink into HF_HUB_CACHE is allowed.
	fifoPath := filepath.Join(tmp, "pipe")
	if err := syscall.Mkfifo(fifoPath, 0600); err == nil {
		fifoRoot := filepath.Join(tmp, "fifo-models")
		fifoModel := filepath.Join(fifoRoot, "m4")
		_ = os.MkdirAll(fifoModel, 0755)
		_ = os.WriteFile(filepath.Join(fifoModel, "config.json"), []byte("{}"), 0644)
		_ = os.WriteFile(filepath.Join(fifoModel, "tokenizer.json"), []byte("{}"), 0644)
		_ = os.Symlink(fifoPath, filepath.Join(fifoModel, "model.safetensors"))
		entries, _ = scanDirectoryModels(fifoRoot)
		if len(entries) != 0 {
			t.Fatalf("directory with FIFO symlink was accepted: %+v", entries)
		}
	}

	t.Setenv("HF_HUB_CACHE", filepath.Join(tmp, "hub"))
	linkedRoot := filepath.Join(tmp, "linked-models")
	linkedModel := filepath.Join(linkedRoot, "hf-linked")
	if err := os.MkdirAll(linkedModel, 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.Symlink(filepath.Join(blobs, "b1"), filepath.Join(linkedModel, "config.json"))
	_ = os.Symlink(filepath.Join(blobs, "b2"), filepath.Join(linkedModel, "tokenizer.json"))
	_ = os.Symlink(filepath.Join(blobs, "b3"), filepath.Join(linkedModel, "model.safetensors"))
	entries, apiErr = scanDirectoryModels(linkedRoot)
	if apiErr != nil || len(entries) != 1 || entries[0].path != linkedModel {
		t.Fatalf("expected HF_HUB_CACHE symlink model, got entries=%+v err=%+v", entries, apiErr)
	}
}

func TestRefreshAssetsConflictsAndErrors(t *testing.T) {
	sNoRoot := testService(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			return v, nil
		}
		return inferyard.Execution{}, nil
	})
	defer sNoRoot.Shutdown(context.Background())
	if apiErr := sNoRoot.RefreshAssets(); apiErr == nil || apiErr.Code != "conflict" || apiErr.Details["reason"] != "settings_incomplete" {
		t.Fatalf("expected settings_incomplete conflict, got %+v", apiErr)
	}

	gate := make(chan struct{})
	entered := make(chan struct{}, 1)
	failMode := false
	var mu sync.Mutex
	s := testService(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			return v, nil
		}
		if len(args) >= 2 && args[0] == "assets" && args[1] == "scan" {
			mu.Lock()
			fail := failMode
			mu.Unlock()
			if fail {
				return inferyard.Execution{
					ExitCode: 4,
					Stdout:   []byte(`{"command":"assets scan","status":"error","completeness":"incomplete","limitations":["io_error"]}`),
				}, nil
			}
			select {
			case entered <- struct{}{}:
			default:
			}
			<-gate
			return inferyard.Execution{Stdout: []byte(`{"command":"assets scan","status":"scanned","completeness":"complete","limitations":[],"details":{"definition":"asset-scan.v1","models":[],"engines":[],"model_requests_sent":0}}`)}, nil
		}
		return inferyard.Execution{Stdout: []byte(`{"command":"device-check","status":"inspected","completeness":"complete","limitations":[],"details":{"kind":"device_preflight.v1","hardware":{},"models":[],"recommendation":{},"platform_capabilities":{}}}`)}, nil
	})
	defer s.Shutdown(context.Background())

	s.mu.Lock()
	s.settings.ModelsRoot = ptr(t.TempDir())
	s.mu.Unlock()

	if apiErr := s.RefreshAssets(); apiErr != nil {
		t.Fatalf("first RefreshAssets failed: %+v", apiErr)
	}
	<-entered
	if apiErr := s.RefreshAssets(); apiErr == nil || apiErr.Code != "conflict" || apiErr.Details["reason"] != "refresh_in_progress" {
		t.Fatalf("expected refresh_in_progress conflict, got %+v", apiErr)
	}
	close(gate)
	wait(t, func() bool { return !s.Assets().AssetsRefreshing })

	mu.Lock()
	failMode = true
	mu.Unlock()
	if apiErr := s.RefreshAssets(); apiErr != nil {
		t.Fatalf("second RefreshAssets failed: %+v", apiErr)
	}
	wait(t, func() bool { return !s.Assets().AssetsRefreshing && s.Assets().AssetsError != nil })
	reg := s.Registry()
	if reg.ModelError == nil || reg.ModelError.Code != "cli_error" {
		t.Fatalf("expected cli_error in Registry().ModelError, got %+v", reg.ModelError)
	}
}
