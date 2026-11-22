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

type resolvePreviewStub struct {
	notices int
	intents []previewIntent
}

func (p *resolvePreviewStub) showNotice()                                       { p.notices++ }
func (p *resolvePreviewStub) dispatch(i previewIntent)                          { p.intents = append(p.intents, i) }
func (p *resolvePreviewStub) resetTask(string, string)                          {}
func (p *resolvePreviewStub) report(string) (*core.ReportIndex, *core.ApiError) { return nil, nil }
func (p *resolvePreviewStub) detail(string) *core.RunTaskDetail                 { return nil }

func clickDownloadsTab(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	info, ok := tt.AccessByRole(ui.RoleTab, label)
	if !ok || info.Bounds.W <= 0 || info.Bounds.H <= 0 {
		t.Fatalf("tab %q not found or has no visible bounds: ok=%v bounds=%+v", label, ok, info.Bounds)
	}
	tt.ClickAt(info.Bounds.X+info.Bounds.W/2, info.Bounds.Y+info.Bounds.H/2)
	tt.Frame()
}

func clickLastSegmented(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	var match *ui.AccessInfo
	for _, info := range tt.AccessAll() {
		if info.Name == label && info.Bounds.W > 0 && info.Bounds.H > 0 {
			copyInfo := info
			match = &copyInfo
		}
	}
	if match == nil {
		t.Fatalf("element %q not found or has no visible bounds", label)
	}
	tt.ClickAt(match.Bounds.X+match.Bounds.W/2, match.Bounds.Y+match.Bounds.H/2)
	tt.Frame()
}

func TestStarterDownloadButtonVisibilityAndSecondaryStyle(t *testing.T) {
	s := testState()
	s.progress.NextAction = "prepare_candidate"
	var accent ui.Color
	tt := ui.NewTester(func(c *ui.Context) {
		accent = c.Theme().Accent
		s.downloadsPageView(c)
	}, 1120, 2400)

	if core.DefaultEngineURL() != "" && core.DefaultModelURL() != "" {
		if !tt.HasText("推荐组合") {
			t.Fatalf("starter heading missing: %v", tt.Texts())
		}
		filled, found := buttonFill(tt, "下载推荐组合", accent)
		if !found {
			t.Fatalf("starter button missing when both assets missing: %v", tt.Texts())
		}
		if filled {
			t.Fatal("starter button must remain secondary (not primary filled)")
		}
		stub := &resolvePreviewStub{}
		s.preview = stub
		click(t, tt, "下载推荐组合")
		if len(stub.intents) != 2 {
			t.Fatalf("expected 2 preview downloads, got %d", len(stub.intents))
		}
		d0, ok0 := stub.intents[0].(previewDownload)
		d1, ok1 := stub.intents[1].(previewDownload)
		if !ok0 || !ok1 || d0.kind != "engine" || d0.source != core.DefaultEngineURL() || d1.kind != "model" || d1.source != core.DefaultModelURL() {
			t.Fatalf("unexpected starter intents: %#v", stub.intents)
		}
		s.preview = nil
	}

	s.registry.Engines = []core.EngineAsset{{Name: "llama"}}
	tt.Frame()
	if tt.HasText("下载推荐组合") {
		t.Fatal("starter button must hide once engine is present")
	}

	s.registry.Engines = nil
	s.downloads = []core.DownloadTask{{TaskId: "m", Kind: "model", State: "downloading"}}
	tt.Frame()
	if tt.HasText("下载推荐组合") {
		t.Fatal("starter button must hide while model is downloading")
	}
}

func TestCatalogMirrorSelectorSwitchesBetweenHFAndModelScope(t *testing.T) {
	s := testState()
	st := s.ensureDownloadsUI()
	st.hostPlatform = "darwin/arm64"
	tt := ui.NewTester(s.downloadsPageView, 1120, 2600)

	clickDownloadsTab(t, tt, "模型")
	qwen4b, ok := core.CatalogEntryByID("model-qwen3-4b-q4km")
	if !ok {
		t.Fatal("missing model-qwen3-4b-q4km")
	}
	if !tt.HasText("HuggingFace") || !tt.HasText("ModelScope") || !tt.HasText("Qwen3-4B-Q4_K_M") {
		t.Fatalf("models tab controls missing: %v", tt.Texts())
	}

	click(t, tt, "ModelScope")
	if st.modelSource != "modelscope" {
		t.Fatalf("modelSource after ModelScope click = %q, want modelscope", st.modelSource)
	}

	click(t, tt, "模型详情 · model-qwen3-4b-q4km")
	if !st.modelDrawerOpen || !tt.HasText("自动解析") || !tt.HasText(qwen4b.MirrorURL) {
		t.Fatalf("model drawer did not reflect ModelScope source: %v", tt.Texts())
	}
	if tt.HasText("SHA-256：") {
		t.Fatalf("unpinned ModelScope mirror URL must omit SHA-256: %v", tt.Texts())
	}

	clickLastSegmented(t, tt, "HuggingFace")
	if st.modelSource != "huggingface" || !tt.HasText(qwen4b.Revision) || !tt.HasText("SHA-256：") {
		t.Fatalf("drawer switch back to HuggingFace failed: source=%q texts=%v", st.modelSource, tt.Texts())
	}
	click(t, tt, "复制模型地址 · model-qwen3-4b-q4km")
	if got := tt.Clipboard(); got != qwen4b.URL {
		t.Fatalf("clipboard = %q, want %q", got, qwen4b.URL)
	}
	click(t, tt, "打开模型链接 · model-qwen3-4b-q4km")
	opened := tt.OpenedURLs()
	if len(opened) == 0 || opened[len(opened)-1] != qwen4b.URL {
		t.Fatalf("opened URLs = %v, want %q", opened, qwen4b.URL)
	}
	click(t, tt, "关闭Qwen3-4B-Q4_K_M")

	// Platform-specific mlx directory model collapses when switching to Windows.
	st.hostPlatform = "windows/amd64"
	tt.Frame()
	if !tt.HasText("其他平台模型（1）") {
		t.Fatalf("expected collapsed 其他平台模型（1） on windows/amd64: %v", tt.Texts())
	}
	click(t, tt, "其他平台模型（1）")
	if _, ok := tt.Find("模型详情 · model-qwen3-0.6b-mlx"); !ok {
		t.Fatal("expected 模型详情 · model-qwen3-0.6b-mlx inside 其他平台模型")
	}
	if _, ok := tt.Find("下载模型 · model-qwen3-0.6b-mlx"); ok {
		t.Fatal("other-platform model must not expose local 下载 button")
	}
}

