package core

import (
	"context"
	"encoding/json"
	"net"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestServicePresetsPersistAndUpdateByModelEngine(t *testing.T) {
	var calls atomic.Int32
	s, _, model, engine := setupStandaloneTestService(t, true, &calls, nil)

	if got := s.RecentServices(); len(got) != 0 {
		t.Fatalf("expected empty recent services initially, got %+v", got)
	}

	gpu := 32
	threads := 8
	if err := s.SaveServicePreset(ServicePreset{
		Model:         model,
		Engine:        engine,
		Port:          49101,
		ContextSize:   16384,
		GPULayers:     &gpu,
		Threads:       &threads,
		ExtraArgs:     []string{"--flash-attn"},
		LastStartedAt: "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("SaveServicePreset failed: %v", err)
	}
	if err := s.SaveServicePreset(ServicePreset{
		Model:         "other.gguf",
		Engine:        engine,
		Port:          49102,
		ContextSize:   8192,
		LastStartedAt: "2026-01-02T00:00:00Z",
	}); err != nil {
		t.Fatalf("SaveServicePreset 2nd failed: %v", err)
	}

	recent := s.RecentServices()
	if len(recent) != 2 || recent[0].Model != "other.gguf" || recent[1].Model != model {
		t.Fatalf("unexpected recent order: %+v", recent)
	}

	// Update existing (model, engine) with newer timestamp and different params.
	gpu2 := 40
	if err := s.SaveServicePreset(ServicePreset{
		Model:         model,
		Engine:        engine,
		Port:          49103,
		ContextSize:   32768,
		GPULayers:     &gpu2,
		ExtraArgs:     []string{"--mlock"},
		LastStartedAt: "2026-01-03T00:00:00Z",
	}); err != nil {
		t.Fatalf("SaveServicePreset update failed: %v", err)
	}

	got, ok := s.ServicePresetFor(model, engine)
	if !ok {
		t.Fatalf("expected preset for (%s, %s)", model, engine)
	}
	if got.Port != 49103 || got.ContextSize != 32768 || got.GPULayers == nil || *got.GPULayers != 40 || len(got.ExtraArgs) != 1 || got.ExtraArgs[0] != "--mlock" {
		t.Fatalf("unexpected updated preset: %+v", got)
	}
	recent = s.RecentServices()
	if len(recent) != 2 || recent[0].Model != model {
		t.Fatalf("expected updated preset first, got %+v", recent)
	}
}

func TestStartStandaloneExplicitPortConflictAndPresetAutoSave(t *testing.T) {
	var calls atomic.Int32
	s, _, model, engine := setupStandaloneTestService(t, true, &calls, nil)

	// Occupy a port and verify StartStandalone rejects it instead of silently switching.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	occupiedPort := ln.Addr().(*net.TCPAddr).Port
	defer ln.Close()

	if _, apiErr := s.StartStandalone(context.Background(), StandaloneStartRequest{
		Model:       model,
		Engine:      engine,
		ContextSize: 8192,
		Port:        occupiedPort,
	}); apiErr == nil || apiErr.Code != "engine_port_in_use" {
		t.Fatalf("expected engine_port_in_use for occupied explicit port, got %+v", apiErr)
	}
	if calls.Load() != 0 {
		t.Fatalf("expected 0 serve prepare calls on explicit port conflict, got %d", calls.Load())
	}

	// Start with a free explicit port and advanced args.
	wantPort := freePort(t)
	gpu := 24
	threads := 6
	st, apiErr := s.StartStandalone(context.Background(), StandaloneStartRequest{
		Model:       model,
		Engine:      engine,
		ContextSize: 8192,
		Port:        wantPort,
		GPULayers:   &gpu,
		Threads:     &threads,
		ExtraArgs:   []string{"--flash-attn"},
	})
	if apiErr != nil {
		t.Fatalf("StartStandalone with explicit port failed: %+v", apiErr)
	}
	st = waitEngineState(t, s, "running")
	if st.Port != wantPort || st.Endpoint != "http://127.0.0.1:"+strconv.Itoa(wantPort) {
		t.Fatalf("unexpected port/endpoint: %+v", st)
	}

	preset, ok := s.ServicePresetFor(model, engine)
	if !ok {
		t.Fatalf("expected preset auto-saved after StartStandalone")
	}
	if preset.Port != wantPort || preset.ContextSize != 8192 || preset.GPULayers == nil || *preset.GPULayers != 24 || preset.Threads == nil || *preset.Threads != 6 {
		t.Fatalf("unexpected auto-saved preset: %+v", preset)
	}
	if len(preset.ExtraArgs) != 1 || preset.ExtraArgs[0] != "--flash-attn" {
		t.Fatalf("unexpected extra_args in preset: %+v", preset.ExtraArgs)
	}
}

func TestKeepServicesOnQuitSettingDefaultAndPatch(t *testing.T) {
	var calls atomic.Int32
	s, _, _, _ := setupStandaloneTestService(t, true, &calls, nil)

	if !s.EffectiveKeepServicesOnQuit() {
		t.Fatalf("expected EffectiveKeepServicesOnQuit default true")
	}
	if !s.Settings().Effective.KeepServicesOnQuit {
		t.Fatalf("expected EffectiveSettings.KeepServicesOnQuit default true")
	}

	resp, apiErr := s.UpdateSettings(map[string]json.RawMessage{
		"keep_services_on_quit": json.RawMessage("false"),
	})
	if apiErr != nil {
		t.Fatalf("UpdateSettings(false) failed: %+v", apiErr)
	}
	if resp.Effective.KeepServicesOnQuit || s.EffectiveKeepServicesOnQuit() {
		t.Fatalf("expected KeepServicesOnQuit=false after patch")
	}

	resp, apiErr = s.UpdateSettings(map[string]json.RawMessage{
		"keep_services_on_quit": json.RawMessage("null"),
	})
	if apiErr != nil {
		t.Fatalf("UpdateSettings(null) failed: %+v", apiErr)
	}
	if !resp.Effective.KeepServicesOnQuit || !s.EffectiveKeepServicesOnQuit() {
		t.Fatalf("expected KeepServicesOnQuit=true when reset to null")
	}
	_ = time.Second
}
