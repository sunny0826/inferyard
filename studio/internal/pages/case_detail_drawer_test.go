package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func TestDetailCaseClickShowsPromptAndOptionalAnswer(t *testing.T) {
	s := readyState("benchmarks")
	s.detailID = "task-1"
	s.detail = &core.RunTaskDetail{
		Task: core.RunTask{TaskId: "task-1", State: "running", Bundle: ptr("zh-core")},
		Progress: &core.RunProgress{
			Done:  1,
			Total: ptr(int64(2)),
			Cases: []core.CaseProgress{
				{
					CaseId:       "case-answered",
					State:        "completed",
					QualityState: ptr("pass"),
					Prompt:       ptr("Prompt for answered case"),
					Response:     ptr("Model output for answered case"),
				},
				{
					CaseId: "case-running",
					State:  "running",
					Prompt: ptr("Prompt for running case without answer"),
				},
			},
		},
	}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).FillWidth().Children(func() { s.detailView(c) })
	}, 1120, 960)
	tt.Frame()

	// Click case without answer: only prompt is shown, no response section.
	click(t, tt, "case-running")
	tt.Frame()
	if !tt.HasText("Prompt for running case without answer") {
		t.Fatalf("missing prompt for unanswered case: %v", tt.Texts())
	}
	if tt.HasText(tr(nil, "detail.case_drawer.response_title")) {
		t.Fatalf("unexpected response section when answer is missing: %v", tt.Texts())
	}

	// Switch to previous case using the in-drawer navigator: both prompt and response appear.
	click(t, tt, tr(nil, "detail.case_drawer.prev_case"))
	tt.Frame()
	if s.detailCaseID != "case-answered" {
		t.Fatalf("prev_case left detailCaseID=%q, want case-answered", s.detailCaseID)
	}
	if !tt.HasText("Prompt for answered case") || !tt.HasText("Model output for answered case") {
		t.Fatalf("missing prompt or response for answered case: %v", tt.Texts())
	}
	if !tt.HasText(tr(nil, "detail.case_drawer.response_title")) {
		t.Fatalf("missing response section title for answered case: %v", tt.Texts())
	}
}

func TestReportCaseClickShowsPromptAndOptionalAnswer(t *testing.T) {
	s := testState()
	s.service, _ = reportFixtureService(t)
	s.openReport("summary-1")

	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).FillWidth().Children(func() { s.reportView(c) })
	}, 1200, 1600)
	tt.Frame()

	// null-case has prompt and null response -> only prompt should render.
	click(t, tt, "null-case")
	tt.Frame()
	if !tt.HasText("计算 128 × 37 并只输出最终整数。") {
		t.Fatalf("missing prompt for null-case: %v", tt.Texts())
	}
	if tt.HasText(tr(nil, "detail.case_drawer.response_title")) {
		t.Fatalf("unexpected response section for null-case: %v", tt.Texts())
	}

	// timed-case has both prompt and response -> both should render.
	s.openedReport.detailCaseIndex = 1
	s.openedReport.detailCaseOpen = true
	tt.Frame()
	if !tt.HasText("HTTP 状态码 429 表示什么含义？用一句话说明。") || !tt.HasText("429 Too Many Requests 表示客户端在规定时间内发送了过多请求，触发了服务端限流。") {
		t.Fatalf("missing prompt or response for timed-case: %v", tt.Texts())
	}
}
