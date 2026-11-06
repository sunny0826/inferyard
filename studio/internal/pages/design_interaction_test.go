package pages

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func TestDesignAsyncFailurePreservesInputFocus(t *testing.T) {
	s := testState()
	s.selected = "settings"
	s.failure = &core.ApiError{Code: "invalid_request", Message: "请修正路径"}
	tt := ui.NewTester(s.view, 1120, 800)
	click(t, tt, "CLI")
	s.failure = &core.ApiError{Code: "output_exists"}
	tt.Frame()
	if !tt.Focused("CLI") {
		t.Fatal("async error stole input focus")
	}
}

func TestDesignInvalidFieldsPreserveEdits(t *testing.T) {
	s := testState()
	s.selected = "settings"
	s.workspace = "relative/workspace"
	s.cliPath = "/keep/cli"
	tt := ui.NewTester(s.view, 1120, 1200)
	click(t, tt, "保存")
	if s.failure == nil || s.busy || s.workspace != "relative/workspace" || s.cliPath != "/keep/cli" {
		t.Fatal("invalid settings submitted or edited values lost")
	}
	click(t, tt, "工作区目录")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("/correct/workspace")
	if s.workspace != "/correct/workspace" {
		t.Fatal("keyboard correction failed", s.workspace)
	}

	s.selected = "dashboard"
	s.resumeSource = &core.RunSummary{RunId: "resume", Path: "/results/run"}
	s.endpoint = "http://127.0.0.1:8080"
	s.pid = ""
	tt = ui.NewTester(s.resumeView, 1000, 700)
	click(t, tt, "确认续测")
	if s.failure == nil || s.endpoint != "http://127.0.0.1:8080" || s.busy {
		t.Fatal("pair error lost input or started work")
	}
	s.endpoint = "invalid url"
	s.pid = "12"
	tt.Frame()
	click(t, tt, "确认续测")
	if s.failure == nil || !strings.Contains(s.failure.Message, "URL") || s.busy {
		t.Fatal("invalid URL passed validation")
	}
}

func TestDesignDetailRetryAndLoading(t *testing.T) {
	s := testState()
	s.selected, s.detailID = "dashboard", "missing"
	entered := make(chan struct{}, 10)
	s.ctx = refreshContext{context.Background(), entered}
	tt := ui.NewTester(s.view, 1120, 800)
	if !tt.HasText("正在读取任务详情") {
		t.Fatal("missing initial loading")
	}
	s.detailError = &core.ApiError{Code: "not_found"}
	tt.Frame()
	if tt.HasText("正在读取任务详情") {
		t.Fatal("failed detail read still claims loading", tt.Texts())
	}
	if _, ok := tt.Find("重试读取详情"); !ok {
		t.Fatal("failed detail read has no retry", tt.Texts())
	}
	click(t, tt, "重试读取详情")
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("retry did not reach refresh")
	}
	if s.detailError != nil || !tt.HasText("正在读取任务详情") {
		t.Fatal("retry did not restore loading state")
	}
}

func TestDesignPageLoadingErrorEmpty(t *testing.T) {
	for _, page := range []string{"dashboard", "device", "models", "downloads", "reports", "settings"} {
		s := testState()
		s.selected = page
		s.loaded = false
		tt := ui.NewTester(s.view, 1120, 800)
		if !tt.HasText("正在读取页面数据") || tt.HasText("暂无报告") || tt.HasText("尚无设备快照") {
			t.Fatal(page, tt.Texts())
		}
		s.refreshFailure = "读取失败，请重试。"
		tt.Frame()
		if tt.HasText("正在读取页面数据") {
			t.Fatal("error also claims loading", page)
		}
		if _, ok := tt.Find("重试读取页面"); !ok {
			t.Fatal("missing recovery", page)
		}
	}
	s := testState()
	s.selected = "reports"
	s.historyError = &core.ApiError{Code: "internal_error"}
	s.reportsError = s.historyError
	tt := ui.NewTester(s.view, 1120, 1200)
	if tt.HasText("暂无报告") || tt.HasText("暂无运行记录") {
		t.Fatal("failed reads shown as empty success")
	}
}
