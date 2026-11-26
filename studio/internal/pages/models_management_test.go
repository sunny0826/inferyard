package pages

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

type testPreviewStub struct {
	notices int
}

func (p *testPreviewStub) showNotice()                       { p.notices++ }
func (p *testPreviewStub) dispatch(previewIntent)            { p.notices++ }
func (p *testPreviewStub) resetTask(string, string)          {}
func (p *testPreviewStub) detail(string) *core.RunTaskDetail { return nil }
func (p *testPreviewStub) report(string) (*core.ReportIndex, *core.ApiError) {
	return nil, nil
}
func (p *testPreviewStub) resolveModelURL(string) inferyard.ModelResolve {
	return inferyard.ModelResolve{}
}
func (p *testPreviewStub) detectedEngines() []inferyard.DetectedEngine { return nil }
func (p *testPreviewStub) downloadLogTail(string) string               { return "" }

func waitUntil(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for condition")
}

func newIsolatedModelService(
	t *testing.T,
	setup func(base, modelsRoot string) []inferyard.ScannedModel,
) (*core.Service, string, string, string) {
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
	modelsRoot := filepath.Join(base, "models")
	resultsRoot := filepath.Join(base, "results")
	if err := os.MkdirAll(modelsRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(resultsRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	var models []inferyard.ScannedModel
	if setup != nil {
		models = setup(base, modelsRoot)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	settingsPath := filepath.Join(base, "settings.json")
	raw, _ := json.Marshal(core.UserSettings{
		InferyardPath: ptr(os.Args[0]),
		WorkspaceRoot: &base,
	})
	if err := os.WriteFile(settingsPath, raw, 0o600); err != nil {
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
				live := []inferyard.ScannedModel{}
				var total int64
				for _, m := range models {
					if _, err := os.Stat(m.Path); err == nil {
						live = append(live, m)
						if m.SizeBytes != nil {
							total += *m.SizeBytes
						}
					}
				}
				payload, _ := json.Marshal(map[string]any{
					"command":      "assets scan",
					"status":       "scanned",
					"completeness": "complete",
					"limitations":  []string{},
					"details": inferyard.AssetScan{
						Definition:      inferyard.AssetScanDefinition,
						Models:          live,
						Engines:         []inferyard.ScannedEngine{},
						TotalModelBytes: &total,
					},
				})
				return inferyard.Execution{Stdout: payload}, nil
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
	return svc, home, base, modelsRoot
}

func TestModelsManagementRevealAndDeleteConfirmation(t *testing.T) {
	var modelPath string
	svc, _, base, _ := newIsolatedModelService(t, func(_, modelsRoot string) []inferyard.ScannedModel {
		modelPath = filepath.Join(modelsRoot, "demo.gguf")
		if err := os.WriteFile(modelPath, make([]byte, 1024), 0o644); err != nil {
			t.Fatal(err)
		}
		return []inferyard.ScannedModel{{
			Path:      modelPath,
			Name:      "demo",
			Format:    "gguf",
			SizeBytes: ptr(int64(1024)),
			Available: true,
		}}
	})
	waitUntil(t, func() bool {
		reg := svc.Registry()
		return !reg.Refreshing && len(reg.Models) == 1
	})

	origReveal := revealAssetFunc
	t.Cleanup(func() { revealAssetFunc = origReveal })
	var revealed [][2]string
	revealAssetFunc = func(_ *core.Service, kind, name string) *core.ApiError {
		revealed = append(revealed, [2]string{kind, name})
		return nil
	}

	s := testState()
	s.selected = "models"
	s.service = svc
	s.settings = svc.Settings()
	s.registry = svc.Registry()
	s.registry.Engines = []core.EngineAsset{{
		Name:      "local-inst",
		Kind:      "local_installation",
		Family:    "llama-cpp",
		Path:      filepath.Join(base, "install.json"),
		Deletable: false,
	}}

	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.modelsView(c) }) }, 1120, 1400)
	tt.Frame()

	if !tt.HasText("打开目录 · demo.gguf") || !tt.HasText("删除 · demo.gguf") {
		t.Fatalf("missing row actions for deletable model: %v", tt.Texts())
	}
	if !tt.HasText("打开目录 · local-inst") || tt.HasText("删除 · local-inst") {
		t.Fatalf("non-deletable engine row actions wrong: %v", tt.Texts())
	}

	click(t, tt, "打开目录 · demo.gguf")
	click(t, tt, "打开目录 · local-inst")
	if len(revealed) != 2 || revealed[0] != [2]string{"model", "demo.gguf"} || revealed[1] != [2]string{"engine", "local-inst"} {
		t.Fatalf("unexpected reveal calls: %+v", revealed)
	}

	click(t, tt, "删除 · demo.gguf")
	if !tt.HasText("删除以释放空间") || !tt.HasText("确认删除") {
		t.Fatalf("AlertDialog did not open: %v", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if tt.HasText("确认删除") {
		t.Fatalf("Escape did not dismiss delete dialog: %v", tt.Texts())
	}
	if _, err := os.Stat(modelPath); err != nil {
		t.Fatalf("file was deleted on cancel: %v", err)
	}

	click(t, tt, "删除 · demo.gguf")
	click(t, tt, "确认删除")
	if _, err := os.Stat(modelPath); !os.IsNotExist(err) {
		t.Fatalf("expected model deleted on confirm, stat err=%v", err)
	}
	if tt.HasText("demo.gguf") {
		t.Fatalf("deleted model still shown in UI: %v", tt.Texts())
	}
}

func TestModelsManagementPreviewSafeAndInlineEngineWizard(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.MkdirAll(filepath.Join(home, ".ollama", "models"), 0o755); err != nil {
		t.Fatal(err)
	}

	sNil := testState()
	sNil.selected = "models"
	ttNil := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { sNil.modelsView(c) }) }, 1120, 1200)
	ttNil.Frame()
	if ttNil.HasText("+ Ollama") {
		t.Fatalf("nil service should not read host HOME: %v", ttNil.Texts())
	}

	click(t, ttNil, "添加本地引擎")
	if sNil.selected != "models" || !ttNil.HasText("注册本地引擎") || !ttNil.HasText("自动探测本机引擎") {
		t.Fatalf("inline local engine wizard did not open in models page: selected=%q texts=%v", sNil.selected, ttNil.Texts())
	}

	sPrev := testState()
	sPrev.selected = "models"
	sPrev.preview = &testPreviewStub{}
	sPrev.registry = core.Registry{
		Models: []core.ModelAsset{{
			Name:      "Preview.gguf",
			Format:    "gguf",
			SizeBytes: ptr(int64(1024)),
			Available: true,
			Path:      "/preview/models/Preview.gguf",
			Deletable: true,
		}},
		TotalModelBytes: 1024,
		DiskFreeBytes:   ptr(int64(2048)),
		ModelRoots:      []string{"/preview/models"},
	}
	ttPrev := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { sPrev.modelsView(c) }) }, 1120, 1400)
	ttPrev.Frame()

	if ttPrev.HasText("+ Ollama") {
		t.Fatalf("preview mode read host HOME instead of preview fixture: %v", ttPrev.Texts())
	}
	click(t, ttPrev, "添加模型目录")
	if len(sPrev.registry.ModelRoots) != 2 || sPrev.registry.ModelRoots[1] != "/preview/extra-models" {
		t.Fatalf("preview addExtraModelsRoot failed: %+v", sPrev.registry.ModelRoots)
	}
	click(t, ttPrev, "打开目录 · Preview.gguf")
	if len(sPrev.pendingToasts) == 0 || sPrev.pendingToasts[len(sPrev.pendingToasts)-1].Title != "模拟打开目录：Preview.gguf" {
		t.Fatalf("preview reveal did not enqueue toast: %+v", sPrev.pendingToasts)
	}
	click(t, ttPrev, "删除 · Preview.gguf")
	click(t, ttPrev, "确认删除")
	if len(sPrev.registry.Models) != 0 || sPrev.registry.TotalModelBytes != 0 {
		t.Fatalf("preview delete did not update in-memory registry: %+v", sPrev.registry)
	}
}

