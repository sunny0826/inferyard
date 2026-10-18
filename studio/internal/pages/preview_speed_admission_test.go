//go:build ui_debug && !release

package pages

import (
	"testing"
	"time"

	"inferyard/studio/internal/core"
)

func TestPreviewSpeedEngineVerificationRecovery(t *testing.T) {
	p, tt := prepareSpeed(t, "128K · 131,072 tokens")
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
