package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func TestRefreshFailureRendersAndClears(t *testing.T) {
	w := viewState{loaded: true, ready: core.Readiness{CLI: core.InferyardProbe{Status: "ok"}}, refreshFailure: "CLI 执行失败，请检查主窗口诊断后重试。"}
	tt := ui.NewTester(w.view, 1000, 740)
	if !tt.HasText("CLI 执行失败，请检查主窗口诊断后重试。") {
		t.Fatalf("missing refresh failure: %v", tt.Texts())
	}
	w.failure = &core.ApiError{Code: "invalid_request", Message: human("invalid_request")}
	tt.Frame()
	if !tt.HasText("输入不符合要求，请检查路径、URL 和环境变量名。") || tt.HasText("CLI 执行失败，请检查主窗口诊断后重试。") {
		t.Fatalf("operation failure must take precedence: %v", tt.Texts())
	}
}
