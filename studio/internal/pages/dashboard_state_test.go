package pages

import (
	"context"
	"image/color"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func TestDashboardCancelTerminalRaceStillCancelsRemaining(t *testing.T) {
	tasks := []core.RunTask{{TaskId: "finishing", State: "running"}, {TaskId: "queued", State: "starting"}}
	calls := []string{}
	e := cancelBenchmarkRuns(context.Background(), func() []core.RunTask { return append([]core.RunTask(nil), tasks...) }, func(id string) (core.RunTask, *core.ApiError) {
		calls = append(calls, id)
		if id == "finishing" {
			tasks[0].State = "done"
			return core.RunTask{}, core.Conflict("invalid_state")
		}
		tasks[1].State = "cancelled"
		return tasks[1], nil
	})
	if e != nil || !slices.Equal(calls, []string{"finishing", "queued"}) || tasks[1].State != "cancelled" {
		t.Fatal(e, calls, tasks)
	}
}

func TestActiveRunTaskPrefersDequeuedTaskAndRunCardShowsBaselineThenCases(t *testing.T) {
	// Service.RunTasks() returns reverse-insertion order: [task2(Queued:true), task1(Queued:false)].
	tasks := []core.RunTask{
		{TaskId: "pelican-queued", State: "starting", Queued: true, Bundle: ptr("zh-svg-pelican")},
		{TaskId: "core-active", State: "starting", Queued: false, PreparationStage: "starting_engine", Bundle: ptr("zh-core")},
	}
	got := activeRunTask(tasks)
	if got == nil || got.TaskId != "core-active" {
		t.Fatalf("expected activeRunTask to select dequeued task1, got %+v", got)
	}

	// When all non-terminal tasks are still queued, fall back to the earliest-enqueued head at the tail of RunTasks().
	allQueued := []core.RunTask{
		{TaskId: "pelican-queued", State: "starting", Queued: true},
		{TaskId: "core-queued", State: "starting", Queued: true},
	}
	if head := activeRunTask(allQueued); head == nil || head.TaskId != "core-queued" {
		t.Fatalf("expected queue head fallback core-queued, got %+v", head)
	}

	// Verify runView renders preparation stage, baseline phase, and formal case progress for the active task.
	s := readyState("dashboard")
	s.tasks = tasks
	tt := ui.NewTester(s.runView, 800, 400)
	prepText := l10n.T(l10n.LanguageZhHans, "common.task.prep.starting_engine")
	if !tt.HasText(prepText) {
		t.Fatalf("expected runView to show preparation stage %q, got %v", prepText, tt.Texts())
	}

	// Transition active task to baseline phase (0/120, CurrentCaseId == nil).
	s.tasks[1].State = "running"
	s.tasks[1].PreparationStage = ""
	s.runProgress = &core.RunProgress{Phase: ptr("baseline"), Done: 0, Total: ptr(int64(120))}
	tt.Frame()
	baselineLabel := l10n.Tf(l10n.LanguageZhHans, "dashboard.run.cases_progress", "done", 0, "total", 120) + " · " + l10n.T(l10n.LanguageZhHans, "common.state.baseline")
	if !tt.HasText(baselineLabel) {
		t.Fatalf("expected runView to display baseline phase %q, got %v", baselineLabel, tt.Texts())
	}

	// Once formal case starts, phase suffix disappears and case progress is shown.
	s.runProgress = &core.RunProgress{Phase: ptr("formal"), Done: 1, Total: ptr(int64(120)), CurrentCaseId: ptr("instruction-02")}
	tt.Frame()
	casesLabel := l10n.Tf(l10n.LanguageZhHans, "dashboard.run.cases_progress", "done", 1, "total", 120)
	if !tt.HasText(casesLabel) || strings.Contains(strings.Join(tt.Texts(), "\n"), "· "+l10n.T(l10n.LanguageZhHans, "common.state.formal")) {
		t.Fatalf("expected formal case progress %q without pre-formal phase suffix, got %v", casesLabel, tt.Texts())
	}
}

func TestBenchmarkWorkflowRunDetailAndSpeedLayoutPolish(t *testing.T) {
	s := readyState("benchmarks")
	s.benchFlowOpen = true
	s.task.stage = "run"
	s.tasks = []core.RunTask{{
		TaskId: "task-1",
		Kind:   "run",
		State:  "running",
		Bundle: ptr("zh-core"),
	}}
	s.runProgress = &core.RunProgress{
		Phase:         ptr("formal"),
		Done:          30,
		Total:         ptr(int64(120)),
		CurrentCaseId: ptr("case-031"),
	}

	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).FillWidth().Children(func() { s.benchmarksView(c) }) }, 1120, 1000)
	tt.Frame()

	if !tt.HasText("25%") || !tt.HasText("当前题：case-031") {
		t.Fatalf("expected live run percentage and current case chip, got %v", tt.Texts())
	}
	detailBtn, ok1 := tt.AccessByRole(ui.RoleButton, "查看实时详情")
	cancelBtn, ok2 := tt.AccessByRole(ui.RoleButton, "取消任务")
	if !ok1 || !ok2 {
		t.Fatalf("missing run action buttons: detail=%v cancel=%v", ok1, ok2)
	}
	if d := detailBtn.Bounds.Y - cancelBtn.Bounds.Y; d < -1 || d > 1 {
		t.Fatalf("run action buttons stacked vertically instead of sharing one row: detailY=%.1f cancelY=%.1f", detailBtn.Bounds.Y, cancelBtn.Bounds.Y)
	}
	if d := detailBtn.Bounds.H - cancelBtn.Bounds.H; d < -1 || d > 1 {
		t.Fatalf("run action button heights differ: detailH=%.1f cancelH=%.1f", detailBtn.Bounds.H, cancelBtn.Bounds.H)
	}

	s.detailID = "task-1"
	s.detail = &core.RunTaskDetail{
		Task: s.tasks[0],
		Progress: &core.RunProgress{
			Phase:         ptr("formal"),
			Done:          30,
			Total:         ptr(int64(120)),
			CurrentCaseId: ptr("case-031"),
			Cases: []core.CaseProgress{
				{CaseId: "case-001", State: "done", QualityState: ptr("pass")},
			},
		},
	}
	ttDetail := ui.NewTester(func(c *ui.Context) { ui.Column(c).FillWidth().Children(func() { s.detailView(c) }) }, 1120, 1000)
	ttDetail.Frame()
	if !ttDetail.HasText("25.0%") {
		t.Fatalf("expected detail view percentage 25.0%%, got %v", ttDetail.Texts())
	}

	s.detailID = ""
	s.speed = &speedWorkspace{
		current: &speedTask{
			id:        "speed-1",
			model:     "Preview-Model-Q4.gguf",
			engine:    "llama-server",
			context:   65536,
			stage:     speedMeasuring,
			completed: 2,
		},
	}
	ttSpeed := ui.NewTester(func(c *ui.Context) { ui.Column(c).FillWidth().Children(func() { s.benchmarksView(c) }) }, 1120, 1000)
	ttSpeed.Frame()
	for _, want := range []string{"40%", "#1", "#2", "#5"} {
		if !ttSpeed.HasText(want) {
			t.Fatalf("expected speed task view to show %q, got %v", want, ttSpeed.Texts())
		}
	}
}

