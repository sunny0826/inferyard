package pages

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

// This CLI fake freezes the model from the supplied preflight, never from the
// selected UI name. WizardExecute and the real resource registry are exercised.
func TestDashboardPreparationFreezesSelectedModel(t *testing.T) {
	for _, selected := range []string{"B.gguf", ""} {
		t.Run("selected_"+selected, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			write := func(path string, data []byte) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0755); err != nil {
					t.Fatal(err)
				}
			}
			a, b := filepath.Join(root, "models", "A.gguf"), filepath.Join(root, "models", "B.gguf")
			write(a, []byte("model"))
			write(b, []byte("model"))
			engine := filepath.Join(root, "engines", "release", "engine", "llama-server.exe")
			write(engine, []byte("engine"))
			receipt := filepath.Join(root, "engines", "release", "runtime-receipt.json")
			write(receipt, []byte(`{"fixture":true}`))
			preflight := filepath.Join(root, "preflight", "old", "device-preflight.json")
			report := func(model string) []byte {
				return []byte(fmt.Sprintf(`{"recommendation":{"status":"recommended","selected":{"model_path":%q}}}`, model))
			}
			write(preflight, report(a))
			old := preflight
			command := "inferyard config create --workspace " + quote(root) + " --bundle zh-core --preflight " + quote(old) + " --engine placeholder --out " + quote(filepath.Join(root, "candidates", "core-1"))
			progress := inferyard.WorkspaceProgress{Kind: "progress", ProgressVersion: 1, Workspace: root, NextAction: "prepare_candidate",
				DevicePreflight: inferyard.ProgressPreflight{Status: "ok", Path: &preflight}, Report: inferyard.ProgressReport{Status: "missing", Runs: []string{}},
				Bundles: []inferyard.BundleProgress{{Bundle: "zh-core", Phase: "candidate_missing", NextAction: "prepare_candidate", NextCommand: &command, Runs: []inferyard.ProgressRun{}}}}
			var checkedModel, frozenModel, usedPreflight string
			firstCheck := false
			flag := func(args []string, name string) string {
				for i := 0; i+1 < len(args); i++ {
					if args[i] == name {
						return args[i+1]
					}
				}
				return ""
			}
			execute := func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
				switch args[0] {
				case "--version":
					return inferyard.Execution{Stdout: []byte("inferyard fixture source=release")}, nil
				case "progress":
					raw, _ := json.Marshal(progress)
					return inferyard.Execution{Stdout: raw}, nil
				case "device-check":
					if flag(args, "--models-root") != filepath.Dir(a) {
						return inferyard.Execution{}, fmt.Errorf("missing models root: %v", args)
					}
					out := flag(args, "--out")
					if out == "" {
						raw := []byte(fmt.Sprintf(`{"command":"device-check","status":"inspected","completeness":"complete","limitations":[],"details":{"kind":"device_preflight.v1","hardware":{},"platform_capabilities":{},"recommendation":{"status":"recommended"},"models":[{"path":%q,"status":"available"},{"path":%q,"status":"available"}]}}`, a, b))
						return inferyard.Execution{Stdout: raw}, nil
					}
					if flag(args, "--model") == "" {
						firstCheck = true
						preflight = filepath.Join(out, "device-preflight.json")
						write(preflight, report(a))
						return inferyard.Execution{}, nil
					}
					checkedModel = flag(args, "--model")
					if checkedModel != b {
						return inferyard.Execution{ExitCode: 2}, nil
					}
					preflight = filepath.Join(out, "device-preflight.json")
					write(preflight, report(checkedModel))
					return inferyard.Execution{}, nil
				case "config":
					if args[1] == "describe" {
						return legacyCandidateResponse(frozenModel, engine), nil
					}
					if runtime.GOOS == "windows" {
						if flag(args, "--engine") != "" || flag(args, "--runtime-receipt") != receipt {
							return inferyard.Execution{ExitCode: 2}, nil
						}
					} else if flag(args, "--engine") != engine {
						return inferyard.Execution{ExitCode: 2}, nil
					}
					usedPreflight = flag(args, "--preflight")
					raw, err := os.ReadFile(usedPreflight)
					if err != nil {
						return inferyard.Execution{ExitCode: 2}, nil
					}
					var doc struct {
						Recommendation struct {
							Selected struct {
								ModelPath string `json:"model_path"`
							}
						}
					}
					if json.Unmarshal(raw, &doc) != nil || doc.Recommendation.Selected.ModelPath == "" {
						return inferyard.Execution{ExitCode: 2}, nil
					}
					frozenModel = doc.Recommendation.Selected.ModelPath
					out := flag(args, "--out")
					write(filepath.Join(out, "candidate.toml"), []byte(fmt.Sprintf("[model]\nlocal_path = %q\n[engine]\nbinary_path = %q\n", frozenModel, engine)))
					progress.Bundles[0].CandidatePath = &out
					progress.Bundles[0].Phase = "bound_missing"
					progress.Bundles[0].NextAction = "bind"
					return inferyard.Execution{}, nil
				}
				return inferyard.Execution{}, fmt.Errorf("unexpected command: %v", args)
			}
			binary, _ := os.Executable()
			models := filepath.Dir(a)
			raw, _ := json.Marshal(core.UserSettings{InferyardPath: &binary, WorkspaceRoot: &root, ModelsRoot: &models})
			settings := filepath.Join(root, "settings.json")
			write(settings, raw)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			service, err := core.New(ctx, core.Options{SettingsPath: settings, Execute: execute})
			if err != nil {
				t.Fatal(err)
			}
			defer service.Shutdown(context.Background())
			deadline := time.Now().Add(5 * time.Second)
			for service.Registry().Refreshing && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			var engineName string
			for _, e := range service.Registry().Engines {
				if e.Name == "engine · llama-server.exe" {
					engineName = e.Name
				}
			}
			if engineName == "" {
				t.Fatal(service.Registry())
			}
			initial, e := service.WizardExecute(ctx, core.WizardRequest{Command: commandOf([]string{"device-check", "--json", "--out", filepath.Join(root, "preflight", "initial")}), ModelName: ""})
			if e != nil || initial.ExitCode != 0 || !firstCheck {
				t.Fatal(initial, e, firstCheck)
			}
			result, e := prepareCombination(ctx, service, selected, engineName)
			if e != nil || result.candidate == "" {
				t.Fatalf("%+v %v", result, e)
			}
			want := b
			if selected == "" {
				want = a
			}
			if frozenModel != want {
				t.Fatalf("frozen %s, want %s", frozenModel, want)
			}
			if selected != "" && (checkedModel != b || usedPreflight == old || usedPreflight != preflight) {
				t.Fatalf("checked=%s preflight=%s", checkedModel, usedPreflight)
			}
		})
	}
}

func legacyCandidateResponse(model, engine string) inferyard.Execution {
	hash := strings.Repeat("a", 64)
	details := map[string]any{"schema_version": 3, "definition": "engine-description.v1", "adapter": "llama_cpp_b11146_v1", "model_requests_sent": 0,
		"launch":   map[string]any{"kind": "direct_process", "argv": []string{engine}, "working_directory": nil, "environment": nil, "ownership": "operator"},
		"assets":   map[string]any{"engine": map[string]any{"path": engine, "sha256": hash}, "model": map[string]any{"path": model, "kind": "gguf", "content_sha256": hash}, "template": map[string]any{"path": "/fixture/template.jinja", "sha256": hash}},
		"endpoint": map[string]any{"url": "http://127.0.0.1:48857", "server_pid": 1, "process_start_ticks": 1}, "conditions": map[string]any{"context_size": 4096}, "generation": map[string]any{"max_tokens": 32, "temperature": 0}}
	raw, _ := json.Marshal(map[string]any{"command": "config describe", "status": "prepared", "completeness": "complete", "run_id": nil, "evidence_dir": nil, "limitations": []string{}, "details": details})
	return inferyard.Execution{Stdout: raw}
}
