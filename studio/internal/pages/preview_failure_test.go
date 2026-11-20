//go:build ui_debug && !release

package pages

import (
	"testing"
)

func TestPreviewFailedSceneShowsDiagnostics(t *testing.T) {
	p := newPreviewSession(t.Context())
	p.loadScene(previewSceneIndex(t, "任务 · 失败"))
	tt := previewTester(p, 1120, 1800)
	texts := tt.Texts()
	for _, want := range []string{"连接并核验服务失败", "engine_start_failed", "连接已有本机服务", "高级：手动连接"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in failed scene: %v", want, texts)
		}
	}
}

func TestPreviewLocalSceneFillsDiscoveredService(t *testing.T) {
	p := newPreviewSession(t.Context())
	p.loadScene(previewSceneIndex(t, "引擎 · LM Studio 手动绑定"))
	s := p.state
	if s.service != nil {
		t.Fatal("preview created a real service")
	}
	tt := previewTester(p, 1120, 1600)
	if !tt.HasText("已发现的本机服务") {
		t.Fatalf("discovered services not offered: %v", tt.Texts())
	}
	s.localPID, s.localEndpoint = "", ""
	tt.Frame()
	if err := tt.Click("PID 4001 · http://127.0.0.1:48857 · 2 个模型 · LM Studio"); err != nil {
		t.Fatal(err)
	}
	if s.localPID != "4001" || s.localEndpoint != "http://127.0.0.1:48857" {
		t.Fatalf("fields not filled: %q %q", s.localPID, s.localEndpoint)
	}
	if s.service != nil {
		t.Fatal("preview created a real service")
	}
}
