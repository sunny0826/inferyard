package pages

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func hasStatusControl(tt *ui.Tester, label string) bool {
	_, ok := tt.Find(label)
	return ok
}

func statusControlEnabled(tt *ui.Tester, label string) bool {
	for range 12 {
		tt.Key(0, ui.KeyTab)
		if tt.Focused(label) {
			return true
		}
	}
	return false
}

func TestStatusIconsAndHoverPanel(t *testing.T) {
	s := testState()
	s.engineState = core.EngineState{State: "running", Pid: 42}
	tt := ui.NewTester(s.cornerStatus, 800, 600)
	designNodes(tt, func(n reflect.Value, _ bool) {
		if n.FieldByName("text").String() != "" {
			t.Fatal("header still renders text", n.FieldByName("text").String())
		}
	})
	icon, ok := tt.Find("引擎 · 运行中")
	if !ok {
		t.Fatal("engine icon lacks accessible status")
	}
	tt.Move(icon.X+icon.W/2, icon.Y+icon.H/2)
	if !tt.HasText("PID 42") || !tt.HasText("停止引擎") {
		t.Fatal("hover does not reveal engine controls", tt.Texts())
	}
	stop, ok := tt.Find("停止引擎")
	if !ok {
		t.Fatal("missing stop")
	}
	tt.Move(stop.X+stop.W/2, stop.Y+stop.H/2)
	tt.Advance(time.Second)
	if !tt.HasText("停止引擎") {
		t.Fatal("panel closed while moving into its controls")
	}
	tt.Move(1, 580)
	tt.Advance(time.Second)
	tt.Frame()
	if tt.HasText("停止引擎") {
		t.Fatal("panel remains after pointer leaves")
	}
	click(t, tt, "引擎 · 运行中")
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if tt.HasText("停止引擎") {
		t.Fatal("Escape did not dismiss panel")
	}
	tt.Move(1, 580)
	for range 4 {
		tt.Key(0, ui.KeyTab)
		if tt.Focused("引擎 · 运行中") {
			break
		}
	}
	tt.Key(0, ui.KeyEnter)
	if !tt.HasText("模型引擎") {
		t.Fatal("keyboard cannot open engine panel", tt.Texts())
	}
}

func TestStatusRefreshAndQueueControls(t *testing.T) {
	for _, state := range []string{"queued", "running", "cancelling", "done", "failed", "cancelled"} {
		t.Run(state, func(t *testing.T) {
			s := testState()
			s.engineState.State = "running"
			s.tasks = []core.RunTask{{TaskId: "task", State: state}}
			tt := ui.NewTester(s.cornerStatus, 800, 600)
			before := slices.Clone(tt.Texts())
			s.refreshing = true
			tt.Frame()
			if !slices.Equal(before, tt.Texts()) {
				t.Fatal("refresh changed header")
			}
			click(t, tt, "引擎 · 运行中")
			if !statusControlEnabled(tt, "停止引擎") != !terminal(state) {
				t.Fatal("stop availability does not follow task lifetime", state)
			}
			s.busy = true
			tt.Frame()
			if statusControlEnabled(tt, "停止引擎") {
				t.Fatal("busy engine stop remains available")
			}
		})
	}
}

func TestManagedEngineStartRequiresCandidateAndIdleQueue(t *testing.T) {
	s := testState()
	s.selected, s.detailID = "reports", "previous-detail"
	tt := ui.NewTester(s.cornerStatus, 800, 600)
	click(t, tt, "引擎 · 已停止")
	if statusControlEnabled(tt, "开启引擎") || !tt.HasText("前往任务") {
		t.Fatal("missing candidate should guide setup")
	}
	click(t, tt, "前往任务")
	if s.selected != "dashboard" || s.detailID != "" {
		t.Fatal("setup guidance reopened an old run detail")
	}
	s.candidate = "/fixture/candidate"
	tt.Frame()
	if !statusControlEnabled(tt, "开启引擎") {
		t.Fatal("prepared candidate cannot start")
	}
	s.tasks = []core.RunTask{{TaskId: "waiting", State: "starting", Queued: true}}
	tt.Frame()
	if statusControlEnabled(tt, "开启引擎") {
		t.Fatal("waiting queue allows engine restart")
	}
	s.tasks = nil
	s.newTaskPending = true
	if s.canStartManagedEngine() {
		t.Fatal("reset permits start")
	}
}

func TestQueueCountsSpeedOnlyOnce(t *testing.T) {
	s := testState()
	s.speed = &speedWorkspace{current: &speedTask{id: "speed", stage: speedMeasuring}}
	s.tasks = []core.RunTask{
		{TaskId: "speed", State: "running", Kind: "context_speed"},
		{TaskId: "waiting", State: "starting", Queued: true},
		{TaskId: "finished", State: "done"},
	}
	if got := s.queueActivity(); got != (queueActivity{1, 1}) {
		t.Fatal(got)
	}
	s.tasks = nil
	s.speed.current.stage = speedQueued
	if got := s.queueActivity(); got != (queueActivity{0, 1}) {
		t.Fatal(got)
	}
}

func TestStatusMotionHonorsReducedMotion(t *testing.T) {
	s := testState()
	s.engineState.State = "running"
	tt := ui.NewTester(s.cornerStatus, 800, 200)
	before := slices.Clone(tt.Image().Pix)
	tt.Advance(350 * time.Millisecond)
	if slices.Equal(before, tt.Image().Pix) {
		t.Fatal("active engine has no visible animation")
	}
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	before = slices.Clone(tt.Image().Pix)
	tt.Advance(350 * time.Millisecond)
	if !slices.Equal(before, tt.Image().Pix) {
		t.Fatal("reduced motion still animates")
	}
}
