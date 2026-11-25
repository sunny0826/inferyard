package core

import (
	"context"
	"encoding/json"
	"errors"
	"inferyard/studio/internal/inferyard"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestStartFromStartupStandaloneOriginOwnerAndShutdown(t *testing.T) {
	t.Setenv("ENGINE_HELPER", "1")
	port := freePort(t)
	workspace := t.TempDir()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	s := engineService(t, workspace)

	startup := Startup{
		Kind:             "direct_process",
		Path:             binary,
		Args:             []string{"--host", "127.0.0.1", "--port", strconv.Itoa(port)},
		Family:           "llama-cpp",
		Adapter:          "prism_llama_server_v1",
		WorkingDirectory: filepath.Dir(binary),
		Endpoint:         "http://127.0.0.1:" + strconv.Itoa(port),
		ModelPath:        "/models/qwen.gguf",
		ModelKind:        "gguf",
		ContextSize:      12000,
	}
	state, apiErr := s.startFromStartup(startup, EngineOriginStandalone, "qwen-llama-12000")
	if apiErr != nil {
		t.Fatalf("startFromStartup failed: %+v", apiErr)
	}
	if state.Origin != EngineOriginStandalone || state.Candidate != "qwen-llama-12000" {
		t.Fatalf("unexpected state: %+v", state)
	}
	deadline := time.Now().Add(3 * time.Second)
	for s.Engine().State != "running" {
		if time.Now().After(deadline) {
			t.Fatalf("engine did not reach running, got %+v", s.Engine())
		}
		time.Sleep(20 * time.Millisecond)
	}

	rawOwner, err := os.ReadFile(ownerPath(workspace))
	if err != nil {
		t.Fatalf("read owner file: %v", err)
	}
	var owner engineOwner
	if err := json.Unmarshal(rawOwner, &owner); err != nil {
		t.Fatalf("unmarshal owner file: %v", err)
	}
	if owner.Origin != EngineOriginStandalone || owner.Family != "llama-cpp" || owner.Model != "/models/qwen.gguf" || owner.Candidate != "qwen-llama-12000" {
		t.Fatalf("unexpected owner metadata: %+v", owner)
	}

	// Same ref with different origin is treated as a mismatch.
	if _, mismatch := s.startFromStartup(startup, EngineOriginBenchmark, "qwen-llama-12000"); mismatch == nil || mismatch.Code != "engine_candidate_mismatch" {
		t.Fatalf("expected engine_candidate_mismatch, got %+v", mismatch)
	}
	// Invalid ref (including path separators for standalone) is rejected.
	for _, bad := range []string{"", ".", "..", "dir/model"} {
		if _, badRef := s.startFromStartup(startup, EngineOriginStandalone, bad); badRef == nil || badRef.Code != "invalid_input" {
			t.Fatalf("expected invalid_input for %q ref, got %+v", bad, badRef)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.ShutdownWithOptions(ctx, true); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
	assertOwnerGone(t, ownerPath(workspace))
}

func TestConfigureEngineProcessSetsWorkingDirectoryForNonLocal(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command("true")
	if err := configureEngineProcess(cmd, Startup{Local: false, WorkingDirectory: dir}); err != nil {
		t.Fatalf("configureEngineProcess failed: %v", err)
	}
	if cmd.Dir != dir {
		t.Fatalf("cmd.Dir=%q want %q", cmd.Dir, dir)
	}
}

func TestOrphanRecoveryPreservesOriginAndCompatFallback(t *testing.T) {
	cases := []struct {
		name           string
		record         engineOwner
		wantOrigin     string
		wantErrCode    string
		wantFamily     string
		wantModel      string
	}{
		{
			name: "standalone_origin_preserved",
			record: engineOwner{
				PID: 4242, StartTicks: 123, Origin: EngineOriginStandalone,
				Family: "polystrata", Model: "/models/ps.gguf",
				Endpoint: "http://127.0.0.1:8080", LogFile: "engine.log", Candidate: "ps-serve",
			},
			wantOrigin: EngineOriginStandalone,
			wantFamily: "polystrata",
			wantModel:  "/models/ps.gguf",
		},
		{
			name: "legacy_owner_without_origin_defaults_to_benchmark",
			record: engineOwner{
				PID: 4242, StartTicks: 123,
				Endpoint: "http://127.0.0.1:8080", LogFile: "engine.log", Candidate: "candidate-1",
			},
			wantOrigin: EngineOriginBenchmark,
		},
		{
			name: "invalid_origin_rejected",
			record: engineOwner{
				PID: 4242, StartTicks: 123, Origin: "unknown_origin",
				Endpoint: "http://127.0.0.1:8080", LogFile: "engine.log", Candidate: "candidate-1",
			},
			wantOrigin:  EngineOriginBenchmark,
			wantErrCode: "engine_owner_invalid",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &ownerFixture{ticks: 123}
			workspace := t.TempDir()
			if err := writeEngineOwner(ownerPath(workspace), tc.record); err != nil {
				t.Fatal(err)
			}
			settings := filepath.Join(t.TempDir(), "settings.json")
			raw, _ := json.Marshal(UserSettings{WorkspaceRoot: &workspace})
			if err := os.WriteFile(settings, raw, 0600); err != nil {
				t.Fatal(err)
			}
			s, err := New(context.Background(), Options{
				SettingsPath:      settings,
				ProcessStartTicks: f.read,
				SignalProcess:     f.signal,
				OrphanStopTimeout: time.Millisecond,
				Execute: func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
					if v, ok := version(args); ok {
						return v, nil
					}
					return inferyard.Execution{}, errors.New("unexpected CLI request")
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = s.Shutdown(context.Background()) })

			got := s.Engine()
			if tc.name == "standalone_origin_preserved" {
				if got.State != "starting" || !got.Adopted {
					t.Fatalf("expected adopted starting standalone (probe not ready), got %+v", got)
				}
			} else if got.State != "orphaned" {
				t.Fatalf("expected orphaned, got %+v", got)
			}
			if got.Origin != tc.wantOrigin {
				t.Fatalf("origin=%q want %q", got.Origin, tc.wantOrigin)
			}
			if tc.wantFamily != "" && got.Family != tc.wantFamily {
				t.Fatalf("family=%q want %q", got.Family, tc.wantFamily)
			}
			if tc.wantModel != "" && got.Model != tc.wantModel {
				t.Fatalf("model=%q want %q", got.Model, tc.wantModel)
			}
			if tc.wantErrCode != "" {
				if got.Error == nil || got.Error.Code != tc.wantErrCode {
					t.Fatalf("error=%+v want code %q", got.Error, tc.wantErrCode)
				}
			} else if got.Error != nil {
				t.Fatalf("unexpected error: %+v", got.Error)
			}
		})
	}
}
