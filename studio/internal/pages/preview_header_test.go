//go:build ui_debug && !release

package pages

import (
	"context"
	"testing"
	"time"
)

func TestPreviewHeaderStartsOnlyEngineAndStopsIt(t *testing.T) {
	for _, fail := range []bool{false, true} {
		p := newPreviewSession(context.Background())
		p.loadScene(previewSceneIndex(t, "任务 · 失败"))
		s := p.state
		before := s.progress.NextAction
		tt := previewTester(p, 1120, 1800)
		p.failNext = fail
		click(t, tt, "引擎 · "+stateLabel("failed"))
		click(t, tt, "开启引擎")
		if s.engineState.State != "starting" || s.engineState.Candidate != "/preview/candidate" {
			t.Fatal("header did not start frozen candidate", s.engineState)
		}
		p.advance(2 * time.Second)
		tt.Frame()
		if len(s.tasks) != 0 || s.task.stage != "bind" || s.progress.NextAction != before || s.service != nil {
			t.Fatal("starting the engine dispatched benchmark/binding or real IO")
		}
		if fail {
			if s.engineState.State != "failed" || s.failure == nil {
				t.Fatal("startup failure was hidden")
			}
			continue
		}
		if s.engineState.State != "running" {
			t.Fatal("engine did not become ready")
		}
		click(t, tt, "引擎 · 运行中")
		click(t, tt, "停止引擎")
		if s.engineState.State != "stopping" {
			t.Fatal("stop control did not dispatch")
		}
		p.advance(time.Second)
		tt.Frame()
		if s.engineState.State != "stopped" || len(s.tasks) != 0 || s.service != nil {
			t.Fatal("stop escaped preview or created a task")
		}
	}
}

func TestPreviewHeaderCanStopDuringEngineStartup(t *testing.T) {
	p := newPreviewSession(context.Background())
	p.loadScene(previewSceneIndex(t, "任务 · 失败"))
	s := p.state
	tt := previewTester(p, 1120, 1800)
	click(t, tt, "引擎 · "+stateLabel("failed"))
	click(t, tt, "开启引擎")
	if s.engineState.State != "starting" || !statusControlEnabled(tt, "停止引擎") {
		t.Fatal("starting engine cannot be stopped after startup request returns")
	}
	click(t, tt, "停止引擎")
	p.advance(2 * time.Second)
	tt.Frame()
	if s.engineState.State != "stopped" || len(s.tasks) != 0 {
		t.Fatal("cancelled startup callback restarted engine or dispatched work")
	}
}

func TestPreviewFailedStopDoesNotDiscardEngineStartup(t *testing.T) {
	p := newPreviewSession(context.Background())
	p.loadScene(previewSceneIndex(t, "任务 · 失败"))
	p.state.startManagedEngine()
	p.failNext = true
	p.state.stopEngine()
	if p.state.engineState.State != "starting" || p.state.failure == nil {
		t.Fatal("rejected stop must preserve startup")
	}
	p.advance(2 * time.Second)
	if p.state.engineState.State != "running" {
		t.Fatal("failed stop discarded pending startup")
	}
	p.state.stopEngine()
	p.advance(time.Second)
	if p.state.engineState.State != "stopped" {
		t.Fatal("retry cannot stop the engine")
	}
}

func TestPreviewDenseResourcesSurviveClockRefresh(t *testing.T) {
	p := newPreviewSession(context.Background())
	p.loadScene(previewSceneIndex(t, "资源 · 密集采样与缺测"))
	p.advance(time.Second)
	if p.state.detail == nil || len(p.state.detail.Resources) != 316 {
		t.Fatal("preview clock discarded resource detail")
	}
}
