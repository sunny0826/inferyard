package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"inferyard/studio/internal/inferyard"
)

func runLocalQueueHelper(args []string) {
	_, port := startupAddress(args)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"data":[]}`)) })
	if http.ListenAndServe("127.0.0.1:"+strconv.Itoa(port), mux) != nil {
		os.Exit(2)
	}
}

type localQueueFixture struct {
	s                                    *Service
	mu                                   sync.Mutex
	workspace, results, family, endpoint string
	candidates                           []CandidateRun
	bindPIDs                             []int64
	runBundles                           []string
	failBundle                           string
	blockRun                             bool
	runStarted                           chan string
}

func fixtureFlag(args []string, flag string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	return ""
}

func newLocalQueueFixture(t *testing.T, family string) *localQueueFixture {
	t.Helper()
	workspace, err := canonical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &localQueueFixture{workspace: workspace, results: filepath.Join(workspace, "results"), family: family, endpoint: fmt.Sprintf("http://127.0.0.1:%d", freePort(t)), runStarted: make(chan string, 2)}
	f.s = testService(t, f.execute)
	f.s.settings.WorkspaceRoot, f.s.settings.ResultsRoot = &f.workspace, &f.results
	f.s.engineTimeout = 3 * time.Second
	if family == "lmstudio" {
		f.s.identityReader = func(pid int64) (int64, error) { return pid * 10, nil }
	}
	for _, bundle := range []string{"zh-core", "zh-svg-pelican"} {
		path := filepath.Join(workspace, "candidates", bundle, "candidate.toml")
		if err := f.writeConfig(path, bundle); err != nil {
			t.Fatal(err)
		}
		f.candidates = append(f.candidates, CandidateRun{CandidatePath: path, Bundle: bundle, Family: family})
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := f.s.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	return f
}

func (f *localQueueFixture) writeConfig(path, bundle string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	bundlePath := filepath.Join(f.workspace, "bundles", bundle+".json")
	if err := os.MkdirAll(filepath.Dir(bundlePath), 0700); err != nil {
		return err
	}
	raw, _ := json.Marshal(map[string]string{"bundle_id": bundle})
	if err := os.WriteFile(bundlePath, raw, 0600); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(fmt.Sprintf("[output]\nroot = %q\n[bundle]\npath = %q\n[endpoint]\nserver_pid = 1\nprocess_start_ticks = 1\n", f.results, bundlePath)), 0600)
}

func (f *localQueueFixture) description() inferyard.Execution {
	hash := strings.Repeat("a", 64)
	port := strings.TrimPrefix(f.endpoint, "http://127.0.0.1:")
	launch := map[string]any{"kind": "direct_process", "argv": []string{os.Args[0], "local-queue-helper", "--host", "127.0.0.1", "--port", port}, "working_directory": f.workspace, "environment": map[string]string{}, "ownership": "operator"}
	kind := "directory"
	if f.family == "llama-cpp" || f.family == "lmstudio" {
		kind = "gguf"
	}
	if f.family == "lmstudio" {
		delete(launch, "argv")
		launch["kind"], launch["lms_executable"], launch["models_root"], launch["background_argv"], launch["served_model"], launch["requires_loaded_instance"] = "lmstudio", os.Args[0], f.workspace, []string{os.Args[0]}, "fixture", true
	}
	details := map[string]any{"schema_version": 3, "definition": "local-engine-description.v2", "family": f.family, "model_requests_sent": 0, "bound": false,
		"launch": launch, "assets": map[string]any{"installation": map[string]any{"path": filepath.Join(f.workspace, "installation.json"), "sha256": hash, "declaration_sha256": hash}, "model": map[string]any{"path": filepath.Join(f.workspace, "model"), "kind": kind, "content_sha256": hash, "manifest_path": filepath.Join(f.workspace, "model-manifest.json"), "manifest_sha256": hash}, "template": map[string]any{"path": filepath.Join(f.workspace, "template.jinja"), "sha256": hash}},
		"endpoint": map[string]any{"url": f.endpoint, "server_pid": 1, "process_start_ticks": 1}, "conditions": map[string]any{"context_size": 4096}, "generation": map[string]any{"max_tokens": 32, "temperature": 0},
		"safety": map[string]any{"interval_seconds": 1, "max_temperature_celsius": 85, "require_temperature": true, "check_environment": true}, "effective_parameters": nil, "effective_template": nil, "effective_unknown_reason": "requires_live_engine_observation"}
	raw, _ := json.Marshal(map[string]any{"command": "config describe", "status": "prepared", "completeness": "complete", "run_id": nil, "evidence_dir": nil, "limitations": []string{}, "details": details})
	return inferyard.Execution{Stdout: raw}
}

func (f *localQueueFixture) execute(ctx context.Context, _ string, args ...string) (inferyard.Execution, error) {
	if out, ok := version(args); ok {
		return out, nil
	}
	if len(args) > 1 && args[0] == "config" && args[1] == "describe" {
		return f.description(), nil
	}
	if len(args) > 1 && args[0] == "config" && args[1] == "bind" {
		_, _, bundle, err := readConfig(fixtureFlag(args, "--candidate"))
		if err != nil {
			return inferyard.Execution{}, err
		}
		pid, _ := strconv.ParseInt(fixtureFlag(args, "--pid"), 10, 64)
		f.mu.Lock()
		f.bindPIDs = append(f.bindPIDs, pid)
		fail := f.failBundle == bundle
		f.mu.Unlock()
		if fail {
			return result(2), nil
		}
		err = f.writeConfig(filepath.Join(fixtureFlag(args, "--out"), "config.toml"), bundle)
		return result(0), err
	}
	if args[0] == "run" {
		_, _, bundle, err := readConfig(fixtureFlag(args, "--config"))
		if err != nil {
			return inferyard.Execution{}, err
		}
		f.mu.Lock()
		f.runBundles = append(f.runBundles, bundle)
		block := f.blockRun
		f.mu.Unlock()
		f.runStarted <- bundle
		if block {
			<-ctx.Done()
			return result(130), ctx.Err()
		}
		return result(0), nil
	}
	return inferyard.Execution{}, fmt.Errorf("unexpected fixture command %v", args)
}

func (f *localQueueFixture) done(t *testing.T, tasks []RunTask) []RunTask {
	t.Helper()
	wait(t, func() bool {
		for _, task := range tasks {
			v, _ := f.s.RunDetail(task.TaskId)
			if !terminal(v.Task.State) {
				return false
			}
		}
		return true
	})
	result := []RunTask{}
	for _, task := range tasks {
		v, _ := f.s.RunDetail(task.TaskId)
		result = append(result, v.Task)
	}
	return result
}
