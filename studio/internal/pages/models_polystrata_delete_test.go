package pages

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func newModelsPolystrataDeleteService(t *testing.T) (*core.Service, string, string, string, string) {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	resultsRoot := filepath.Join(base, "results")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	managedDir := filepath.Join(ws, "engines", "polystrata-ui")
	extDir := filepath.Join(base, "external-ps")
	for _, dir := range []string{
		resultsRoot,
		filepath.Join(managedDir, "engine-poly"),
		filepath.Join(extDir, "engine-poly"),
		filepath.Join(dataDir, "packs", "demo"),
		filepath.Join(dataDir, "models", "other"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	_ = os.WriteFile(filepath.Join(managedDir, "payload.bin"), bytes.Repeat([]byte("e"), 1<<20), 0o600)
	_ = os.WriteFile(filepath.Join(managedDir, "engine-poly", "BUILD.json"), []byte(`{"version":"v0.1.45-poly.10"}`), 0o600)
	_ = os.WriteFile(filepath.Join(managedDir, ".inferyard-runtime.json"), []byte(`{"definition":"polystrata-runtime.v1"}`), 0o600)
	managedInst := filepath.Join(managedDir, "installation.json")
	managedRaw, _ := json.Marshal(map[string]any{
		"schema_version": 3, "definition": "local-engine-installation.v1",
		"family": "polystrata", "working_directory": managedDir, "data_dir": dataDir,
	})
	_ = os.WriteFile(managedInst, managedRaw, 0o600)

	_ = os.WriteFile(filepath.Join(extDir, "engine-poly", "BUILD.json"), []byte(`{"version":"v0.1.44-ext"}`), 0o600)
	extInst := filepath.Join(extDir, "installation.json")
	extRaw, _ := json.Marshal(map[string]any{
		"schema_version": 3, "definition": "local-engine-installation.v1",
		"family": "polystrata", "working_directory": extDir, "data_dir": dataDir,
	})
	_ = os.WriteFile(extInst, extRaw, 0o600)

	sharedWeights := filepath.Join(dataDir, "packs", "demo", "weights.bin")
	_ = os.WriteFile(sharedWeights, bytes.Repeat([]byte("w"), 2<<20), 0o600)
	cfgSharedPath := filepath.Join(managedDir, "strata-demo.json")
	cfgSharedRaw, _ := json.Marshal(map[string]any{
		"exe": "/opt/ps/strata", "args": []string{"--pack", filepath.Join(dataDir, "packs", "demo")},
		"model_name": "Qwen3.6 Shared",
	})
	_ = os.WriteFile(cfgSharedPath, cfgSharedRaw, 0o600)

	otherCfgPath := filepath.Join(dataDir, "models", "other", "strata-other.json")
	otherRaw, _ := json.Marshal(map[string]any{
		"exe": "/opt/ps/strata", "args": []string{"--pack", filepath.Join(dataDir, "packs", "demo")},
		"model_name": "Qwen3.6 Keep",
	})
	_ = os.WriteFile(otherCfgPath, otherRaw, 0o600)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	settingsPath := filepath.Join(base, "settings.json")
	rawSettings, _ := json.Marshal(core.UserSettings{
		InferyardPath:       ptr(os.Args[0]),
		WorkspaceRoot:       &ws,
		ModelsRoot:          &modelsRoot,
		ResultsRoot:         &resultsRoot,
		EngineInstallations: []string{managedInst, extInst},
	})
	if err := os.WriteFile(settingsPath, rawSettings, 0o600); err != nil {
		t.Fatal(err)
	}

	svc, err := core.New(ctx, core.Options{
		SettingsPath:  settingsPath,
		HostStatePath: filepath.Join(base, "host.json"),
		Execute: func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
			if len(args) > 0 && args[0] == "--version" {
				return inferyard.Execution{Stdout: []byte("inferyard 1.0 source=release\n")}, nil
			}
			if len(args) >= 2 && args[0] == "assets" && args[1] == "scan" {
				return inferyard.Execution{Stdout: []byte(`{"command":"assets scan","status":"scanned","completeness":"complete","limitations":[],"details":{"definition":"asset_scan.v1","models":[],"engines":[],"total_model_bytes":0}}`)}, nil
			}
			if len(args) > 0 && args[0] == "device-check" {
				return inferyard.Execution{Stdout: []byte(`{"command":"device-check","status":"inspected","completeness":"complete","limitations":[],"details":{"kind":"device_preflight.v1","hardware":{},"models":[],"recommendation":{},"platform_capabilities":{}}}`)}, nil
			}
			return inferyard.Execution{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })
	return svc, managedDir, cfgSharedPath, otherCfgPath, sharedWeights
}

func TestModelsPolystrataDeleteScopeDialogAndExternalProtection(t *testing.T) {
	svc, managedDir, cfgSharedPath, _, sharedWeights := newModelsPolystrataDeleteService(t)
	waitUntil(t, func() bool { return !svc.Registry().Refreshing })

	s := testState()
	s.selected = "models"
	s.service = svc
	s.settings = svc.Settings()
	s.registry = svc.Registry()
	s.model = "Qwen3.6 Shared"
	s.engine = "PolyStrata · v0.1.45-poly.10"

	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.modelsView(c) }) }, 1120, 1400)
	tt.Frame()

	if !tt.HasText("删除 · PolyStrata · v0.1.45-poly.10") || !tt.HasText("删除 · Qwen3.6 Shared") {
		t.Fatalf("missing delete actions for managed PolyStrata assets: %v", tt.Texts())
	}
	if !tt.HasText("打开目录 · PolyStrata · v0.1.44-ext") || tt.HasText("删除 · PolyStrata · v0.1.44-ext") {
		t.Fatalf("external PolyStrata installation must not expose delete button: %v", tt.Texts())
	}

	click(t, tt, "删除 · Qwen3.6 Shared")
	joined := strings.Join(tt.Texts(), "\n")
	if !strings.Contains(joined, "共享权重将被保留") {
		t.Fatalf("expected shared-weight preservation note in dialog, got %q", joined)
	}
	click(t, tt, "确认删除")
	if _, err := os.Stat(cfgSharedPath); !os.IsNotExist(err) {
		t.Fatalf("expected shared config deleted, stat err=%v", err)
	}
	if _, err := os.Stat(sharedWeights); err != nil {
		t.Fatalf("shared weights must remain on disk: %v", err)
	}
	if s.model != "" {
		t.Fatalf("selected deleted model should clear, got %q", s.model)
	}

	click(t, tt, "删除 · PolyStrata · v0.1.45-poly.10")
	joined = strings.Join(tt.Texts(), "\n")
	if !strings.Contains(joined, "1.00 MiB") || !strings.Contains(joined, "polystrata-data") {
		t.Fatalf("expected reclaimed size and polystrata-data notice in engine uninstall dialog, got %q", joined)
	}
	click(t, tt, "确认删除")
	if _, err := os.Stat(managedDir); !os.IsNotExist(err) {
		t.Fatalf("expected managed PolyStrata dir removed, stat err=%v", err)
	}
	if _, err := os.Stat(sharedWeights); err != nil {
		t.Fatalf("model data must remain when uninstalling runtime: %v", err)
	}
	if s.engine != "" {
		t.Fatalf("selected uninstalled engine should clear, got %q", s.engine)
	}
}

