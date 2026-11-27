package pages

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

type engineInstallPreviewStub struct {
	testPreviewStub
	intent previewIntent
}

func (p *engineInstallPreviewStub) dispatch(intent previewIntent) {
	p.intent = intent
}

func TestResolveEngineReleaseForInstallPrefersLocalInstall(t *testing.T) {
	family, ok := core.DefaultEngineCatalog().FamilyByID("llama-cpp")
	if !ok {
		t.Fatal("llama-cpp missing")
	}
	releases := family.ReleasesForPlatform("darwin/arm64", false)
	release := resolveEngineReleaseForInstall("", releases, "darwin/arm64")
	if release.Distribution != "PrismML" {
		t.Fatalf("default release = %s, want PrismML", release.Distribution)
	}
	selected := resolveEngineReleaseForInstall("llama-cpp-upstream-v1.0.0", releases, "darwin/arm64")
	if selected.Distribution != "ggml-org" {
		t.Fatalf("explicit upstream selection was not preserved: %+v", selected)
	}
}

func TestManagedEngineInstallActionUsesPreviewIntent(t *testing.T) {
	preview := &engineInstallPreviewStub{}
	s := testState()
	s.preview = preview
	s.ensureDownloadsUI().hostPlatform = "darwin/arm64"
	tt := ui.NewTester(func(c *ui.Context) { s.downloadsEnginesTab(c) }, 1280, 1200)
	tt.Frame()
	click(t, tt, "安装 · mlx-lm")
	op, ok := preview.intent.(previewDownload)
	if !ok || op.kind != "engine_install" || op.family != "mlx-lm" || op.releaseID != "mlx-lm-0.32.2" {
		t.Fatalf("unexpected managed install intent: %#v", preview.intent)
	}
	click(t, tt, "安装 · llama-cpp")
	op, ok = preview.intent.(previewDownload)
	if !ok || op.kind != "engine" || !strings.Contains(op.source, "PrismML-Eng") {
		t.Fatalf("llama.cpp did not use direct PrismML build: %#v", preview.intent)
	}
}

func TestManagedVenvDeleteConfirmationScope(t *testing.T) {
	message := assetDeleteConfirmMessage(pendingAssetDelete{
		name: "mlx-lm · 0.32.2", path: "/workspace/engines/mlx-lm-0.32.2", scope: "managed_venv_engine",
	})
	if message == "" || !strings.Contains(message, "mlx-lm") {
		t.Fatalf("missing managed venv confirmation: %q", message)
	}
}
