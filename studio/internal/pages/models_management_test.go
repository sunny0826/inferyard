package pages

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"
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
		ModelsRoot:    &modelsRoot,
		ResultsRoot:   &resultsRoot,
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

func TestModelsManagementStorageRootsPickerAndQuickAdd(t *testing.T) {
	svc, home, _, modelsRoot := newIsolatedModelService(t, nil)
	waitUntil(t, func() bool { return !svc.Registry().Refreshing })

	lmsDir := filepath.Join(home, ".lmstudio", "models")
	if err := os.MkdirAll(lmsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	s := testState()
	s.selected = "models"
	s.service = svc
	s.settings = svc.Settings()
	s.registry = core.Registry{
		TotalModelBytes: 2 << 30,
		DiskFreeBytes:   ptr(int64(10 << 30)),
		ModelRoots:      []string{modelsRoot, filepath.Join(home, "custom-models")},
	}
	s.openDialog = func(opts mygo.OpenDialogOptions) ([]string, error) {
		if !opts.Directory {
			t.Fatal("expected directory picker")
		}
		return []string{"/picked/extra-models"}, nil
	}

	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.modelsView(c) }) }, 1120, 1400)
	tt.Frame()

	for _, want := range []string{
		"总占用 2.00 GiB · 可用磁盘空间 10.00 GiB",
		modelsRoot,
		filepath.Join(home, "custom-models"),
		"主目录",
		"添加模型目录",
		"+ LM Studio",
		"下载更多",
		"添加本地引擎",
	} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in %v", want, tt.Texts())
		}
	}

	click(t, tt, "添加常见目录 · LM Studio")
	if len(s.pendingToasts) == 0 || s.pendingToasts[len(s.pendingToasts)-1].Title != "已记录目录："+lmsDir {
		t.Fatalf("unexpected toast after quick-add: %+v", s.pendingToasts)
	}
	tt.Frame()
	if tt.HasText("+ LM Studio") {
		t.Fatalf("quick-add pill should hide once root is active: %v", tt.Texts())
	}

	click(t, tt, "添加模型目录")
	if s.pendingToasts[len(s.pendingToasts)-1].Title != "已记录目录：/picked/extra-models" {
		t.Fatalf("unexpected toast after picker: %+v", s.pendingToasts)
	}

	click(t, tt, "移除目录 · "+filepath.Join(home, "custom-models"))
	if s.pendingToasts[len(s.pendingToasts)-1].Title != "已移除目录："+filepath.Join(home, "custom-models") {
		t.Fatalf("unexpected toast after remove: %+v", s.pendingToasts)
	}

	s.registry.DiskFreeBytes = nil
	tt.Frame()
	if !tt.HasText("总占用 2.00 GiB · 可用磁盘空间 缺测") {
		t.Fatalf("nil free space not rendered cleanly: %v", tt.Texts())
	}
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