func TestLibraryModelsAndEnginesPolishLayout(t *testing.T) {
	s := testState()
	s.selected = "library"
	s.preview = &testPreviewStub{}
	modelsRoot := "/preview/models"
	s.settings.Effective.ModelsRoot = &modelsRoot
	s.registry = core.Registry{
		Models: []core.ModelAsset{
			{
				Name:         "Preview-Model-Q4.gguf",
				Format:       "gguf",
				Architecture: "llama",
				SizeBytes:    ptr(int64(4 << 30)),
				Available:    true,
				Path:         "/preview/models/Preview-Model-Q4.gguf",
				Deletable:    true,
			},
			{
				Name:      "Preview-Small.gguf",
				Format:    "gguf",
				SizeBytes: ptr(int64(2 << 30)),
				Available: true,
				Path:      "/preview/models/Preview-Small.gguf",
				Deletable: true,
			},
		},
		Engines: []core.EngineAsset{{
			Name:      "llama-server",
			Family:    "llama-cpp",
			Path:      "/preview/engines/llama-server",
			Deletable: true,
		}},
		TotalModelBytes: 6 << 30,
		DiskFreeBytes:   ptr(int64(100 << 30)),
		ModelRoots:      []string{modelsRoot},
	}
	best := core.EngineSpeedRecord{Engine: "llama.cpp", TokensPerSecond: 68.4, ReportRef: "rep-1"}
	second := core.EngineSpeedRecord{Engine: "mlx-lm", TokensPerSecond: 54.2, ReportRef: "rep-2"}
	s.modelEvidence = map[string]core.ModelEvidenceSummary{
		core.NormalizeModelEvidenceKey("Preview-Model-Q4.gguf"): {
			Model:    "Preview-Model-Q4.gguf",
			Best:     &best,
			ByEngine: []core.EngineSpeedRecord{best, second},
		},
	}

	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).FillWidth().Children(func() { s.libraryView(c) }) }, 1120, 1400)
	tt.Frame()

	if _, ok := tt.AccessByRole(ui.RoleHeading, "可下载模型"); !ok {
		t.Fatalf("expected single catalog heading 可下载模型, texts=%v", tt.Texts())
	}
	if _, ok := tt.AccessByRole(ui.RoleHeading, "模型"); ok {
		t.Fatalf("unexpected duplicate inner heading 模型 inside library models tab")
	}
	if tt.HasText("gguf · 约 2.0 GiB") || tt.HasText("gguf · 架构 llama · 约 4.0 GiB") {
		t.Fatalf("redundant lowercase format prefix still present in model details: %v", tt.Texts())
	}

	for _, name := range []string{"Preview-Model-Q4.gguf", "Preview-Small.gguf"} {
		startBtn, ok1 := tt.AccessByRole(ui.RoleButton, "启动 · "+name)
		benchBtn, ok2 := tt.AccessByRole(ui.RoleButton, "用此模型测评 · "+name)
		openBtn, ok3 := tt.AccessByRole(ui.RoleButton, "打开目录 · "+name)
		delBtn, ok4 := tt.AccessByRole(ui.RoleButton, "删除 · "+name)
		if !ok1 || !ok2 || !ok3 || !ok4 {
			t.Fatalf("missing action buttons for %s: ok=%v,%v,%v,%v", name, ok1, ok2, ok3, ok4)
		}
		for label, b := range map[string]ui.Rect{"bench": benchBtn.Bounds, "open": openBtn.Bounds, "del": delBtn.Bounds} {
			if d := b.Y - startBtn.Bounds.Y; d < -1 || d > 1 {
				t.Fatalf("%s %s wrapped onto another row: startY=%.1f gotY=%.1f", name, label, startBtn.Bounds.Y, b.Y)
			}
			if d := b.H - startBtn.Bounds.H; d < -1 || d > 1 {
				t.Fatalf("%s %s height mismatch: startH=%.1f gotH=%.1f", name, label, startBtn.Bounds.H, b.H)
			}
		}
	}

	click(t, tt, "添加模型目录")
	if len(s.registry.ModelRoots) != 2 || s.registry.ModelRoots[1] != "/preview/extra-models" {
		t.Fatalf("add model dir from library models tab failed: %+v", s.registry.ModelRoots)
	}

	s.libraryTab = 1
	tt.Frame()
	if _, ok := tt.AccessByRole(ui.RoleHeading, "可下载引擎"); !ok {
		t.Fatalf("expected single catalog heading 可下载引擎, texts=%v", tt.Texts())
	}
	if _, ok := tt.AccessByRole(ui.RoleHeading, "引擎"); ok {
		t.Fatalf("unexpected duplicate inner heading 引擎 inside library engines tab")
	}
}
