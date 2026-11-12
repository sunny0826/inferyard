//go:build ui_debug && !release

package pages

import (
	"context"
	"slices"
	"testing"
	"time"

	"inferyard/studio/internal/core"
)

// Both preview bind entry points must dispatch the simulated bind. The window
// must stay in memory, but the recorded progress and service state must show
// the bind happened; a bind that silently does nothing leaves the task stuck at
// "bind" without reporting anything.
func TestPreviewBindDispatchHasVisibleEffect(t *testing.T) {
	t.Run("discovered service", func(t *testing.T) {
		p := previewBindPending(t, "/preview/workspace")
		p.state.bindDiscovered(42, "http://127.0.0.1:48857")
		p.advance(time.Minute)
		assertPreviewBindLanded(t, p)
	})

	t.Run("manual bind command", func(t *testing.T) {
		p := previewBindPending(t, "/preview/workspace")
		command, e := p.state.bindCommand("6124", "http://127.0.0.1:8081")
		if command != "" || e != nil {
			t.Fatalf("preview bind built a command or failed: %q %v", command, e)
		}
		p.advance(time.Minute)
		assertPreviewBindLanded(t, p)
	})
}

func previewBindPending(t *testing.T, root string) *previewSession {
	t.Helper()
	p := newPreviewSession(context.Background())
	p.loadScene(previewSceneIndex(t, "模型 · 可用组合"))
	s := p.state
	s.progress = bindingParityProgress(root)
	s.candidate = *s.progress.Bundles[0].CandidatePath
	s.tasks, s.task.bundles = nil, nil
	s.engineState = core.EngineState{State: "stopped"}
	return p
}

func assertPreviewBindLanded(t *testing.T, p *previewSession) {
	t.Helper()
	s := p.state
	if s.service != nil || s.taskCancel != nil || s.binding || s.busy {
		t.Fatalf("preview bind left real work behind: service=%v cancel=%v binding=%v busy=%v",
			s.service != nil, s.taskCancel != nil, s.binding, s.busy)
	}
	if s.failure != nil {
		t.Fatalf("preview bind reported %v", s.failure)
	}
	if s.engineState.State != "running" || s.engineState.Pid != 42 {
		t.Fatalf("preview bind did not leave the simulated service running: %+v", s.engineState)
	}
	if s.progress.NextAction != "run" {
		t.Fatalf("preview bind left next action %q, want run", s.progress.NextAction)
	}
	want := []string{"run_missing", "run_missing"}
	if got := bindingParityPhases(s.progress); !slices.Equal(got, want) {
		t.Fatalf("preview bind phases = %v, want %v", got, want)
	}
}
