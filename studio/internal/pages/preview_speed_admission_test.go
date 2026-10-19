//go:build ui_debug && !release

package pages

import (
	"testing"
	"time"

	"inferyard/studio/internal/core"
)

func TestPreviewSpeedEngineVerificationRecovery(t *testing.T) {
	p, tt := prepareSpeed(t, "128k")
	s := p.state
	s.checks.Supported = false
	s.checks.Engines[s.engine] = core.EngineEligibility{State: core.EngineCheckUnknown, Reason: "cli_outdated"}
	tt.Frame()
	if !tt.HasText("当前 CLI 不支持引擎核验；请在设置中选择支持长上下文速度的 CLI。") {
		t.Fatal("preview hid the CLI blocker", tt.Texts())
	}
	click(t, tt, "开始专项测评")
	p.advance(time.Second)
	if s.speed.current != nil || s.busy || len(p.jobs) != 0 {
		t.Fatal("blocked selection started simulated work")
	}
	s.checks.Supported = true
	s.checks.Engines[s.engine] = core.EngineEligibility{State: core.EngineCheckEligible}
	tt.Frame()
	click(t, tt, "开始专项测评")
	if s.speed.current == nil || s.speed.current.context != 131072 || !s.busy {
		t.Fatal("verified selection did not recover without resetting the form")
	}
	if s.service != nil {
		t.Fatal("preview created a real service")
	}
}

func TestPreviewSpeedModelContextAdmissionParity(t *testing.T) {
	p, tt := prepareSpeed(t, "64k")
	production := runStateFixture()
	production.speed = &speedWorkspace{selected: true, context: 65536}
	want := "所选模型最多支持 8k 上下文，不能运行 64k 档；请更换长上下文模型。"
	for _, s := range []*viewState{production, p.state} {
		s.registry.Models = []core.ModelAsset{{Name: s.model, ContextLength: ptr(int64(8192)), Available: true}}
		if got := s.speedStartBlockReason(); got != want {
			t.Fatalf("model limit: %q", got)
		}
		s.startSpeed()
		if s.speed.current != nil || s.busy {
			t.Fatal("unsupported context started work")
		}
	}
	tt.Frame()
	if !tt.HasText(want) {
		t.Fatal(tt.Texts())
	}
	for _, s := range []*viewState{production, p.state} {
		s.registry.Models[0].ContextLength = ptr(int64(262144))
		if reason := s.speedStartBlockReason(); reason != "" {
			t.Fatal("supported model did not recover", reason)
		}
	}
	tt.Frame()
	click(t, tt, "开始专项测评")
	if p.state.speed.current == nil || p.state.service != nil {
		t.Fatal("preview recovery failed or reached real IO")
	}
}
