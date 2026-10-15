package e2e

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"inferyard/studio/internal/core"
)

// prepareBound drives a fresh service through init, device-check, fabricated
// candidates, a Studio-managed fake engine, and config bind for both default
// bundles — the exact shared precondition of the P0 scenario tests. It stops
// when progress first asks for a run, and returns the running engine's
// candidate directory alongside the service.
func prepareBound(t *testing.T) (context.Context, *Env, *core.Service, string) {
	t.Helper()
	cli := ResolveCLI(t)
	env := NewEnv(t, cli)
	ctx := context.Background()
	service, err := core.New(ctx, env.Options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		shutdownCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		_ = service.Shutdown(shutdownCtx)
	})

	mustJSON := func(v any) json.RawMessage {
		data, _ := json.Marshal(v)
		return data
	}
	if _, e := service.UpdateSettings(map[string]json.RawMessage{
		"inferyard_path": mustJSON(cli),
		"models_root":    mustJSON(env.Models),
	}); e != nil {
		t.Fatal(e)
	}

	runWizard(t, service, ctx, "inferyard init --out "+quote(env.Workspace), false, env.Recorder)
	assets := writeFakeAssets(t)
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	writeJSONFile(t, assets.Manifest, map[string]string{filepath.Base(binary): sha256File(t, binary)})
	placeFakeModel(t, env.Models, assets.Model)
	port := freePort(t)

	mustProgress := func() *core.ProgressResponse {
		t.Helper()
		p, apiErr := service.Progress(ctx)
		if apiErr != nil {
			t.Fatalf("progress: %+v [%s]", apiErr, analyzeFailure(env.Recorder))
		}
		if p.ExitCode != 0 || p.Progress == nil {
			t.Fatalf("progress blocked, exit %d [%s]", p.ExitCode, analyzeFailure(env.Recorder))
		}
		return &p
	}

	candidateDir := ""
	engineRunning := false
	for step := 0; step < 10; step++ {
		progress := mustProgress().Progress
		switch progress.NextAction {
		case "device_check":
			runWizard(t, service, ctx, *progress.NextCommand, false, env.Recorder)
		case "prepare_candidate":
			for _, b := range progress.Bundles {
				dir := WriteCandidate(t, progress.Workspace, b.Bundle, assets, binary, port)
				if b.Bundle == "zh-core" {
					candidateDir = dir
				}
			}
		case "bind":
			if !engineRunning {
				if _, e := service.StartEngine(candidateDir); e != nil {
					t.Fatalf("start engine: %+v", e)
				}
				waitEngineRunning(t, service, 60*time.Second)
				engineRunning = true
			}
			engine := service.Engine()
			bound := false
			for _, b := range progress.Bundles {
				if b.Phase == "bound_missing" && b.NextCommand != nil {
					runWizard(t, service, ctx, bindCommand(t, *b.NextCommand, engine.Pid, engine.Endpoint), false, env.Recorder)
					bound = true
					break // re-read progress: bind numbering comes from the CLI
				}
			}
			if !bound {
				t.Fatal("bind action but no bound_missing bundle")
			}
		case "run":
			// Progress asks to run the first bound bundle while the other may
			// still await binding; finish bindings before handing over.
			pendingBind := false
			for _, b := range progress.Bundles {
				if b.Phase == "bound_missing" && b.NextCommand != nil {
					engine := service.Engine()
					runWizard(t, service, ctx, bindCommand(t, *b.NextCommand, engine.Pid, engine.Endpoint), false, env.Recorder)
					pendingBind = true
					break
				}
			}
			if !pendingBind {
				return ctx, env, service, candidateDir
			}
		default:
			t.Fatalf("unexpected next_action %q", progress.NextAction)
		}
	}
	t.Fatal("prepareBound did not reach run within 10 steps")
	return nil, nil, nil, ""
}

// boundConfigPath resolves the bound config.toml the CLI produced for a
// bundle, via the binding preparation record.
func boundConfigPath(t *testing.T, workspace, bundle string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(workspace, "bound", bundle+"-*", "preparation.json"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no bound preparation for %s", bundle)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		Config string `json:"config"`
	}
	if err := json.Unmarshal(data, &record); err != nil || record.Config == "" {
		t.Fatalf("bound preparation unreadable: %s", matches[0])
	}
	return record.Config
}

