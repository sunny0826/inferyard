package pages

import (
	"testing"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

type enginePreviewStub struct {
	dispatched []previewIntent
}

func (p *enginePreviewStub) showNotice() {}
func (p *enginePreviewStub) dispatch(intent previewIntent) {
	p.dispatched = append(p.dispatched, intent)
}
func (p *enginePreviewStub) resetTask(string, string)                          {}
func (p *enginePreviewStub) report(string) (*core.ReportIndex, *core.ApiError) { return nil, nil }
func (p *enginePreviewStub) detail(string) *core.RunTaskDetail                 { return nil }
func (p *enginePreviewStub) resolveModelURL(string) inferyard.ModelResolve {
	return inferyard.ModelResolve{}
}
func (p *enginePreviewStub) detectedEngines() []inferyard.DetectedEngine { return nil }
func (p *enginePreviewStub) downloadLogTail(string) string               { return "" }

func TestEngineCardWebUIGuardsAndPreviewIsolation(t *testing.T) {
	t.Run("webui-disabled", func(t *testing.T) {
		s := readyState("reports")
		s.engineState = core.EngineState{
			State:         "running",
			Family:        "llama-cpp",
			Pid:           42,
			Endpoint:      "http://127.0.0.1:48857",
			WebUIDisabled: true,
		}
		tt := sidebarHeaderState(s)
		if err := tt.Click("引擎 · 运行中"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if !tt.HasText("网页未启用") || tt.HasText("打开网页") {
			t.Fatalf("unexpected webui-disabled texts: %v", tt.Texts())
		}
		_ = tt.Click("http://127.0.0.1:48857")
		tt.Frame()
		if len(tt.OpenedURLs()) != 0 {
			t.Fatalf("disabled webui still opened URL: %v", tt.OpenedURLs())
		}
		if err := tt.Click("复制服务地址"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if got := tt.Clipboard(); got != "http://127.0.0.1:48857" {
			t.Fatalf("Clipboard = %q", got)
		}
	})

	t.Run("non-loopback-blocked", func(t *testing.T) {
		s := readyState("reports")
		s.engineState = core.EngineState{
			State:    "running",
			Pid:      42,
			Endpoint: "http://192.168.1.20:8080",
		}
		tt := sidebarHeaderState(s)
		if err := tt.Click("引擎 · 运行中"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if !tt.HasText("非本机地址") || tt.HasText("打开网页") {
			t.Fatalf("unexpected non-loopback texts: %v", tt.Texts())
		}
		if _, ok := tt.Find("复制服务地址"); ok {
			t.Fatal("non-loopback endpoint exposed copy button")
		}
		if len(tt.OpenedURLs()) != 0 {
			t.Fatalf("non-loopback endpoint opened URL: %v", tt.OpenedURLs())
		}
	})

	t.Run("preview-no-real-io", func(t *testing.T) {
		s := readyState("reports")
		s.preview = &enginePreviewStub{}
		s.engineState = core.EngineState{
			State:    "running",
			Family:   "llama-cpp",
			Pid:      42,
			Endpoint: "http://127.0.0.1:48857",
		}
		tt := sidebarHeaderState(s)
		if err := tt.Click("引擎 · 运行中"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if err := tt.Click("打开网页"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if len(tt.OpenedURLs()) != 0 {
			t.Fatalf("preview mode called OpenURLThen: %v", tt.OpenedURLs())
		}
		if len(s.pendingToasts) == 0 || s.pendingToasts[0].Title != "已在浏览器打开引擎界面（模拟）" {
			t.Fatalf("preview mode did not enqueue simulated toast: %+v", s.pendingToasts)
		}
	})
}
