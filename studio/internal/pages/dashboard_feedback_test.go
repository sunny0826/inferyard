package pages

import (
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"testing"
)

func TestExecuteMessage(t *testing.T) {
	running := executeMessage(core.WizardResult{Tasks: []core.RunTask{{TaskId: "t1"}}})
	if running != "运行已开始，封存后进入下一步。" {
		t.Fatalf("queued run must not read as done: %q", running)
	}
	if got := executeMessage(core.WizardResult{}); got != "操作完成，已更新工作区进度。" {
		t.Fatalf("%q", got)
	}
}

func TestHumanCoversFormerDefaults(t *testing.T) {
	generic := "当前步骤未能完成，请检查主窗口诊断与工作区证据后重试。"
	for _, code := range []string{"internal_error", "not_found", "conflict", "error", "unknown"} {
		if human(code) == generic {
			t.Fatalf("%s must have a specific message", code)
		}
	}
}

func TestRefreshFailureRendersAndClears(t *testing.T) {
	// A refresh-sourced failure renders like an operation failure...
	w := viewState{loaded: true, ready: core.Readiness{CLI: core.InferyardProbe{Status: "ok"}}, refreshFailure: "CLI 执行失败，请检查主窗口诊断后重试。"}
	tt := ui.NewTester(w.view, 1000, 740)
	if !tt.HasText("CLI 执行失败，请检查主窗口诊断后重试。") {
		t.Fatalf("missing refresh failure: %v", tt.Texts())
	}
	// ...and an explicit operation failure wins over it.
	w.failure = &core.ApiError{Code: "invalid_request", Message: human("invalid_request")}
	tt.Frame()
	if !tt.HasText("输入不符合要求，请检查路径、URL 和环境变量名。") || tt.HasText("CLI 执行失败，请检查主窗口诊断后重试。") {
		t.Fatalf("operation failure must take precedence: %v", tt.Texts())
	}
}

func TestCandidateViewExplainsRemainingBundles(t *testing.T) {
	w := viewState{selected: "dashboard", loaded: true, progress: &inferyard.WorkspaceProgress{NextAction: "prepare_candidate", Bundles: []inferyard.BundleProgress{{Bundle: "zh-core", Phase: "run_complete"}, {Bundle: "zh-svg-pelican", Phase: "candidate_missing"}}}}
	tt := ui.NewTester(w.candidateView, 1000, 1400)
	if !tt.HasText("运行 benchmark") || !w.selectedBundles["zh-core"] || w.selectedBundles["zh-svg-pelican"] {
		t.Fatalf("missing remaining-bundle guidance: %v", tt.Texts())
	}
}
