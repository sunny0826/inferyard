//go:build ui_debug && !release

package pages

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func openDownloadsPrototypeTester(t *testing.T, height int) (*previewSession, *ui.Tester) {
	t.Helper()
	p := newPreviewSession(context.Background())
	p.loadScene(previewSceneIndex(t, "下载 · 引擎目录原型"))
	tt := previewTester(p, 1200, height)
	tt.Frame()
	return p, tt
}

func clickTab(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	info, ok := tt.AccessByRole(ui.RoleTab, label)
	if !ok || info.Bounds.W <= 0 || info.Bounds.H <= 0 {
		t.Fatalf("tab %q not found or has no visible bounds: ok=%v bounds=%+v", label, ok, info.Bounds)
	}
	tt.ClickAt(info.Bounds.X+info.Bounds.W/2, info.Bounds.Y+info.Bounds.H/2)
	tt.Frame()
}

func TestPreviewDownloadsPrototypeStructureAndDefaults(t *testing.T) {
	p, tt := openDownloadsPrototypeTester(t, 3600)

	for _, want := range []string{"引擎", "外部应用", "关联已有引擎", "自定义引擎地址", "下载设置", "llama.cpp", "SGLang", "LM Studio"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in prototype view: %v", want, tt.Texts())
		}
	}
	for _, unwanted := range []string{"筛选平台", "仅显示正式版", "交互原型", "筛选格式"} {
		if tt.HasText(unwanted) {
			t.Fatalf("removed control/copy %q should not appear: %v", unwanted, tt.Texts())
		}
	}

	st := p.ensurePrototypeState()
	if got := st.selectedRelease["llama-cpp"]; got != "llama-cpp-prism-b10743" {
		t.Fatalf("llama-cpp default release = %q, want llama-cpp-prism-b10743", got)
	}
	if got := st.selectedRelease["sglang"]; got != "sglang-v0.4.6" {
		t.Fatalf("sglang default release = %q, want sglang-v0.4.6", got)
	}

	llama, ok := previewPrototypeCatalog(false).FamilyByID("llama-cpp")
	if !ok {
		t.Fatal("missing llama-cpp family in prototype catalog")
	}
	v1Idx, b11146Idx := -1, -1
	for i, rel := range llama.Releases {
		switch rel.ID {
		case "llama-cpp-upstream-v1.0.0":
			v1Idx = i
		case "llama-cpp-upstream-b11146":
			b11146Idx = i
		}
	}
	if v1Idx < 0 || b11146Idx < 0 || v1Idx >= b11146Idx {
		t.Fatalf("expected llama-cpp v1.0.0 before b11146, got v1Idx=%d b11146Idx=%d", v1Idx, b11146Idx)
	}

	p.themeMode = 1
	tt.Frame()
	if !tt.HasText("引擎") || !tt.HasText("外部应用") {
		t.Fatal("light mode failed to render prototype sections")
	}
	p.themeMode = 2
	tt.Frame()
	if !tt.HasText("引擎") || !tt.HasText("外部应用") {
		t.Fatal("dark mode failed to render prototype sections")
	}
}

func TestPreviewDownloadsPrototypeDrawerConfigurationAndActions(t *testing.T) {
	p, tt := openDownloadsPrototypeTester(t, 3600)
	st := p.ensurePrototypeState()

	click(t, tt, "版本与地址 · llama-cpp")
	if !st.drawerOpen || !tt.HasText("引擎详情") {
		t.Fatalf("expected detail drawer to open, texts=%v", tt.Texts())
	}

	// Switch to pre-release v1.1.0-rc1, then filter to stable releases only.
	click(t, tt, "切换版本")
	for _, wantOpt := range []string{"v1.0.0 · ggml-org · 正式版", "v1.1.0-rc1 · ggml-org · 预发布"} {
		if !tt.HasText(wantOpt) {
			t.Fatalf("missing version option %q: %v", wantOpt, tt.Texts())
		}
	}
	click(t, tt, "v1.1.0-rc1 · ggml-org · 预发布")
	if got := st.selectedRelease["llama-cpp"]; got != "llama-cpp-upstream-v1.1.0-rc1" {
		t.Fatalf("expected selected release v1.1.0-rc1, got %q", got)
	}

	click(t, tt, "仅正式版")
	if !st.drawerStableOnly {
		t.Fatal("expected drawerStableOnly to become true")
	}
	if got := st.selectedRelease["llama-cpp"]; got != "llama-cpp-prism-b10743" {
		t.Fatalf("stable filter should fall back to PrismML stable, got %q", got)
	}
	if tt.HasText("v1.1.0-rc1") {
		t.Fatalf("prerelease should be hidden under stable-only filter: %v", tt.Texts())
	}

	// Switch platform to Windows x64 and pick CUDA 12.4 variant.
	click(t, tt, "选择平台")
	click(t, tt, "Windows x64")
	if got := st.selectedPlatform["llama-cpp"]; got != "windows/amd64" {
		t.Fatalf("expected windows/amd64 platform selected, got %q", got)
	}
	if st.effectiveHostPlatform() != "windows/amd64" && !tt.HasText("与本机") {
		t.Fatalf("expected non-host platform warning banner: %v", tt.Texts())
	}

	click(t, tt, "CUDA 12.4")
	for _, want := range []string{"llama-server (CUDA 12.4)", "CUDA 12.4 (cudart)", "SHA-256："} {
		if !tt.HasText(want) {
			t.Fatalf("missing Windows CUDA artifact detail %q: %v", want, tt.Texts())
		}
	}

	// Simulated copy & open without OS side effects.
	click(t, tt, "复制地址 · engine-windows-cuda-x64 · 0")
	if !strings.Contains(p.notice, "已复制 下载地址（模拟）") {
		t.Fatalf("unexpected copy notice: %q", p.notice)
	}
	click(t, tt, "打开链接 · engine-windows-cuda-x64 · 0")
	if !strings.Contains(p.notice, "已在浏览器打开") {
		t.Fatalf("unexpected open-url notice: %q", p.notice)
	}

	// Switching to v1.0.0 (release_page) hides SHA-256 line.
	click(t, tt, "切换版本")
	click(t, tt, "v1.0.0 · ggml-org · 正式版")
	if !tt.HasText("下载地址") {
		t.Fatalf("expected 下载地址 section for v1.0.0 release_page: %v", tt.Texts())
	}
	if tt.HasText("SHA-256：") {
		t.Fatalf("release_page entry should omit SHA-256 row: %v", tt.Texts())
	}
}

