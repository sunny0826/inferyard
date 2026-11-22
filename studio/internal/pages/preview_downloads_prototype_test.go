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

func clickTab(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	info, ok := tt.AccessByRole(ui.RoleTab, label)
	if !ok || info.Bounds.W <= 0 || info.Bounds.H <= 0 {
		t.Fatalf("tab %q not found or has no visible bounds: ok=%v bounds=%+v", label, ok, info.Bounds)
	}
	tt.ClickAt(info.Bounds.X+info.Bounds.W/2, info.Bounds.Y+info.Bounds.H/2)
	tt.Frame()
}

func clickLastNamed(t *testing.T, tt *ui.Tester, label string) {
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
	if got := st.selectedRelease["llama-cpp"]; got != "llama-cpp-upstream-v1.0.0" {
		t.Fatalf("llama-cpp default release = %q, want llama-cpp-upstream-v1.0.0", got)
	}
	if got := st.selectedRelease["sglang"]; got != "sglang-v0.4.6" {
		t.Fatalf("sglang default release = %q, want sglang-v0.4.6", got)
	}

	llama, ok := previewPrototypeCatalog(false).FamilyByID("llama-cpp")
	if !ok {
		t.Fatal("missing llama-cpp family in prototype catalog")
	}
	v1Idx, prismIdx, b11146Idx := -1, -1, -1
	for i, rel := range llama.Releases {
		switch rel.ID {
		case "llama-cpp-upstream-v1.0.0":
			v1Idx = i
		case "llama-cpp-prism-b10743":
			prismIdx = i
		case "llama-cpp-upstream-b11146":
			b11146Idx = i
		}
	}
	if v1Idx < 0 || prismIdx < 0 || b11146Idx < 0 || !(v1Idx < prismIdx && prismIdx < b11146Idx) {
		t.Fatalf("expected llama-cpp order v1.0.0 -> prism -> b11146, got v1=%d prism=%d b11146=%d", v1Idx, prismIdx, b11146Idx)
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
	// Default recommended v1.0.0 is a release_page entry and omits SHA-256.
	if !tt.HasText("下载地址") || tt.HasText("SHA-256：") {
		t.Fatalf("default v1.0.0 release_page should show 下载地址 without SHA-256: %v", tt.Texts())
	}

	// Switch to pre-release v1.1.0-rc1, then filter to stable releases only.
	click(t, tt, "切换版本")
	for _, wantOpt := range []string{"v1.0.0 · ggml-org · 正式版 · 推荐", "prism-b10743-adfffbe · PrismML · 正式版", "v1.1.0-rc1 · ggml-org · 预发布"} {
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
	if got := st.selectedRelease["llama-cpp"]; got != "llama-cpp-upstream-v1.0.0" {
		t.Fatalf("stable filter should fall back to official v1.0.0, got %q", got)
	}
	if tt.HasText("v1.1.0-rc1") {
		t.Fatalf("prerelease should be hidden under stable-only filter: %v", tt.Texts())
	}

	// Switch to PrismML stable archive build, pick Windows x64 + CUDA 12.4.
	click(t, tt, "切换版本")
	click(t, tt, "prism-b10743-adfffbe · PrismML · 正式版")
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
}

func TestPreviewDownloadsPrototypeTabsAndSimulatedInstall(t *testing.T) {
	p, tt := openDownloadsPrototypeTester(t, 3600)

	clickTab(t, tt, "模型")
	for _, want := range []string{"HuggingFace", "ModelScope", "Qwen3-0.6B-Q8_0", "已下载", "Qwen3-4B-Q4_K_M", "自定义模型地址"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q on 模型 tab: %v", want, tt.Texts())
		}
	}
	for _, unwanted := range []string{"获取模型", "任意平台", "需要凭据时"} {
		if tt.HasText(unwanted) {
			t.Fatalf("legacy copy %q should not appear on 模型 tab: %v", unwanted, tt.Texts())
		}
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

func TestPreviewDownloadsPrototypeModelsTabAndDrawer(t *testing.T) {
	p, tt := openDownloadsPrototypeTester(t, 3600)
	st := p.ensurePrototypeState()

	clickTab(t, tt, "模型")

	// Switch download source to ModelScope.
	click(t, tt, "ModelScope")
	if st.modelSource != "modelscope" {
		t.Fatalf("expected modelSource=modelscope, got %q", st.modelSource)
	}

	// Open Qwen3-4B detail drawer under ModelScope source.
	click(t, tt, "模型详情 · model-qwen3-4b-q4km")
	if !st.modelDrawerOpen || !tt.HasText("模型详情") || !tt.HasText("自动解析") {
		t.Fatalf("expected model drawer with auto-resolve status: %v", tt.Texts())
	}
	if !tt.HasText("https://modelscope.cn/Qwen/Qwen3-4B-GGUF/resolve/master/Qwen3-4B-Q4_K_M.gguf") {
		t.Fatalf("expected ModelScope URL inside drawer: %v", tt.Texts())
	}
	if tt.HasText("SHA-256：") {
		t.Fatalf("ModelScope mirror URL should omit unpinned SHA-256: %v", tt.Texts())
	}

	// Switch back to HuggingFace inside drawer and verify pinned SHA-256 + actions.
	clickLastNamed(t, tt, "HuggingFace")
	if st.modelSource != "huggingface" {
		t.Fatalf("expected drawer switch back to huggingface, got %q", st.modelSource)
	}
	if !tt.HasText("bc640142c66e1fdd12af0bd68f40445458f3869b") || !tt.HasText("SHA-256：") {
		t.Fatalf("expected pinned revision and SHA-256 on HuggingFace source: %v", tt.Texts())
	}
	click(t, tt, "复制模型地址 · model-qwen3-4b-q4km")
	if !strings.Contains(p.notice, "已复制 下载地址（模拟）") {
		t.Fatalf("unexpected model copy notice: %q", p.notice)
	}
	click(t, tt, "打开模型链接 · model-qwen3-4b-q4km")
	if !strings.Contains(p.notice, "已在浏览器打开") {
		t.Fatalf("unexpected model open-url notice: %q", p.notice)
	}

	// Close drawer and trigger staged model download.
	click(t, tt, "关闭Qwen3-4B-Q4_K_M")
	if st.modelDrawerOpen {
		t.Fatal("expected model drawer to close")
	}

	qwen4b, ok := core.CatalogEntryByID("model-qwen3-4b-q4km")
	if !ok || p.isProtoModelDownloaded(qwen4b) {
		t.Fatalf("expected Qwen3-4B-Q4_K_M initially not downloaded: ok=%v", ok)
	}
	click(t, tt, "下载模型 · model-qwen3-4b-q4km")
	if !p.isProtoModelDownloading(qwen4b.ID) {
		t.Fatalf("expected active model download task: downloads=%+v", p.state.downloads)
	}
	p.advance(400 * time.Millisecond)
	tt.Frame()
	if !p.isProtoModelDownloading(qwen4b.ID) {
		t.Fatal("expected model download to still be active mid-flight")
	}
	p.advance(400 * time.Millisecond)
	tt.Frame()
	if p.isProtoModelDownloading(qwen4b.ID) || !p.isProtoModelDownloaded(qwen4b) {
		t.Fatalf("expected Qwen3-4B-Q4_K_M downloaded after completion: models=%+v", p.state.registry.Models)
	}

	// Expand custom model URL fold and resolve a HuggingFace model URL.
	click(t, tt, "自定义模型地址")
	tt.Advance(500 * time.Millisecond)
	tt.Frame()
	st.customModelURL = "https://huggingface.co/unsloth/Qwen3-1.7B-GGUF/blob/main/Qwen3-1.7B-Q4_K_M.gguf"
	tt.Frame()
	click(t, tt, "解析地址")
	if st.customResolved == nil || !tt.HasText("固定地址：") {
		t.Fatalf("expected resolved custom model metadata: resolved=%+v texts=%v", st.customResolved, tt.Texts())
	}
	click(t, tt, "下载自定义模型")
	p.advance(400 * time.Millisecond)
	tt.Frame()
	p.advance(400 * time.Millisecond)
	tt.Frame()
	foundCustom := false
	for _, m := range p.state.registry.Models {
		if m.Name == "Qwen3-1.7B-Q4_K_M.gguf" && m.Available {
			foundCustom = true
			break
		}
	}
	if !foundCustom {
		t.Fatalf("expected custom downloaded model registered: %+v", p.state.registry.Models)
	}

	// Simulate Windows host where macOS arm64 directory model collapses into 其他平台模型（1）.
	st.hostPlatform = "windows/amd64"
	tt.Frame()
	if !tt.HasText("其他平台模型（1）") {
		t.Fatalf("expected collapsed 其他平台模型（1） on windows/amd64: %v", tt.Texts())
	}
	click(t, tt, "其他平台模型（1）")
	if !tt.HasText("Qwen3-0.6B-4bit") {
		t.Fatalf("expected macOS mlx model when expanding 其他平台模型（1）: %v", tt.Texts())
	}
	if _, ok := tt.Find("模型详情 · model-qwen3-0.6b-mlx"); !ok {
		t.Fatal("expected 模型详情 · model-qwen3-0.6b-mlx inside 其他平台模型")
	}
	if _, ok := tt.Find("下载模型 · model-qwen3-0.6b-mlx"); ok {
		t.Fatal("other-platform model row must not show local 下载 button")
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
	for _, want := range []string{"推荐组合", "llama.cpp (v1.0.0) · Qwen3-0.6B-Q8_0 (GGUF)", "下载推荐组合"} {
		if !tt.HasText(want) {
			t.Fatalf("missing starter card text %q when no assets present: %v", want, tt.Texts())
		}
	}
	click(t, tt, "下载推荐组合")
	if !p.isProtoFamilyInstalling("llama-cpp") || !p.isProtoModelDownloading("model-qwen3-0.6b-q8") {
		t.Fatalf("expected starter bundle to launch engine + model downloads: downloads=%+v", p.state.downloads)
	}
	p.advance(400 * time.Millisecond)
	tt.Frame()
	p.advance(400 * time.Millisecond)
	tt.Frame()
	if !p.isProtoFamilyInstalled("llama-cpp") {
		t.Fatalf("expected starter bundle to install llama.cpp: %+v", p.state.registry.Engines)
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