func TestHandoffConnectExplainsDisabledState(t *testing.T) {
	s := readyState("benchmarks")
	s.tasks = []core.RunTask{{TaskId: "next", State: "starting", AwaitingService: true}}
	s.localBindingTask = "next"
	s.startup.Endpoint = "http://127.0.0.1:1234"
	s.localUsedPIDs = map[int64]bool{41: true}
	pidHint := l10n.TR("dashboard.handoff.select_pid")
	endpointHint := l10n.TRf("dashboard.handoff.match_endpoint", "endpoint", s.startup.Endpoint)
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.awaitingLocalServiceView(c) }) }, 960, 900)
	for _, tc := range []struct{ pid, endpoint, reason string }{
		{"", s.startup.Endpoint, pidHint},
		{"abc", s.startup.Endpoint, pidHint},
		{"9999999999999999999999", s.startup.Endpoint, pidHint},
		{"42", "http://127.0.0.1:8080", endpointHint},
		{"41", s.startup.Endpoint, human("local_engine_service_reused")},
		{"42", s.startup.Endpoint, ""},
	} {
		s.localPID, s.localEndpoint = tc.pid, tc.endpoint
		tt.Frame()
		info, ok := tt.AccessByRole(ui.RoleButton, l10n.TR("engine.local.handoff.connect_continue"))
		if !ok || info.Disabled != (tc.reason != "") || info.Description != tc.reason {
			t.Fatalf("PID %q endpoint %q: %+v, want reason %q", tc.pid, tc.endpoint, info, tc.reason)
		}
		if tc.reason != "" && !tt.HasText(tc.reason) {
			t.Fatalf("missing visible reason %q: %v", tc.reason, tt.Texts())
		}
	}
}

func TestRunDetailButtonGivesPrimaryActionToHandoff(t *testing.T) {
	s := readyState("benchmarks")
	s.tasks = []core.RunTask{{TaskId: "run", State: "starting"}}
	detail := l10n.TR("dashboard.run.view_live_detail")
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Children(func() {
			s.runView(c)
			primaryButton(c, "primary")
			outlineButton(c, "outline")
		})
	}, 960, 600)
	for _, tc := range []struct {
		waiting bool
		style   string
	}{{false, "primary"}, {true, "outline"}} {
		s.tasks[0].AwaitingService = tc.waiting
		tt.Frame()
		img := tt.Image()
		fill := func(label string) color.Color {
			t.Helper()
			button, ok := tt.AccessByRole(ui.RoleButton, label)
			if !ok {
				t.Fatalf("missing button %q", label)
			}
			r := button.Bounds
			return img.At(int(r.X+r.W/2), int(r.Y+4))
		}
		if fill("primary") == fill("outline") || fill(detail) != fill(tc.style) {
			t.Fatalf("waiting=%t: expected %s live-detail button", tc.waiting, tc.style)
		}
	}
}