func TestPreviewDownloadsPrototypeTabsAndSimulatedInstall(t *testing.T) {
	p, tt := openDownloadsPrototypeTester(t, 3600)

	clickTab(t, tt, "模型")
	if !tt.HasText("自定义模型地址") {
		t.Fatalf("expected model assets view on 模型 tab: %v", tt.Texts())
	}
	if tt.HasText("获取模型") {
		t.Fatalf("outer heading 获取模型 should be removed: %v", tt.Texts())
	}

	clickTab(t, tt, "引擎")
	if !tt.HasText("关联已有引擎") {
		t.Fatalf("expected return to 引擎 tab: %v", tt.Texts())
	}

	if p.isProtoFamilyInstalled("sglang") {
		t.Fatal("sglang should not start pre-installed")
	}
	click(t, tt, "安装 · sglang")
	if !tt.HasText("下载任务") || !p.isProtoFamilyInstalling("sglang") || p.isProtoFamilyInstalling("llama-cpp") {
		t.Fatalf("expected only sglang install task active: downloads=%+v texts=%v", p.state.downloads, tt.Texts())
	}

	p.advance(400 * time.Millisecond)
	tt.Frame()
	if !p.isProtoFamilyInstalling("sglang") {
		t.Fatal("expected staged install to remain active mid-flight")
	}

	p.advance(400 * time.Millisecond)
	tt.Frame()
	if p.isProtoFamilyInstalling("sglang") {
		t.Fatal("expected staged install to complete")
	}
	if !p.isProtoFamilyInstalled("sglang") {
		t.Fatalf("expected sglang registered in engines registry: %+v", p.state.registry.Engines)
	}
}

func TestPreviewDownloadsPrototypeExtensionStarterHostSplitAndWizard(t *testing.T) {
	p, tt := openDownloadsPrototypeTester(t, 3600)
	st := p.ensurePrototypeState()

	click(t, tt, "模拟新增引擎")
	if !tt.HasText("KoboldCpp") {
		t.Fatalf("expected custom KoboldCpp engine when toggled: %v", tt.Texts())
	}
	for _, internalCopy := range []string{"扩展接口示例", "EngineCatalogProvider"} {
		if tt.HasText(internalCopy) {
			t.Fatalf("internal copy %q leaked into custom engine UI: %v", internalCopy, tt.Texts())
		}
	}

	click(t, tt, "模拟无本地资源")
	for _, want := range []string{"推荐组合", "llama-server (prism-b10743-adfffbe) · Qwen3-0.6B-Q8_0 (GGUF)", "下载推荐组合"} {
		if !tt.HasText(want) {
			t.Fatalf("missing starter card text %q when no assets present: %v", want, tt.Texts())
		}
	}

	// Simulate a Windows host where mlx-lm and vLLM collapse into 其他平台引擎（2）.
	st.hostPlatform = "windows/amd64"
	tt.Frame()
	if !tt.HasText("其他平台引擎（2）") {
		t.Fatalf("expected collapsed 其他平台引擎（2） on windows/amd64: %v", tt.Texts())
	}
	click(t, tt, "其他平台引擎（2）")
	if !tt.HasText("mlx-lm") || !tt.HasText("vLLM") {
		t.Fatalf("expanding 其他平台引擎（2） should reveal mlx-lm and vLLM: %v", tt.Texts())
	}
	if _, ok := tt.Find("版本与地址 · mlx-lm"); !ok {
		t.Fatal("expected 版本与地址 · mlx-lm inside 其他平台引擎")
	}
	if _, ok := tt.Find("安装 · mlx-lm"); ok {
		t.Fatal("other-platform engine row must not show local 安装 button")
	}

	// LM Studio wizard handoff.
	click(t, tt, "关联 · lmstudio")
	if p.state.selected != "models" || !tt.HasText("添加本地引擎") || !tt.HasText("LM Studio 模型目录") {
		t.Fatalf("expected LM Studio wizard handoff on models page, got selected=%q texts=%v",
			p.state.selected, tt.Texts())
	}
}
