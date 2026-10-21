package pages

import (
	"bytes"
	"context"
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDesignAnnouncementsOnce(t *testing.T) {
	s := testState()
	tt := ui.NewTester(s.view, 1120, 800)
	if got := tt.Announcements(); len(got) != 1 || got[0] != "任务" {
		t.Fatal(got)
	}
	for range 3 {
		tt.Frame()
	}
	if got := tt.Announcements(); len(got) != 0 {
		t.Fatal("idle frames repeat page announcement", got)
	}
	click(t, tt, "设置")
	if got := tt.Announcements(); len(got) != 1 || got[0] != "设置" {
		t.Fatal(got)
	}
	s.finishWork("", &core.ApiError{Code: "invalid_request", Message: "请修正路径"})
	tt.Frame()
	if got := tt.Announcements(); len(got) != 1 || !strings.Contains(got[0], "请修正路径") {
		t.Fatal(got)
	}
	s.failure = &core.ApiError{Code: "invalid_request", Message: "请修正路径"}
	for range 4 {
		tt.Frame()
	}
	if got := tt.Announcements(); len(got) != 0 {
		t.Fatal("refresh repeats same error", got)
	}
	click(t, tt, "CLI")
	s.failure = &core.ApiError{Code: "output_exists"}
	tt.Frame()
	if !tt.Focused("CLI") {
		t.Fatal("async error stole input focus")
	}
	if got := tt.Announcements(); len(got) != 1 {
		t.Fatal(got)
	}
	s.failure = nil
	tt.Frame()
	s.failure = &core.ApiError{Code: "output_exists"}
	tt.Frame()
	if got := tt.Announcements(); len(got) != 1 {
		t.Fatal("new failure occurrence not announced", got)
	}
}

func TestDesignHiddenDetailErrorIsNotAnnounced(t *testing.T) {
	s := testState()
	s.selected = "dashboard"
	s.detailError = &core.ApiError{Code: "not_found", Message: "hidden detail"}
	tt := ui.NewTester(s.view, 1120, 800)
	for _, message := range tt.Announcements() {
		if strings.Contains(message, "hidden detail") {
			t.Fatal("hidden error announced")
		}
	}
	s.failure = &core.ApiError{Code: "invalid_request", Message: "same failure"}
	s.historyError = s.failure
	tt.Frame()
	if got := tt.Announcements(); len(got) != 1 {
		t.Fatal("duplicate error announcement", got)
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
	invalid := false
	for _, n := range designFieldInputs(tt, "工作区目录") {
		if n.FieldByName("invalid").Bool() && strings.Contains(n.FieldByName("description").String(), "绝对路径") {
			invalid = true
		}
	}
	if !invalid {
		t.Fatal("input error was not associated with workspace field")
	}
	click(t, tt, "工作区目录")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("/correct/workspace")
	if s.workspace != "/correct/workspace" {
		t.Fatal("keyboard correction failed", s.workspace)
	}
	for _, n := range designFieldInputs(tt, "工作区目录") {
		if n.FieldByName("invalid").Bool() {
			t.Fatal("corrected field remains invalid")
		}
	}

	s.selected = "dashboard"
	s.resumeSource = &core.RunSummary{RunId: "resume", Path: "/results/run"}
	s.endpoint = "http://127.0.0.1:8080"
	s.pid = ""
	tt = ui.NewTester(func(c *ui.Context) { s.resumeView(c) }, 1000, 700)
	click(t, tt, "确认续测")
	if s.failure == nil || s.endpoint != "http://127.0.0.1:8080" {
		t.Fatal("pair error lost input")
	}
	for _, label := range []string{"endpoint_url（可选）", "server_pid（与 endpoint 同给同省）"} {
		nodes := designFieldInputs(tt, label)
		if len(nodes) == 0 || !nodes[0].FieldByName("invalid").Bool() {
			t.Fatal("missing paired field error", label)
		}
	}
	s.endpoint = "invalid url"
	s.pid = "12"
	tt.Frame()
	click(t, tt, "确认续测")
	if s.failure == nil || !strings.Contains(s.failure.Message, "URL") {
		t.Fatal("invalid URL passed validation")
	}
	s.selected = "models"
	s.modelURL = "bad url"
	s.modelName = "../bad"
	tt = ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.modelDownloadForm(c) }) }, 800, 1200)
	click(t, tt, "自定义模型地址")
	for _, label := range []string{"模型 URL（固定 40 位 revision 的 GGUF）"} {
		nodes := designFieldInputs(tt, label)
		if len(nodes) == 0 || !nodes[0].FieldByName("invalid").Bool() {
			t.Fatal("missing download field error", label)
		}
	}
}

func TestDesignNamedControlsAndHeadingLevels(t *testing.T) {
	for _, page := range []string{"dashboard", "device", "models", "downloads", "reports", "settings", "detail"} {
		s := designFixture(page)
		tt := ui.NewTester(s.view, 1120, 800)
		designNodes(tt, func(n reflect.Value, _ bool) {
			role := ui.Role(n.FieldByName("role").Uint())
			switch role {
			case ui.RoleHeading:
				level := n.FieldByName("level").Int()
				if level < 1 || level > 3 {
					t.Errorf("%s bad heading level %d", page, level)
				}
			case ui.RoleProgress:
				if n.FieldByName("label").String() == "" {
					t.Errorf("%s unnamed progress", page)
				}
			}
			if role == ui.RoleTable {
				if n.FieldByName("label").String() == "" {
					t.Errorf("%s unnamed table", page)
				}
			}
		})
	}
}

