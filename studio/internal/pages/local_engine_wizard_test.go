package pages

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

type wizardPreviewStub struct {
	intents []previewIntent
	notices int
}

func (p *wizardPreviewStub) showNotice()               { p.notices++ }
func (p *wizardPreviewStub) dispatch(op previewIntent) { p.intents = append(p.intents, op) }
func (p *wizardPreviewStub) resetTask(string, string)  {}
func (p *wizardPreviewStub) report(string) (*core.ReportIndex, *core.ApiError) {
	return nil, nil
}
func (p *wizardPreviewStub) detail(string) *core.RunTaskDetail { return nil }
func (p *wizardPreviewStub) resolveModelURL(string) inferyard.ModelResolve {
	return inferyard.ModelResolve{}
}
func (p *wizardPreviewStub) downloadLogTail(string) string { return "" }
func (p *wizardPreviewStub) detectedEngines() []inferyard.DetectedEngine {
	lmsModels := "/Users/demo/.lmstudio/models"
	return []inferyard.DetectedEngine{
		{
			Family:           "llama-cpp",
			Executable:       "/opt/homebrew/bin/llama-server",
			WorkingDirectory: "/opt/homebrew/bin",
			RuntimeRoots:     []string{"/opt/homebrew/bin"},
			Label:            "llama-cpp · /opt/homebrew/bin/llama-server",
		},
		{
			Family:           "mlx-lm",
			Executable:       "/Users/demo/.local/share/uv/tools/mlx-lm/bin/python",
			WorkingDirectory: "/Users/demo/.local/share/uv/tools/mlx-lm",
			RuntimeRoots:     []string{"/Users/demo/.local/share/uv/tools/mlx-lm"},
			Label:            "mlx-lm · mlx-lm (uv tool)",
		},
		{
			Family:           "lmstudio",
			Executable:       "/Users/demo/.lmstudio/bin/lms",
			WorkingDirectory: "/Users/demo/.lmstudio/bin",
			RuntimeRoots:     []string{"/Users/demo/.lmstudio/bin"},
			ModelsRoot:       &lmsModels,
			Label:            "lmstudio · /Users/demo/.lmstudio/bin/lms",
		},
	}
}

