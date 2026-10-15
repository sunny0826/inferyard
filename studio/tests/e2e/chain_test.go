package e2e

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

// TestChainBindRunReport is the golden-path chain: real CLI init and
// device-check, Studio-managed fake engine, real config bind for both default
// bundles, formal runs, and a verified report. Every CLI call goes through
// the recording executor, so a failure produces a blame transcript.
func TestChainBindRunReport(t *testing.T) {
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
	// device-check discovers local GGUFs under the configured models root.
	placeFakeModel(t, env.Models, assets.Model)
	port := freePort(t)
	engineRunning := false

	for step := 0; step < 40; step++ {
		p, apiErr := service.Progress(ctx)
		if apiErr != nil {
			t.Fatalf("progress error: %+v", apiErr)
		}
		if p.ExitCode != 0 || p.Progress == nil {
			t.Fatalf("progress blocked, exit %d: %s", p.ExitCode, lastStderr(env.Recorder))
		}
		progress := p.Progress
		if progress.DefaultScopeComplete {
			break
		}
		t.Logf("step %d: next_action=%s", step, progress.NextAction)
		switch progress.NextAction {
		case "device_check":
			runWizard(t, service, ctx, *progress.NextCommand, false, env.Recorder)
		case "prepare_candidate":
			for _, b := range progress.Bundles {
				if b.CandidatePath == nil {
					WriteCandidate(t, progress.Workspace, b.Bundle, assets, binary, port)
				}
			}
		case "bind":
			if !engineRunning {
				candidate := firstUnboundCandidate(t, progress)
				if _, e := service.StartEngine(candidate); e != nil {
					t.Fatalf("start engine: %+v", e)
				}
				waitEngineRunning(t, service, 60*time.Second)
				engineRunning = true
			}
			engine := service.Engine()
			for _, b := range progress.Bundles {
				if b.Phase == "bound_missing" && b.NextCommand != nil {
					runWizard(t, service, ctx, bindCommand(t, *b.NextCommand, engine.Pid, engine.Endpoint), false, env.Recorder)
				}
			}
		case "run":
			runWizard(t, service, ctx, *progress.NextCommand, false, env.Recorder)
			waitTasksTerminal(t, service, 10*time.Minute)
		case "report":
			runWizard(t, service, ctx, *progress.NextCommand, true, env.Recorder)
		case "verify":
			runWizard(t, service, ctx, *progress.NextCommand, false, env.Recorder)
		default:
			t.Fatalf("unexpected next_action %q", progress.NextAction)
		}
		if step == 39 {
			t.Fatal("chain did not finish within 40 steps")
		}
	}

	final, apiErr := service.Progress(ctx)
	if apiErr != nil || final.Progress == nil || !final.Progress.DefaultScopeComplete {
		t.Fatalf("default scope not complete: %+v", final)
	}
	for _, b := range final.Progress.Bundles {
		if b.Phase != "run_complete" {
			t.Errorf("bundle %s phase %q, want run_complete", b.Bundle, b.Phase)
		}
	}
	assertGoldenTranscript(t, env.Recorder)
	assertReportExists(t, final.Progress.Workspace)
	assertEvidenceContracts(t, final.Progress.Workspace)
}

func runWizard(t *testing.T, service *core.Service, ctx context.Context, command string, verify bool, recorder *Recorder) {
	t.Helper()
	result, apiErr := service.WizardExecute(ctx, core.WizardRequest{Command: command, ReportVerify: verify})
	if apiErr != nil {
		t.Fatalf("wizard %q: %+v", command, apiErr)
	}
	if result.ExitCode != 0 {
		t.Fatalf("wizard %q exit %d limitations %v: %s", command, result.ExitCode, result.Limitations, lastStderr(recorder))
	}
}

func lastStderr(recorder *Recorder) string {
	records := recorder.Records()
	if len(records) == 0 {
		return ""
	}
	data, _ := os.ReadFile(records[len(records)-1].StderrFile)
	return strings.TrimSpace(string(data))
}

// placeFakeModel copies the fake GGUF into the models root so the real
// device-check discovers one available local model.
func placeFakeModel(t *testing.T, modelsRoot, modelPath string) {
	t.Helper()
	if err := os.MkdirAll(modelsRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(modelPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modelsRoot, filepath.Base(modelPath)), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func firstUnboundCandidate(t *testing.T, progress *inferyard.WorkspaceProgress) string {
	t.Helper()
	for _, b := range progress.Bundles {
		if b.Phase == "bound_missing" && b.CandidatePath != nil {
			return *b.CandidatePath
		}
	}
	t.Fatal("no bound_missing bundle with a candidate")
	return ""
}

func bindCommand(t *testing.T, nextCommand string, pid int64, endpoint string) string {
	t.Helper()
	args, err := inferyard.SplitCommand(nextCommand)
	if err != nil {
		t.Fatal(err)
	}
	args = setArg(args, "--pid", strconv.FormatInt(pid, 10))
	args = setArg(args, "--endpoint", endpoint)
	return commandOf(args)
}

// quote and commandOf mirror the dashboard's command rendering (pages.quote,
// pages.commandOf): single-quote every argument and restore the inferyard
// prefix that SplitCommand strips.
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

func commandOf(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = quote(a)
	}
	return "inferyard " + strings.Join(out, " ")
}

func setArg(args []string, flag, value string) []string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			args[i+1] = value
			return args
		}
	}
	return append(args, flag, value)
}

func waitEngineRunning(t *testing.T, service *core.Service, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		state := service.Engine()
		switch state.State {
		case "running":
			return
		case "failed", "orphaned":
			t.Fatalf("engine state %s", state.State)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("engine did not reach running")
}

func waitTasksTerminal(t *testing.T, service *core.Service, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		tasks := service.RunTasks()
		if len(tasks) > 0 {
			terminalCount := 0
			for _, task := range tasks {
				switch task.State {
				case "done", "failed", "cancelled":
					terminalCount++
				}
			}
			if terminalCount == len(tasks) {
				last := tasks[len(tasks)-1]
				if last.State != "done" || last.Exit == nil || last.Exit.ExitCode != 0 {
					t.Fatalf("run task state %s exit %+v", last.State, last.Exit)
				}
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("run tasks did not reach terminal state")
}

func assertGoldenTranscript(t *testing.T, recorder *Recorder) {
	t.Helper()
	commands := map[string]int{}
	for _, record := range recorder.Records() {
		if len(record.Argv) < 2 {
			continue
		}
		key := record.Argv[1]
		if key == "config" && len(record.Argv) > 2 {
			key += " " + record.Argv[2]
		}
		commands[key]++
		if record.ExitCode != 0 {
			t.Errorf("record %d %q exited %d", record.Seq, key, record.ExitCode)
		}
	}
	for command, want := range map[string]int{
		"init": 1, "device-check": 1, "config bind": 2, "run": 2, "report": 1, "verify": 1,
	} {
		if commands[command] < want {
			t.Errorf("command %q recorded %d times, want >= %d", command, commands[command], want)
		}
	}
}

func assertReportExists(t *testing.T, workspace string) {
	t.Helper()
	reports := filepath.Join(workspace, "reports")
	entries, err := os.ReadDir(reports)
	if err != nil {
		t.Fatal("no reports directory")
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		found, _ := filepath.Glob(filepath.Join(reports, entry.Name(), "*.html"))
		if len(found) > 0 {
			return
		}
	}
	t.Fatal("no HTML report produced")
}