func TestDesignReducedMotionIsStatic(t *testing.T) {
	s := testState()
	s.downloads = []core.DownloadTask{{TaskId: "active", State: "downloading", OutName: "model"}}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.downloadsView(c) }) }, 800, 800)
	tt.SetPreferences(ui.Preferences{TextScale: 1, ReduceMotion: true})
	before := bytes.Clone(tt.Image().Pix)
	time.Sleep(90 * time.Millisecond)
	tt.Frame()
	if !bytes.Equal(before, tt.Image().Pix) {
		t.Fatal("reduced-motion indicator still animates")
	}
	nodes := modelUINodes(tt, "label", "下载任务活动")
	if len(nodes) != 1 || nodes[0].FieldByName("accRange").Index(2).Float() != -1 {
		t.Fatal("static activity lost unknown range")
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

func designFieldInputs(tt *ui.Tester, label string) []reflect.Value {
	var inputs []reflect.Value
	designNodes(tt, func(n reflect.Value, _ bool) {
		name := n.FieldByName("nameFrom")
		if !name.IsNil() && name.Elem().FieldByName("text").String() == label {
			inputs = append(inputs, n)
		}
	})
	return inputs
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
		if page == "dashboard" && (tt.HasText("Dashboard") || tt.HasText("首次上手")) {
			t.Fatal("incorrect dashboard loading title", tt.Texts())
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
	tt := ui.NewTester(s.view, 1120, 1200)
	click(t, tt, "新任务")
	if s.selected != "dashboard" {
		t.Fatal("empty sources navigation failed")
	}
	s.selected = "reports"
	s.historyError = &core.ApiError{Code: "internal_error"}
	s.reportsError = s.historyError
	tt.Frame()
	if tt.HasText("暂无报告") || tt.HasText("暂无运行记录") {
		t.Fatal("failed reads shown as empty success")
	}
}

func TestDesignKeyboardDetailsReturnAndHorizontalTable(t *testing.T) {
	s := testState()
	s.detailID = "two"
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.detailView(c) }) }, 800, 600)
	click(t, tt, "返回运行")
	if s.detailID != "" {
		t.Fatal("return retained detail")
	}

	s = testState()
	s.history.StandaloneRuns = []core.RunSummary{{RunId: "fixture", Path: "/fixture", Status: "complete", StartedAt: ptr("2026-10-14")}}
	tt = ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.historyView(c) }) }, 800, 600)
	before := modelUINodes(tt, "text", "2026-10-14")[0].FieldByName("x").Float()
	bounds, ok := tt.Find("运行历史表")
	if !ok {
		t.Fatal("missing table")
	}
	tt.Scroll(bounds.X+100, bounds.Y+60, 500, 0)
	after := modelUINodes(tt, "text", "2026-10-14")[0].FieldByName("x").Float()
	if after >= before {
		t.Fatal("wide table did not scroll horizontally", before, after)
	}
}

func TestDesignReportKeyboardOpenFailure(t *testing.T) {
	s := testState()
	s.selected = "reports"
	s.reports = []core.ReportSummary{{ReportId: "missing-report", Path: "/missing-report"}}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.reportsView(c) }) }, 800, 900)
	click(t, tt, "missing-report")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if s.openedReport == nil || !tt.HasText("无法读取") {
		t.Fatal("keyboard open did not show unreadable report", tt.Texts())
	}
	click(t, tt, "返回报告列表")
	if s.openedReport != nil {
		t.Fatal("return did not close report view")
	}

}

func TestDesignEngineSemanticColors(t *testing.T) {
	badges := map[string]string{
		"starting": "启动中…",
		"running":  "运行中 · PID 42 · http://127.0.0.1:8080",
		"stopping": "停止中…",
		"failed":   "失败：engine_exited · 模型服务启动后退出",
		"stopped":  "已停止",
	}
	for _, dark := range []bool{false, true} {
		for _, contrast := range []bool{false, true} {
			for _, state := range []string{"starting", "running", "stopping", "failed", "stopped"} {
				s := runStateFixture()
				s.engineState = core.EngineState{State: state, Pid: 42, Endpoint: "http://127.0.0.1:8080", Candidate: "/w/candidates/zh-core-1"}
				if state == "failed" {
					s.engineState.Error = &core.ApiError{Code: "engine_exited"}
				}
				var theme *ui.Theme
				tt := ui.NewTester(func(c *ui.Context) { theme = c.Theme(); s.runPanelView(c) }, 1000, 1400)
				tt.SetDark(dark)
				tt.SetPreferences(ui.Preferences{HighContrast: contrast, TextScale: 1, ReduceMotion: true})
				want := theme.TextMuted
				switch state {
				case "starting", "stopping":
					want = theme.Accent
				case "running":
					want = theme.Success
				case "failed":
					want = theme.Danger
				}
				assertTextColor(t, tt, badges[state], want)
			}
		}
	}
}
