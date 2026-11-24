package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

type stubPreviewMode struct {
	noticed bool
}

func (p *stubPreviewMode) showNotice()              { p.noticed = true }
func (p *stubPreviewMode) dispatch(previewIntent)   {}
func (p *stubPreviewMode) resetTask(string, string) {}
func (p *stubPreviewMode) detail(string) *core.RunTaskDetail {
	return nil
}
func (p *stubPreviewMode) report(string) (*core.ReportIndex, *core.ApiError) {
	return nil, nil
}
func (p *stubPreviewMode) resolveModelURL(string) inferyard.ModelResolve {
	return inferyard.ModelResolve{}
}
func (p *stubPreviewMode) detectedEngines() []inferyard.DetectedEngine { return nil }
func (p *stubPreviewMode) downloadLogTail(string) string               { return "" }

func TestServiceViewEngineLogPreviewModeDoesNoIO(t *testing.T) {
	origOpen := openLogPathFunc
	t.Cleanup(func() { openLogPathFunc = origOpen })
	called := false
	openLogPathFunc = func(string) error {
		called = true
		return nil
	}

	s := testState()
	s.task = benchmarkTask{
		stage:   "bind",
		bundles: []string{"zh-svg-pelican"},
		start:   startChainResult{candidate: "/w/candidates/zh-svg-pelican.toml"},
		failure: core.Err("engine_start_failed"),
	}
	s.engineState = core.EngineState{State: "failed", LogPath: "/nonexistent/preview/engine.log"}
	preview := &stubPreviewMode{}
	s.preview = preview

	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Children(func() { s.serviceView(c) })
	}, 1120, 1600)

	click(t, tt, "查看引擎日志尾部")
	tt.Frame()
	if !tt.HasText("预览模式无本地日志。") {
		t.Fatalf("preview placeholder missing: %v", tt.Texts())
	}

	click(t, tt, "打开日志文件")
	if called || preview.noticed {
		t.Fatalf("preview mode should be a silent no-op (called=%v noticed=%v)", called, preview.noticed)
	}
}
