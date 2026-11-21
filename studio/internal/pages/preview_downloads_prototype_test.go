//go:build ui_debug && !release

package pages

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func openDownloadsPrototypeTester(t *testing.T, height int) (*previewSession, *ui.Tester) {
	t.Helper()
	p := newPreviewSession(context.Background())
	p.loadScene(previewSceneIndex(t, "下载 · 引擎目录原型"))
	tt := previewTester(p, 1200, height)
	tt.Frame()
	return p, tt
}

func TestPreviewDownloadsPrototypeLoadsAllFamiliesAndStableFirst(t *testing.T) {
	p, tt := openDownloadsPrototypeTester(t, 3600)

	// Switch to all platforms so all 5 families are visible regardless of test host OS.
	click(t, tt, "筛选平台")
	click(t, tt, "全部平台")

	for _, name := range []string{"llama.cpp", "mlx-lm", "vLLM", "SGLang", "LM Studio"} {
		if !tt.HasText(name) {
			t.Fatalf("missing engine family %q in prototype: %v", name, tt.Texts())
		}
	}

	// Verify stable-first defaults on multi-version families.
	st := p.ensurePrototypeState()
	if got := st.selectedRelease["llama-cpp"]; got != "llama-cpp-prism-b10743" {
		t.Fatalf("llama-cpp default release = %q, want llama-cpp-prism-b10743", got)
	}
	if got := st.selectedRelease["sglang"]; got != "sglang-v0.4.6" {
		t.Fatalf("sglang default release = %q, want sglang-v0.4.6", got)
	}

	// Verify light and dark modes render cleanly.
	p.themeMode = 1
	tt.Frame()
	if !tt.HasText("交互原型") {
		t.Fatal("light mode failed to render prototype")
	}
	p.themeMode = 2
	tt.Frame()
	if !tt.HasText("交互原型") {
		t.Fatal("dark mode failed to render prototype")
	}
}

func TestPreviewDownloadsPrototypeVersionSwitchAndStableFilter(t *testing.T) {
	p, tt := openDownloadsPrototypeTester(t, 3600)

	// Switch llama.cpp from PrismML to ggml-org upstream b11146.
	click(t, tt, "选择版本 · llama-cpp")
	click(t, tt, "b11146 · ggml-org 上游 · 正式版")
	if got := p.ensurePrototypeState().selectedRelease["llama-cpp"]; got != "llama-cpp-upstream-b11146" {
		t.Fatalf("llama-cpp selected release = %q, want llama-cpp-upstream-b11146", got)
	}
	if !tt.HasText("https://github.com/ggml-org/llama.cpp/releases/tag/b11146") {
		t.Fatalf("upstream URL did not update after version switch: %v", tt.Texts())
	}

	// Switch SGLang to snapshot e262b44, then enable "仅显示正式版" to verify non-stable is filtered out.
	click(t, tt, "选择版本 · sglang")
	click(t, tt, "e262b44 · 固定源码提交 · 源码快照（推荐）")
	if got := p.ensurePrototypeState().selectedRelease["sglang"]; got != "sglang-e262b44" {
		t.Fatalf("sglang selected release = %q, want sglang-e262b44", got)
	}

	click(t, tt, "仅显示正式版")
	if !p.ensurePrototypeState().stableOnly {
		t.Fatal("stableOnly checkbox did not toggle")
	}
	if got := p.ensurePrototypeState().selectedRelease["sglang"]; got != "sglang-v0.4.6" {
		t.Fatalf("sglang did not fall back to stable release when stableOnly=true: %q", got)
	}
}

