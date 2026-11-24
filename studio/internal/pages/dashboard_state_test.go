package pages

import (
	"context"
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
