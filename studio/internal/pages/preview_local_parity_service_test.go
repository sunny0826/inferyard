//go:build ui_debug && !release

package pages

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

// The executor supplies CLI boundary responses; admission, queueing, binding,
// cancellation, evidence polling and the page's report orchestration are real.
type localParityService struct {
	service                        *core.Service
	ctx                            context.Context
	cancel                         context.CancelFunc
	mu                             sync.Mutex
	root, results, endpoint, stage string
	family                         string
	failure                        *core.ApiError
	failSecond                     bool
	release                        map[string]chan struct{}
	binds                          []int64
	report                         []string
	progress                       inferyard.WorkspaceProgress
	result                         startChainResult
	bindings                       benchmarkBindings
	done                           chan *core.ApiError
}

func newLocalParityService(t *testing.T, families ...string) *localParityService {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &localParityService{root: root, results: filepath.Join(root, "results"), endpoint: "http://127.0.0.1:48857", release: map[string]chan struct{}{"zh-core": make(chan struct{}), "zh-svg-pelican": make(chan struct{})}}
	f.family = "lmstudio"
	if len(families) > 0 {
		f.family = families[0]
		f.endpoint = localParityEngineEndpoint(t)
	}
	close(f.release["zh-core"])
	f.ctx, f.cancel = context.WithCancel(t.Context())
	f.progress = inferyard.WorkspaceProgress{Kind: "progress", ProgressVersion: 1, Workspace: root, NextAction: "run", DevicePreflight: inferyard.ProgressPreflight{Status: "ok"}, Report: inferyard.ProgressReport{Status: "missing", Runs: []string{}}}
	f.result = startChainResult{workspace: root, existingService: &core.EngineState{Pid: 4001, Endpoint: f.endpoint}, localRequest: &core.LocalCandidateRequest{EngineName: "lmstudio", ModelName: "fixture", ServicePID: 4001, Endpoint: f.endpoint}}
	if f.family != "lmstudio" {
		f.result.existingService = nil
	}
	for _, bundle := range []string{"zh-core", "zh-svg-pelican"} {
		path := filepath.Join(root, "candidates", bundle, "candidate.toml")
		if err := f.writeConfig(path, bundle); err != nil {
			t.Fatal(err)
		}
		f.result.localCandidates = append(f.result.localCandidates, core.CandidateRun{CandidatePath: path, Bundle: bundle, Family: f.family})
		f.progress.Bundles = append(f.progress.Bundles, inferyard.BundleProgress{Bundle: bundle, Phase: "run_missing", NextAction: "run", Runs: []inferyard.ProgressRun{}})
	}
	binary, _ := os.Executable()
	settings := filepath.Join(root, "settings.json")
	raw, _ := json.Marshal(core.UserSettings{InferyardPath: &binary, WorkspaceRoot: &root, ResultsRoot: &f.results})
	if err := os.WriteFile(settings, raw, 0600); err != nil {
		t.Fatal(err)
	}
	options := core.Options{SettingsPath: settings, HostStatePath: filepath.Join(root, "host.json"), Execute: f.execute}
	if f.family == "lmstudio" {
		options.ProcessStartTicks = func(pid int64) (int64, error) { return pid * 10, nil }
		options.SignalProcess = func(int64, os.Signal) error { return fmt.Errorf("must never signal external LM Studio") }
	}
	f.service, err = core.New(f.ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		f.cancel()
		ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := f.service.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	return f
}

func (f *localParityService) writeConfig(path, bundle string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	bundlePath := filepath.Join(f.root, bundle+".json")
	raw, _ := json.Marshal(map[string]string{"bundle_id": bundle})
	if err := os.WriteFile(bundlePath, raw, 0600); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(fmt.Sprintf("[output]\nroot = %q\n[bundle]\npath = %q\n[endpoint]\nserver_pid = 1\nprocess_start_ticks = 1\n", f.results, bundlePath)), 0600)
}

func localParityFlag(args []string, key string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key {
			return args[i+1]
		}
	}
	return ""
}