func TestPreviewDownloadsPrototypePlatformFilterAndMultiArtifactDrawer(t *testing.T) {
	p, tt := openDownloadsPrototypeTester(t, 3600)

	click(t, tt, "筛选平台")
	click(t, tt, "Windows x64")
	if core.CurrentPlatform() != "windows/amd64" {
		if !tt.HasText("当前正在查看 Windows x64 引擎构建；可复制或打开地址，不直接安装到本机。") {
			t.Fatalf("missing non-current-platform banner: %v", tt.Texts())
		}
	}
	if !tt.HasText("Windows x64 · CUDA 12.4") || !tt.HasText("全部地址与校验") {
		t.Fatalf("windows cuda build not rendered: %v", tt.Texts())
	}

	// Open Windows CUDA detail drawer and inspect both artifacts (engine + cudart).
	click(t, tt, "全部地址与校验 · engine-windows-cuda-x64")
	if !p.ensurePrototypeState().drawerOpen {
		t.Fatal("detail drawer did not open")
	}
	if !tt.HasText("llama-server 主程序 (CUDA 12.4)") || !tt.HasText("CUDA 12.4 运行时 (cudart)") {
		t.Fatalf("drawer missing multi-artifact entries: %v", tt.Texts())
	}

	// Verify in-memory copy and external open update preview state safely.
	click(t, tt, "复制文件 URL · 0")
	if !strings.Contains(p.notice, "已模拟复制文件地址") {
		t.Fatalf("copy did not record simulated notice: %q", p.notice)
	}
	click(t, tt, "复制校验和 · 0")
	if !strings.Contains(p.notice, "已模拟复制SHA-256") {
		t.Fatalf("copy sha256 did not record simulated notice: %q", p.notice)
	}
	click(t, tt, "打开文件链接 · 1")
	if !strings.Contains(p.notice, "cudart-llama-bin-win-cuda-12.4-x64.zip") {
		t.Fatalf("open link did not record simulated notice: %q", p.notice)
	}

	// Switch version directly inside the detail drawer and confirm synced state.
	click(t, tt, "抽屉切换版本")
	click(t, tt, "b11146 · ggml-org 上游 · 正式版")
	if got := p.ensurePrototypeState().selectedRelease["llama-cpp"]; got != "llama-cpp-upstream-b11146" {
		t.Fatalf("drawer version switch did not sync selectedRelease: %q", got)
	}

	click(t, tt, "关闭llama.cpp · b11146")
	if p.ensurePrototypeState().drawerOpen {
		t.Fatal("detail drawer did not close")
	}

	// Verify model format segmented filter hides/shows matching families.
	click(t, tt, "目录模型")
	if tt.HasText("选择版本 · llama-cpp") || !tt.HasText("选择版本 · sglang") {
		t.Fatalf("directory format filter failed: %v", tt.Texts())
	}
	click(t, tt, "全部格式")
	if !tt.HasText("选择版本 · llama-cpp") {
		t.Fatalf("resetting format filter to all did not restore llama.cpp: %v", tt.Texts())
	}
}

func TestPreviewDownloadsPrototypeTabsAndDirectDownload(t *testing.T) {
	p, tt := openDownloadsPrototypeTester(t, 3600)

	// Switch to Model tab and verify model catalog + mirror switcher render.
	click(t, tt, "模型资源")
	if !tt.HasText("获取模型") || !tt.HasText("Qwen3-0.6B-Q8_0（GGUF）") || !tt.HasText("ModelScope") {
		t.Fatalf("model tab did not render expected catalog: %v", tt.Texts())
	}

	// Switch back to Engine tab and trigger simulated direct download if supported on host.
	click(t, tt, "推理引擎")
	defaultEngineID := core.CatalogEngineForCurrentPlatform()
	if defaultEngineID != "" {
		click(t, tt, "下载引擎 · "+defaultEngineID)
		if len(p.state.downloads) != 1 || p.state.downloads[0].State != "downloading" {
			t.Fatalf("engine download did not start: %+v", p.state.downloads)
		}
		if !tt.HasText("下载任务") {
			t.Fatalf("download task card missing after starting download: %v", tt.Texts())
		}
		p.advance(3 * time.Second)
		tt.Frame()
		if p.state.downloads[0].State != "done" {
			t.Fatalf("simulated engine download did not complete: %+v", p.state.downloads[0])
		}
	}
}

func TestPreviewDownloadsPrototypeWizardHandoffAndCustomExtension(t *testing.T) {
	p, tt := openDownloadsPrototypeTester(t, 3600)

	// Toggle custom engine provider extension and verify dynamic registration + stable-first sorting.
	click(t, tt, "模拟扩展接入新引擎")
	if !tt.HasText("KoboldCpp（扩展接口示例）") {
		t.Fatalf("custom extension engine did not appear: %v", tt.Texts())
	}
	if got := p.ensurePrototypeState().selectedRelease["koboldcpp"]; got != "koboldcpp-v1.91" {
		t.Fatalf("custom extension did not default to stable release: %q", got)
	}

	// Toggle empty assets simulation and verify quick-start bundle card appears.
	click(t, tt, "模拟首次无资源")
	if core.DefaultEngineURL() != "" && core.DefaultModelURL() != "" {
		if !tt.HasText("快速开始 · 一键获取推荐组合") || !tt.HasText("一键下载推荐引擎与模型") {
			t.Fatalf("starter bundle card did not appear when no assets: %v", tt.Texts())
		}
	}

	// Switch to all platforms and verify "去注册" opens models page with wizard pre-selected to mlx-lm.
	click(t, tt, "筛选平台")
	click(t, tt, "全部平台")
	click(t, tt, "去注册 · engine-mlx-lm-macos-arm64")
	if p.state.selected != "models" {
		t.Fatalf("selected page = %q, want models", p.state.selected)
	}
	if !tt.HasText("注册本地引擎") || !tt.HasText("虚拟环境 Python 路径") {
		t.Fatalf("wizard did not open with mlx-lm pre-selected: %v", tt.Texts())
	}
}
