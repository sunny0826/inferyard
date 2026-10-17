//go:build ui_debug && !release

package pages

import (
	"context"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

// These scenarios begin with a prepared candidate. The production driver runs
// runBenchmarkFlow against scripted service responses; the preview driver runs
// the independent simulator. Neither driver supplies the expected observations.
type parityScenario struct {
	name      string
	bundles   []string
	failStart bool
	cancelAt  string
}

type parityFrame struct {
	Checkpoint string
	Heading    string
	Error      string
	Controls   []string
	Runs       []string
	Report     []string
}

func TestPreviewBenchmarkParity(t *testing.T) {
	for _, scenario := range []parityScenario{
		{name: "core", bundles: []string{"zh-core"}},
		{name: "pelican", bundles: []string{"zh-svg-pelican"}},
		{name: "both", bundles: []string{"zh-core", "zh-svg-pelican"}},
		{name: "startup_failure_retry", bundles: []string{"zh-core", "zh-svg-pelican"}, failStart: true},
		{name: "cancel_binding", bundles: []string{"zh-core", "zh-svg-pelican"}, cancelAt: "bind"},
		{name: "cancel_running_and_queued", bundles: []string{"zh-core", "zh-svg-pelican"}, cancelAt: "run"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			production := runProductionParity(t, scenario)
			preview := runPreviewParity(t, scenario)
			want := parityExpectation(scenario)
			for name, got := range map[string][]parityFrame{"production": production, "preview": preview} {
				if !reflect.DeepEqual(got, want) {
					t.Errorf("%s contract mismatch\n got: %+v\nwant: %+v", name, got, want)
				}
			}
			if !reflect.DeepEqual(production, preview) {
				t.Errorf("behavior drift\nproduction: %+v\n   preview: %+v", production, preview)
			}
		})
	}
}

func parityExpectation(s parityScenario) []parityFrame {
	bind := parityFrame{Checkpoint: "bind", Heading: "启动与绑定"}
	want := []parityFrame{bind}
	if s.failStart {
		want = append(want, parityFrame{Checkpoint: "failed", Heading: "启动与绑定", Error: "engine_start_failed", Controls: []string{"启动服务"}}, bind)
	}
	if s.cancelAt == "bind" {
		return append(want, parityFrame{Checkpoint: "cancelled"})
	}
	var running, done, cancelled []string
	for i, bundle := range s.bundles {
		state := "queued"
		if i == 0 {
			state = "running"
		}
		running = append(running, bundle+":"+state)
		done = append(done, bundle+":done")
		cancelled = append(cancelled, bundle+":cancelled")
	}
	want = append(want, parityFrame{Checkpoint: "run", Heading: "运行", Controls: []string{"查看实时详情", "取消运行"}, Runs: running})
	if s.cancelAt == "run" {
		return append(want, parityFrame{Checkpoint: "cancelled", Runs: cancelled})
	}
	if len(s.bundles) == 2 {
		want = append(want, parityFrame{Checkpoint: "next_run", Heading: "运行", Controls: []string{"查看实时详情", "取消运行"}, Runs: []string{s.bundles[0] + ":done", s.bundles[1] + ":running"}})
	}
	want = append(want, parityFrame{Checkpoint: "report", Heading: "报告", Runs: done})
	return append(want, parityFrame{Checkpoint: "done", Heading: "报告", Controls: []string{"查看报告"}, Runs: done, Report: s.bundles})
}

func observeParity(s *viewState, checkpoint string, report []string) parityFrame {
	frame := parityFrame{Checkpoint: checkpoint, Report: report}
	for _, task := range s.tasks {
		if task.TaskId != "unrelated" {
			state := task.State
			if task.Queued && !terminal(state) {
				state = "queued"
			}
			frame.Runs = append(frame.Runs, emptyText(task.Bundle)+":"+state)
		}
	}
	if checkpoint == "cancelled" {
		return frame
	}
	tt := ui.NewTester(s.stageView, 1120, 1800)
	for _, heading := range []string{"启动与绑定", "运行", "报告"} {
		if slices.Contains(tt.Texts(), heading) {
			frame.Heading = heading
		}
	}
	labels := []string{"启动服务", "查看实时详情", "取消运行", "查看报告"}
	enabled := map[string]bool{}
	for range 40 {
		tt.Key(0, ui.KeyTab)
		for _, label := range labels {
			if tt.Focused(label) {
				enabled[label] = true
			}
		}
	}
	for _, label := range labels {
		if enabled[label] {
			frame.Controls = append(frame.Controls, label)
		}
	}
	if s.failure != nil {
		frame.Error = s.failure.Code
	}
	return frame
}

func runPreviewParity(t *testing.T, scenario parityScenario) []parityFrame {
	t.Helper()
	p := newPreviewSession(context.Background())
	p.loadScene(previewSceneIndex(t, "模型 · 可用组合"))
	s := p.state
	s.tasks = []core.RunTask{{TaskId: "unrelated", State: "done", Bundle: ptr("unrelated"), RunPath: ptr("/unrelated")}}
	s.selectedBundles = map[string]bool{}
	for _, bundle := range scenario.bundles {
		s.selectedBundles[bundle] = true
	}
	p.failNext = scenario.failStart
	s.startBenchmark()
	var frames []parityFrame
	seen := map[string]bool{}
	for range 1000 {
		p.advance(100 * time.Millisecond)
		checkpoint := s.task.stage
		if checkpoint == "run" && s.running() != nil && emptyText(s.running().Bundle) != scenario.bundles[0] {
			checkpoint = "next_run"
		}
		if s.task.failure != nil {
			checkpoint = "failed"
		}
		if !slices.Contains([]string{"bind", "run", "next_run", "report", "done", "failed"}, checkpoint) || seen[checkpoint] {
			continue
		}
		seen[checkpoint] = true
		var report []string
		if checkpoint == "done" {
			index, e := p.report(s.task.report)
			if e != nil {
				t.Fatal(e)
			}
			for _, run := range index.Runs {
				report = append(report, run.Facets.BundleLabel)
			}
		}
		frames = append(frames, observeParity(s, checkpoint, report))
		if checkpoint == scenario.cancelAt {
			s.newBenchmark()
			p.advance(time.Minute)
			if s.task.report != "" || len(p.indexes) != 0 || s.busy || s.newTaskPending {
				t.Fatal("cancelled preview retained work or generated a report")
			}
			return append(frames, observeParity(s, "cancelled", nil))
		}
		if checkpoint == "failed" {
			p.advance(time.Minute)
			if s.task.failure == nil || len(p.indexes) != 0 {
				t.Fatal("failed preview retried without a user action")
			}
			s.retryBenchmark()
			delete(seen, "bind")
		}
		if checkpoint == "done" {
			return frames
		}
	}
	t.Fatal("preview did not reach a terminal checkpoint", frames)
	return nil
}