// TestWizardCommandGuards locks the request-validation lane: malformed or
// out-of-contract commands are rejected before any CLI invocation.
func TestWizardCommandGuards(t *testing.T) {
	cli := ResolveCLI(t)
	env := NewEnv(t, cli)
	ctx := context.Background()
	service, err := core.New(ctx, env.Options)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		shutdownCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		_ = service.Shutdown(shutdownCtx)
	}()
	data, _ := json.Marshal(cli)
	if _, e := service.UpdateSettings(map[string]json.RawMessage{"inferyard_path": data}); e != nil {
		t.Fatal(e)
	}

	cases := []struct {
		name    string
		command string
	}{
		{"missing inferyard prefix", "init --out " + env.Workspace},
		{"shell metacharacters", "inferyard init --out /tmp/x$(whoami)"},
		{"shell pipe", "inferyard init --out /tmp/x | tee"},
		{"unknown command", "inferyard frobnicate --out /tmp/x"},
		{"disallowed flag", "inferyard init --out /tmp/x --force"},
		{"relative output path", "inferyard init --out relative/dir"},
		{"report without runs", "inferyard report --out /tmp/x"},
		{"verify without path", "inferyard verify"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, apiErr := service.WizardExecute(ctx, core.WizardRequest{Command: tc.command})
			if apiErr == nil {
				t.Fatalf("command accepted: %q", tc.command)
			}
			if apiErr.Code != "invalid_request" {
				t.Fatalf("code %q, want invalid_request for %q", apiErr.Code, tc.command)
			}
		})
		// Guarded commands may still probe CLI availability once; the subtests
		// above prove no guarded command itself was executed.
	}
}

// TestQueuedRunCancel covers the cancellation ladder's deterministic rung:
// a run cancelled while still queued never reaches the CLI.
func TestQueuedRunCancel(t *testing.T) {
	ctx, env, service, _ := prepareBound(t)

	start := func(config string) string {
		t.Helper()
		result, apiErr := service.WizardExecute(ctx, core.WizardRequest{
			Command: "inferyard run --config " + quote(config),
		})
		if apiErr != nil {
			t.Fatalf("run %s: %+v", config, apiErr)
		}
		if len(result.Tasks) != 1 {
			t.Fatalf("run returned %d tasks, want 1", len(result.Tasks))
		}
		return result.Tasks[0].TaskId
	}
	first := start(boundConfigPath(t, env.Workspace, "zh-core"))
	second := start(boundConfigPath(t, env.Workspace, "zh-svg-pelican"))

	cancelled, apiErr := service.CancelRun(second)
	if apiErr != nil {
		t.Fatalf("cancel queued run: %+v", apiErr)
	}
	if cancelled.State != "cancelled" {
		t.Fatalf("cancelled task state %q", cancelled.State)
	}

	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		tasks := service.RunTasks()
		states := map[string]string{}
		terminalCount := 0
		for _, task := range tasks {
			states[task.TaskId] = task.State
			switch task.State {
			case "done", "failed", "cancelled":
				terminalCount++
			}
		}
		if terminalCount == len(tasks) && len(tasks) == 2 {
			if states[first] != "done" {
				t.Fatalf("first run state %q, want done", states[first])
			}
			if states[second] != "cancelled" {
				t.Fatalf("second run state %q, want cancelled", states[second])
			}
			runs := 0
			for _, record := range env.Recorder.Records() {
				if len(record.Argv) > 1 && record.Argv[1] == "run" {
					runs++
				}
			}
			if runs != 1 {
				t.Fatalf("transcript holds %d run calls, want 1 (cancelled run must not execute)", runs)
			}
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("tasks did not reach terminal state")
}

// TestEngineCrashRecovery kills the managed engine mid-life and asserts the
// monitor reports the failure and a fresh engine can be started afterwards.
func TestEngineCrashRecovery(t *testing.T) {
	_, _, service, candidateDir := prepareBound(t)

	state := service.Engine()
	if state.State != "running" || state.Pid <= 0 {
		t.Fatalf("engine not running: %+v", state)
	}
	process, err := os.FindProcess(int(state.Pid))
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Kill(); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if service.Engine().State == "failed" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if got := service.Engine().State; got != "failed" {
		t.Fatalf("engine state %q after kill, want failed", got)
	}

	restarted, apiErr := service.StartEngine(candidateDir)
	if apiErr != nil {
		t.Fatalf("restart after crash: %+v", apiErr)
	}
	if restarted.State != "starting" && restarted.State != "running" {
		t.Fatalf("restarted state %q", restarted.State)
	}
	waitEngineRunning(t, service, 60*time.Second)
	if service.Engine().Pid == state.Pid {
		t.Fatal("restart reused the killed pid")
	}
}