func TestModelResolveButtonPreviewAndLiveAsyncResolution(t *testing.T) {
	// Preview mode: deterministic inline resolve without unsupported notice.
	sPreview := testState()
	stub := &resolvePreviewStub{}
	sPreview.preview = stub
	qwen, _ := core.CatalogEntryByID("model-qwen3-0.6b-q8")
	sPreview.ensureDownloadsUI().customModelURL = qwen.MirrorURL
	ttPreview := ui.NewTester(sPreview.downloadsPageView, 1120, 2600)
	clickDownloadsTab(t, ttPreview, "模型")
	click(t, ttPreview, "自定义模型地址")
	ttPreview.Advance(500 * time.Millisecond)
	ttPreview.Frame()
	click(t, ttPreview, "解析地址")
	if stub.notices != 0 {
		t.Fatalf("preview resolve showed unsupported notice: %d", stub.notices)
	}
	if !ttPreview.HasText("已解析版本："+qwen.Revision) || !ttPreview.HasText("Qwen3-0.6B-Q8_0.gguf") {
		t.Fatalf("preview resolved info missing: %v", ttPreview.Texts())
	}

	// Live service mode: async resolve populates resolve state and clears on URL change.
	root := t.TempDir()
	ws := filepath.Join(root, "ws")
	models := filepath.Join(root, "models")
	_ = os.MkdirAll(ws, 0700)
	_ = os.MkdirAll(models, 0700)
	settingsPath := filepath.Join(root, "settings.json")
	rawSettings, _ := json.Marshal(core.UserSettings{
		WorkspaceRoot: ptr(ws),
		ModelsRoot:    ptr(models),
		InferyardPath: ptr(os.Args[0]),
	})
	if err := os.WriteFile(settingsPath, rawSettings, 0600); err != nil {
		t.Fatal(err)
	}
	sha := "23749fefcc72300e3a2ad315e1317431b06b590a"
	rawURL := "https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/resolve/main/Qwen3-0.6B-Q8_0.gguf?download=true"
	pinnedURL := "https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/blob/" + sha + "/Qwen3-0.6B-Q8_0.gguf"

	svc, err := core.New(context.Background(), core.Options{
		SettingsPath: settingsPath,
		Execute: func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
			if len(args) > 0 && args[0] == "--version" {
				return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 1.0 source=release\n")}, nil
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
			return inferyard.Execution{ExitCode: inferyard.ExitBlocked}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	sLive := testState()
	sLive.ctx = context.Background()
	sLive.service = svc
	sLive.assetsPreset = true
	sLive.engineURL = core.DefaultEngineURL()
	sLive.ensureDownloadsUI().customModelURL = rawURL
	ttLive := ui.NewTester(sLive.downloadsPageView, 1120, 2600)
	clickDownloadsTab(t, ttLive, "模型")
	click(t, ttLive, "自定义模型地址")
	ttLive.Advance(500 * time.Millisecond)
	ttLive.Frame()

	click(t, ttLive, "解析地址")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		ttLive.Frame()
		if ttLive.HasText("已解析版本：" + sha) {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if !ttLive.HasText("已解析版本："+sha) || !ttLive.HasText("固定地址："+pinnedURL) || !ttLive.HasText("609.82 MiB（639446688 bytes）") {
		t.Fatalf("live resolved info missing: %v", ttLive.Texts())
	}

	sLive.ensureDownloadsUI().customModelURL = "https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/resolve/other/Qwen3-0.6B-Q8_0.gguf"
	ttLive.Frame()
	if ttLive.HasText("已解析版本：" + sha) {
		t.Fatal("stale resolved info remained after URL change")
	}
}

func TestCustomModelURLValidationErrors(t *testing.T) {
	if got := customModelURLError("http://huggingface.co/a/b/resolve/main/m.gguf"); got != "请输入完整的 https URL。" {
		t.Fatalf("http error = %q", got)
	}
	if got := customModelURLError("https://example.com/a/b/resolve/main/m.gguf"); got == "" {
		t.Fatal("expected domain error for example.com")
	}
	if got := customModelURLError("https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/resolve/main/Qwen3-0.6B-Q8_0.gguf?download=true"); got != "" {
		t.Fatalf("unexpected error for valid resolve url: %q", got)
	}
}
