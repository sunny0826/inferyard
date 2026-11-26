//go:build ui_debug && !release

package pages

import (
	"context"
	"testing"
	"time"

	"inferyard/studio/internal/core"
)

func TestPreviewStandaloneStartStopAndHandoff(t *testing.T) {
	p := newPreviewSession(context.Background())
	// Scene 0 (task_preparation) has CLI not_found -> serveSupported must be false until prepared.
	if p.state.serveSupported {
		t.Fatalf("expected task_preparation scene to have serveSupported=false while CLI is not_found")
	}
	p.prepareEnvironment()
	p.advance(700 * time.Millisecond)
	if !p.state.serveSupported {
		t.Fatalf("expected serveSupported=true after prepareEnvironment completes")
	}

	// Switch to task_ready scene where models and engines are available.
	p.loadScene(previewSceneIndex(t, "测评 · 待运行"))
	s := p.state
	if !s.serveSupported {
		t.Fatalf("expected task_ready scene serveSupported=true")
	}
	model := s.registry.Models[0]
	engine := s.registry.Engines[0]

	// Start standalone in preview: preparing -> starting -> running.
	s.launchStandalone(model.Name, engine.Name, 12000)
	if !s.standalone.isPreparing(model.Name) {
		t.Fatalf("expected preparing state, got %+v", s.standalone)
	}
	p.advance(350 * time.Millisecond)
	if s.standalone.isPreparing(model.Name) || s.engineState.State != "starting" || s.engineState.Origin != core.EngineOriginStandalone {
		t.Fatalf("expected starting standalone state, got %+v", s.engineState)
	}
	p.advance(550 * time.Millisecond)
	if s.engineState.State != "running" || s.engineState.ContextSize != 12000 || s.engineState.Endpoint == "" {
		t.Fatalf("expected running standalone state, got %+v", s.engineState)
	}

	// Stop standalone via previewStopEngineThen: callback runs only after stopped, and Origin/Model are preserved.
	called := false
	s.stopActiveServiceThen(func() { called = true })
	if called || s.engineState.State != "stopping" {
		t.Fatalf("expected stopping without premature callback, called=%v state=%q", called, s.engineState.State)
	}
	p.advance(450 * time.Millisecond)
	if !called || s.engineState.State != "stopped" || s.engineState.Origin != core.EngineOriginStandalone || s.engineState.Model == "" {
		t.Fatalf("expected stopped standalone state with callback executed, called=%v state=%+v", called, s.engineState)
	}

	// Simulated failure on stop must NOT run callback.
	s.engineState.State = "running"
	p.failNext = true
	calledAfterFail := false
	s.stopActiveServiceThen(func() { calledAfterFail = true })
	p.advance(500 * time.Millisecond)
	if calledAfterFail || s.failure == nil {
		t.Fatalf("expected failed stop to skip callback, called=%v failure=%+v", calledAfterFail, s.failure)
	}

	// Local engines scene: vLLM uses local_openai_v1 + python, and model row matches running state.
	p.loadScene(previewSceneIndex(t, "模型库 · 本地安装与目录模型"))
	s = p.state
	var dirModel core.ModelAsset
	var vllmEngine core.EngineAsset
	for _, m := range s.registry.Models {
		if m.Format == "directory" {
			dirModel = m
		}
	}
	for _, e := range s.registry.Engines {
		if e.Family == "vllm" {
			vllmEngine = e
		}
	}
	s.launchStandalone(dirModel.Name, vllmEngine.Name, 8192)
	p.advance(900 * time.Millisecond)
	if s.engineState.State != "running" || s.engineState.Adapter != "local_openai_v1" || s.engineState.Executable != "python" {
		t.Fatalf("unexpected vLLM preview state: %+v", s.engineState)
	}
	if rowState, _, _, _, _ := s.standaloneRowState(dirModel); rowState != "running" {
		t.Fatalf("expected directory model row to match running state, got %q", rowState)
	}

	// Simulated failure on standalone start transitions preparing -> failed with Origin=standalone and no started toast.
	s.engineState = core.EngineState{State: "stopped"}
	s.pendingToasts = nil
	p.failNext = true
	s.launchStandalone(dirModel.Name, vllmEngine.Name, 8192)
	if !s.standalone.isPreparing(dirModel.Name) {
		t.Fatalf("expected preparing state before failure")
	}
	if len(s.pendingToasts) != 0 {
		t.Fatalf("expected no premature started toast during preparing, got %+v", s.pendingToasts)
	}
	p.advance(350 * time.Millisecond)
	if s.standalone.isPreparing(dirModel.Name) || s.engineState.State != "failed" || s.engineState.Origin != core.EngineOriginStandalone || s.standalone.lastErrors[dirModel.Name] == nil {
		t.Fatalf("expected failed standalone state after preparing failure, engine=%+v standalone=%+v", s.engineState, s.standalone)
	}
	if len(s.pendingToasts) != 0 {
		t.Fatalf("expected no started toast on failed start, got %+v", s.pendingToasts)
	}
}
