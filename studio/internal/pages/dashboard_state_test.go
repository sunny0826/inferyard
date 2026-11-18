package pages

import (
	"context"
	"slices"

	"testing"

	"inferyard/studio/internal/core"
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