func TestModelsPolystrataDeletePreviewModeZeroIO(t *testing.T) {
	s := testState()
	s.selected = "models"
	s.preview = &testPreviewStub{}
	s.registry = core.Registry{
		Engines: []core.EngineAsset{
			{Name: "PolyStrata · v0.1.45-poly.10", Family: "polystrata", Kind: "local_installation", Source: "模拟引导安装", Path: "/preview/engines/polystrata-1/installation.json", Deletable: true},
			{Name: "PolyStrata · External", Family: "polystrata", Kind: "local_installation", Source: "本地安装声明", Path: "/external/ps/installation.json", Deletable: false},
		},
		Models: []core.ModelAsset{
			{Name: "Qwen3.6 Preview", Format: "strata_pack", SizeBytes: ptr(int64(4 << 30)), Path: "/preview/models/polystrata-data/models/qwen/strata-qwen.json", Available: true, Deletable: true},
		},
		TotalModelBytes: 4 << 30,
	}
	s.engine = "PolyStrata · v0.1.45-poly.10"
	s.model = "Qwen3.6 Preview"

	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.modelsView(c) }) }, 1120, 1400)
	tt.Frame()

	if tt.HasText("删除 · PolyStrata · External") {
		t.Fatalf("external preview installation must not have delete button: %v", tt.Texts())
	}

	click(t, tt, "删除 · Qwen3.6 Preview")
	if !strings.Contains(strings.Join(tt.Texts(), "\n"), "4.00 GiB") {
		t.Fatalf("expected preview reclaimed model size in dialog: %v", tt.Texts())
	}
	click(t, tt, "确认删除")
	if len(s.registry.Models) != 0 || s.model != "" || s.registry.TotalModelBytes != 0 {
		t.Fatalf("expected preview model cleared on confirm: reg=%+v model=%q", s.registry, s.model)
	}

	click(t, tt, "删除 · PolyStrata · v0.1.45-poly.10")
	joined := strings.Join(tt.Texts(), "\n")
	if !strings.Contains(joined, "2.00 GiB") || !strings.Contains(joined, "polystrata-data") {
		t.Fatalf("expected preview engine scope in dialog, got %q", joined)
	}
	click(t, tt, "确认删除")
	if len(s.registry.Engines) != 1 || s.engine != "" {
		t.Fatalf("expected only external engine left in preview registry: %+v engine=%q", s.registry.Engines, s.engine)
	}
}