func (f *localParityService) describe() inferyard.Execution {
	raw := legacyCandidateResponse(filepath.Join(f.root, "model.gguf"), os.Args[0]).Stdout
	var envelope map[string]any
	_ = json.Unmarshal(raw, &envelope)
	d := envelope["details"].(map[string]any)
	delete(d, "adapter")
	d["definition"], d["family"], d["bound"] = "local-engine-description.v2", f.family, false
	d["effective_parameters"], d["effective_template"], d["effective_unknown_reason"] = nil, nil, "requires_live_engine_observation"
	d["safety"] = map[string]any{"interval_seconds": 1, "max_temperature_celsius": 85, "require_temperature": true, "check_environment": true}
	d["launch"] = map[string]any{"kind": "lmstudio", "working_directory": f.root, "environment": map[string]string{}, "ownership": "operator", "lms_executable": os.Args[0], "models_root": f.root, "background_argv": []string{os.Args[0]}, "served_model": "fixture", "requires_loaded_instance": true}
	a := d["assets"].(map[string]any)
	delete(a, "engine")
	hash := strings.Repeat("a", 64)
	a["installation"] = map[string]any{"path": filepath.Join(f.root, "installation.json"), "sha256": hash, "declaration_sha256": hash}
	m := a["model"].(map[string]any)
	m["manifest_path"], m["manifest_sha256"] = filepath.Join(f.root, "model-manifest.json"), hash
	d["endpoint"].(map[string]any)["url"] = f.endpoint
	if f.family != "lmstudio" {
		m["kind"] = "directory"
		d["launch"] = map[string]any{"kind": "direct_process", "argv": []string{os.Args[0], "-test.run=^TestLocalParityEngineHelper$", "--", "--local-parity-engine", "--host", "127.0.0.1", "--port", strings.TrimPrefix(f.endpoint, "http://127.0.0.1:")}, "working_directory": f.root, "environment": map[string]string{}, "ownership": "operator"}
	}
	raw, _ = json.Marshal(envelope)
	return inferyard.Execution{Stdout: raw}
}

func (f *localParityService) execute(ctx context.Context, _ string, args ...string) (inferyard.Execution, error) {
	result := func(code int) inferyard.Execution {
		return inferyard.Execution{ExitCode: code, Stdout: []byte(`{"command":"run","status":"complete","completeness":"complete","limitations":[]}`)}
	}
	switch args[0] {
	case "--version":
		return inferyard.Execution{Stdout: []byte("inferyard fixture source=release")}, nil
	case "config":
		if args[1] == "describe" {
			return f.describe(), nil
		}
		if args[1] != "bind" {
			return result(2), nil
		}
		bundle := filepath.Base(filepath.Dir(localParityFlag(args, "--candidate")))
		pid, _ := strconv.ParseInt(localParityFlag(args, "--pid"), 10, 64)
		f.mu.Lock()
		f.binds = append(f.binds, pid)
		fail := f.failSecond && bundle == "zh-svg-pelican"
		f.mu.Unlock()
		if fail {
			return result(2), nil
		}
		out := localParityFlag(args, "--out")
		if err := f.writeConfig(filepath.Join(out, "config.toml"), bundle); err != nil {
			return result(4), err
		}
		if err := os.WriteFile(filepath.Join(out, "bundle"), []byte(bundle), 0600); err != nil {
			return result(4), err
		}
		return result(0), nil
	case "run":
		raw, err := os.ReadFile(filepath.Join(filepath.Dir(localParityFlag(args, "--config")), "bundle"))
		if err != nil {
			return result(4), err
		}
		bundle := string(raw)
		run := filepath.Join(f.results, bundle)
		if err := os.MkdirAll(run, 0700); err != nil {
			return result(4), err
		}
		inputs, _ := json.Marshal(map[string]any{"schema_version": 3, "evidence_layout_version": 2, "run_id": bundle, "documents": map[string]any{"run": map[string]any{}}})
		if err := os.WriteFile(filepath.Join(run, "inputs.json"), inputs, 0600); err != nil {
			return result(4), err
		}
		select {
		case <-ctx.Done():
			return result(130), ctx.Err()
		case <-f.release[bundle]:
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		for i := range f.progress.Bundles {
			b := &f.progress.Bundles[i]
			if b.Bundle == bundle {
				b.Phase, b.NextAction = "run_complete", "none"
				b.Runs = append(b.Runs, inferyard.ProgressRun{Path: run, Complete: true, Sealed: true})
			}
		}
		return result(0), nil
	case "progress":
		f.mu.Lock()
		raw, _ := json.Marshal(f.progress)
		f.mu.Unlock()
		return inferyard.Execution{Stdout: raw}, nil
	case "report":
		return result(0), nil
	}
	return result(4), fmt.Errorf("unexpected command: %v", args)
}

func (f *localParityService) start() {
	f.mu.Lock()
	f.failure = nil
	f.mu.Unlock()
	done := make(chan *core.ApiError, 1)
	f.done = done
	go func() {
		failure := runBenchmarkFlow(f.ctx, f.service, f.result, []string{"zh-core", "zh-svg-pelican"}, &f.bindings, func(stage string) { f.mu.Lock(); f.stage = stage; f.mu.Unlock() }, func([]string) {}, func(paths []string, _ string) {
			f.mu.Lock()
			f.report = append([]string{}, paths...)
			f.stage = "done"
			f.mu.Unlock()
		})
		f.mu.Lock()
		f.failure = failure
		f.mu.Unlock()
		done <- failure
	}()
}
