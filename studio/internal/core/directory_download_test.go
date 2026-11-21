package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"inferyard/studio/internal/inferyard"
)

func TestDirectoryCatalogEntryAndDownloadArgs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	mlxEntry, ok := CatalogEntryByID("model-qwen3-0.6b-mlx")
	if !ok {
		t.Fatal("missing catalog entry model-qwen3-0.6b-mlx")
	}
	if mlxEntry.Format != "directory" || mlxEntry.Platform != "darwin/arm64" || !mlxEntry.Downloadable() {
		t.Fatalf("unexpected mlx directory catalog entry: %+v", mlxEntry)
	}
	if got := SuggestDownloadSlug("model", mlxEntry.URL); got != "Qwen3-0.6B-4bit" {
		t.Fatalf("SuggestDownloadSlug = %q, want Qwen3-0.6B-4bit", got)
	}
	if got := ResolveDownloadDisplayName("model", mlxEntry.URL); got != mlxEntry.DisplayName() {
		t.Fatalf("ResolveDownloadDisplayName = %q, want %q", got, mlxEntry.DisplayName())
	}

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(root, "workspace")
	results := filepath.Join(root, "results")
	models := filepath.Join(root, "models")
	for _, dir := range []string{workspace, results, models} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	settingsPath := filepath.Join(root, "settings.json")
	raw, _ := json.Marshal(UserSettings{
		InferyardPath: ptr(os.Args[0]),
		WorkspaceRoot: &workspace,
		ResultsRoot:   &results,
		ModelsRoot:    &models,
	})
	if err := os.WriteFile(settingsPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var recorded [][]string
	svc, apiErr := New(context.Background(), Options{
		SettingsPath:  settingsPath,
		HostStatePath: filepath.Join(root, "host.json"),
		Execute: func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
			if len(args) > 0 && args[0] == "--version" {
				return inferyard.Execution{Stdout: []byte("inferyard 1.0 source=release\n")}, nil
			}
			if len(args) >= 2 && args[0] == "assets" && args[1] == "scan" {
				payload, _ := json.Marshal(map[string]any{
					"command":      "assets scan",
					"status":       "scanned",
					"completeness": "complete",
					"limitations":  []string{},
					"details": inferyard.AssetScan{
						Definition: inferyard.AssetScanDefinition,
						Models: []inferyard.ScannedModel{{
							Path:      filepath.Join(models, "Big-00001-of-00003.gguf"),
							Name:      "Big-Sharded",
							Format:    "gguf",
							SizeBytes: ptr(int64(3 << 30)),
							Available: true,
						}},
						Engines: []inferyard.ScannedEngine{},
					},
				})
				return inferyard.Execution{Stdout: payload}, nil
			}
			return inferyard.Execution{}, nil
		},
		ExecStream: func(_ context.Context, _ func(string), _ string, args ...string) (inferyard.Execution, error) {
			mu.Lock()
			recorded = append(recorded, append([]string(nil), args...))
			mu.Unlock()
			return inferyard.Execution{
				Stdout: []byte(`{"command":"model acquire","status":"prepared","completeness":"complete","limitations":[],"details":{"out":"/tmp/mlx","model":"/tmp/mlx","kind":"directory","files":3,"bytes":335450584,"sha256":"73e3e38d981303bc594367cd910ea6eb48349da8000000000000000000000000","ready_to_run":false,"model_requests_sent":0}}`),
			}, nil
		},
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	task, errDL := svc.CreateModelDownload(CreateDownloadRequest{SourceUrl: mlxEntry.URL}, "")
	if errDL != nil {
		t.Fatalf("CreateModelDownload(directory) failed: %+v", errDL)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var state string
		for _, item := range svc.Downloads() {
			if item.TaskId == task.TaskId {
				state = item.State
			}
		}
		if state == "done" {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(recorded) == 0 || !slices.Contains(recorded[0], "--kind") || !slices.Contains(recorded[0], "directory") {
		t.Fatalf("expected --kind directory in CLI args, got %v", recorded)
	}

	regDeadline := time.Now().Add(3 * time.Second)
	var reg Registry
	for time.Now().Before(regDeadline) {
		reg = svc.Registry()
		if !reg.Refreshing && len(reg.Models) == 1 {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if len(reg.Models) != 1 || reg.Models[0].Name != "Big-00001-of-00003.gguf" || reg.Models[0].Format != "gguf" || reg.Models[0].SizeBytes == nil || *reg.Models[0].SizeBytes != 3<<30 {
		t.Fatalf("expected grouped multi-shard model in registry, got %+v", reg.Models)
	}
}
