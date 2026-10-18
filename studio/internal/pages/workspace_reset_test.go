package pages

import (
	"context"
	"sync"
	"testing"
	"time"

	"inferyard/studio/internal/core"
)

func TestNewTaskResetWaitsForUIAndLeavesNextTaskAlone(t *testing.T) {
	tasks := productionResetThenNextTask(t, nil)
	if len(tasks) != 1 || tasks[0].State != "starting" {
		t.Fatal("new task was changed by previous reset", tasks)
	}
}

func productionResetThenNextTask(t *testing.T, initial []core.RunTask) []core.RunTask {
	t.Helper()
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	var mu sync.Mutex
	tasks := append([]core.RunTask(nil), initial...)
	updates := make(chan func(), 8)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		awaitBenchmarkReset(ctx, func() []core.RunTask {
			mu.Lock()
			defer mu.Unlock()
			return append([]core.RunTask(nil), tasks...)
		}, func(id string) (core.RunTask, *core.ApiError) {
			mu.Lock()
			defer mu.Unlock()
			for i := range tasks {
				if tasks[i].TaskId == id {
					tasks[i].State, tasks[i].Queued = "cancelled", false
					return tasks[i], nil
				}
			}
			return core.RunTask{}, core.Err("not_found")
		}, func(fn func()) { updates <- fn }, func(e *core.ApiError) bool {
			if e != nil {
				t.Errorf("reset failed: %v", e)
			}
			mu.Lock()
			tasks = append(tasks, core.RunTask{TaskId: "next-specialty", State: "starting"})
			mu.Unlock()
			return true
		})
	}()
	var apply func()
	select {
	case apply = <-updates:
	case <-time.After(time.Second):
		t.Fatal("reset did not schedule UI completion")
	}
	select {
	case <-updates:
		stop()
		<-finished
		t.Fatal("reset scheduled another cancellation pass before UI completion")
	case <-time.After(250 * time.Millisecond):
	}
	apply()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("reset continued after UI completion")
	}
	mu.Lock()
	defer mu.Unlock()
	return append([]core.RunTask(nil), tasks...)
}