func newWizardTestService(t *testing.T, execute core.Executor) (*core.Service, string) {
	t.Helper()
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(root, "settings.json")
	raw, _ := json.Marshal(core.UserSettings{
		InferyardPath: ptr(os.Args[0]),
		WorkspaceRoot: ptr(workspace),
	})
	if err := os.WriteFile(settingsPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s, err := core.New(ctx, core.Options{
		SettingsPath:  settingsPath,
		Execute:       execute,
		HostStatePath: filepath.Join(root, "host.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Shutdown(context.Background()) })
	return s, workspace
}

func TestCoreDetectAndInitRegisterLocalEngine(t *testing.T) {
	var seenCommands [][]string
	svc, workspace := newWizardTestService(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if len(args) > 0 && args[0] == "--version" {
			return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 1.0 source=release\n")}, nil
		}
		seenCommands = append(seenCommands, slices.Clone(args))
		if len(args) >= 2 && args[0] == "engine" && args[1] == "detect" {
			payload, _ := json.Marshal(map[string]any{
				"command":      "engine detect",
				"status":       "detected",
				"completeness": "complete",
				"run_id":       nil,
				"evidence_dir": nil,
				"limitations":  []string{},
				"details": map[string]any{
					"definition": "local-engine-detect.v1",
					"candidates": []map[string]any{{
						"family":            "llama-cpp",
						"executable":        "/opt/homebrew/bin/llama-server",
						"working_directory": "/opt/homebrew/bin",
						"runtime_roots":     []string{"/opt/homebrew/bin"},
						"models_root":       nil,
						"label":             "llama-cpp · /opt/homebrew/bin/llama-server",
					}},
					"model_requests_sent": 0,
				},
			})
			return inferyard.Execution{ExitCode: 0, Stdout: payload}, nil
		}
		if len(args) >= 2 && args[0] == "engine" && args[1] == "init-installation" {
			var family, exe, outPath string
			for i := 0; i+1 < len(args); i++ {
				switch args[i] {
				case "--family":
					family = args[i+1]
				case "--executable":
					exe = args[i+1]
				case "--out":
					outPath = args[i+1]
				}
			}
			decl := map[string]any{
				"schema_version":    3,
				"definition":        "local-engine-installation.v1",
				"family":            family,
				"executable":        exe,
				"working_directory": filepath.Dir(exe),
				"runtime_roots":     []string{filepath.Dir(exe)},
				"environment":       map[string]string{},
			}
			rawDecl, _ := json.Marshal(decl)
			if err := os.WriteFile(outPath, rawDecl, 0600); err != nil {
				t.Fatal(err)
			}
			payload, _ := json.Marshal(map[string]any{
				"command":      "engine init-installation",
				"status":       "prepared",
				"completeness": "complete",
				"run_id":       nil,
				"evidence_dir": nil,
				"limitations":  []string{},
				"details": map[string]any{
					"definition":          "local-engine-init.v1",
					"out":                 outPath,
					"family":              family,
					"installation":        decl,
					"verified":            true,
					"model_requests_sent": 0,
				},
			})
			return inferyard.Execution{ExitCode: 0, Stdout: payload}, nil
		}
		if len(args) >= 3 && args[0] == "engine" && args[1] == "check" && args[2] == "--installation" {
			payload, _ := json.Marshal(map[string]any{
				"command":      "engine check",
				"status":       "checked",
				"completeness": "complete",
				"run_id":       nil,
				"evidence_dir": nil,
				"limitations":  []string{},
				"details": map[string]any{
					"definition":          "local-engine-check.v2",
					"eligible":            true,
					"reason":              nil,
					"family":              "llama-cpp",
					"declaration_sha256":  strings.Repeat("a", 64),
					"verification_scope":  "installation_runtime_closure_only",
					"model_requests_sent": 0,
					"ready_to_run":        false,
				},
			})
			return inferyard.Execution{ExitCode: 0, Stdout: payload}, nil
		}
		return inferyard.Execution{}, nil
	})

	detected, apiErr := svc.DetectLocalEngines(context.Background())
	if apiErr != nil {
		t.Fatalf("DetectLocalEngines unexpected error: %+v", apiErr)
	}
	if len(detected.Candidates) != 1 || detected.Candidates[0].Family != "llama-cpp" {
		t.Fatalf("unexpected candidates: %+v", detected.Candidates)
	}

	// Reject lmstudio without models_root.
	if _, err := svc.InitAndRegisterLocalEngine(context.Background(), core.InitLocalEngineRequest{
		Family:     "lmstudio",
		Executable: "/Users/demo/.lmstudio/bin/lms",
	}); err == nil || err.Code != "local_engine_models_root_required_for_lmstudio_only" {
		t.Fatalf("expected lmstudio models_root error, got %+v", err)
	}

	settings, apiErr := svc.InitAndRegisterLocalEngine(context.Background(), core.InitLocalEngineRequest{
		Family:     "llama-cpp",
		Executable: "/opt/homebrew/bin/llama-server",
	})
	if apiErr != nil {
		t.Fatalf("InitAndRegisterLocalEngine unexpected error: %+v", apiErr)
	}
	if len(settings.Settings.EngineInstallations) != 1 {
		t.Fatalf("expected 1 registered installation, got %+v", settings.Settings.EngineInstallations)
	}
	firstOut := settings.Settings.EngineInstallations[0]
	if !strings.HasPrefix(filepath.Base(firstOut), "llama-cpp-") || !strings.HasSuffix(firstOut, ".json") {
		t.Fatalf("unexpected output path %q", firstOut)
	}
	if !strings.Contains(firstOut, filepath.Join(filepath.Base(workspace), "engine-installations")) {
		t.Fatalf("expected file inside workspace/engine-installations, got %q", firstOut)
	}

	// Second call must pick a unique non-overwriting path (-2.json).
	settings2, apiErr := svc.InitAndRegisterLocalEngine(context.Background(), core.InitLocalEngineRequest{
		Family:     "llama-cpp",
		Executable: "/opt/homebrew/bin/llama-server",
	})
	if apiErr != nil {
		t.Fatalf("second InitAndRegisterLocalEngine failed: %+v", apiErr)
	}
	if len(settings2.Settings.EngineInstallations) != 2 || settings2.Settings.EngineInstallations[0] == settings2.Settings.EngineInstallations[1] {
		t.Fatalf("expected 2 distinct registered paths, got %+v", settings2.Settings.EngineInstallations)
	}
	_ = seenCommands
}

func TestWizardPreviewSafeActionsDoNotPerformIO(t *testing.T) {
	s := settingsState()
	preview := &wizardPreviewStub{}
	s.preview = preview
	s.service = nil

	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Children(func() { s.localInstallationsView(c) })
	}, 1120, 1400)
	tt.Frame()

	click(t, tt, "自动探测本机引擎")
	tt.Frame()
	if !tt.HasText("mlx-lm · mlx-lm (uv tool)") {
		t.Fatalf("preview detect did not show mock candidates: %v", tt.Texts())
	}
	click(t, tt, "mlx-lm · mlx-lm (uv tool)")
	tt.Frame()
	click(t, tt, "生成并注册引擎")
	tt.Frame()

	if len(preview.intents) != 1 {
		t.Fatalf("expected 1 preview intent, got %+v", preview.intents)
	}
	inst, ok := preview.intents[0].(previewInstallation)
	if !ok || inst.action != "register" || !strings.Contains(inst.path, "mlx-lm") {
		t.Fatalf("unexpected preview intent: %+v", preview.intents[0])
	}
	if preview.notices != 0 || s.failure != nil {
		t.Fatalf("preview wizard triggered unexpected error/notice: notices=%d failure=%+v", preview.notices, s.failure)
	}

	// Test browse buttons in preview mode stay in memory and do not dispatch unsupported browse intents.
	click(t, tt, "浏览… · 虚拟环境 Python 路径")
	tt.Frame()
	if len(preview.intents) != 1 || s.failure != nil {
		t.Fatalf("preview executable browse failed: intents=%+v failure=%+v", preview.intents, s.failure)
	}
}
