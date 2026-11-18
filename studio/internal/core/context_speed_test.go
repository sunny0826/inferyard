package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"inferyard/studio/internal/inferyard"
)

type speedFixture struct {
	mu        sync.Mutex
	commands  [][]string
	result    []byte
	blockInit bool
	hold      chan struct{}
	started   chan struct{}
}

func speedService(t *testing.T, f *speedFixture) *Service {
	t.Helper()
	t.Setenv("ENGINE_HELPER", "1")
	workspace, _ := engineWorkspace(t, freePort(t))
	s := engineService(t, workspace)
	s.settings.InferyardPath = ptr(os.Args[0])
	s.settings.ModelsRoot = ptr(t.TempDir())
	model := filepath.Join(*s.settings.ModelsRoot, "model.gguf")
	if e := os.WriteFile(model, []byte("synthetic"), 0600); e != nil {
		t.Fatal(e)
	}
	s.device.Snapshot, _ = json.Marshal(map[string]any{"models": []any{map[string]any{"path": model, "status": "available", "size_bytes": 9, "architecture": "llama", "context_length": 262144}}})
	s.registryRoots = func(string) []string { return []string{} }
	engine, _ := os.Executable()
	engineDir := filepath.Join(workspace, "engines")
	writeEngineBinary(t, engineDir, 0755, time.Now())
	s.registryRoots = func(string) []string { return []string{engineDir} }
	port := freePort(t)
	s.exec = func(ctx context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			return v, nil
		}
		if out, ok := engineFixtureDescription(args); ok {
			return out, nil
		}
		f.mu.Lock()
		f.commands = append(f.commands, append([]string{}, args...))
		f.mu.Unlock()
		flag := func(key string) string {
			for i, a := range args {
				if a == key && i+1 < len(args) {
					return args[i+1]
				}
			}
			return ""
		}
		ok := inferyard.Execution{ExitCode: 0, Stdout: []byte(`{"command":"fixture","status":"finished","completeness":"complete"}`)}
		switch args[0] {
		case "--schema":
			ok.Stdout, _ = os.ReadFile("../../../cli/schemas/context_speed_result.schema.json")
		case "init":
			if f.blockInit {
				return inferyard.Execution{ExitCode: 2, Stdout: []byte(`{"command":"init","status":"blocked","completeness":"incomplete","limitations":["bundle_review_required"]}`)}, nil
			}
			if e := os.MkdirAll(flag("--out"), 0700); e != nil {
				return ok, e
			}
		case "device-check":
		case "config":
			out := flag("--out")
			if e := os.MkdirAll(out, 0700); e != nil {
				return ok, e
			}
			if args[1] == "create" {
				body := fmt.Sprintf("[engine]\nbinary_path = %q\nstartup_args = [\"--host\", \"127.0.0.1\", \"--port\", \"%d\"]\n", engine, port)
				if e := os.WriteFile(filepath.Join(out, "candidate.toml"), []byte(body), 0600); e != nil {
					return ok, e
				}
			}
		case "plan":
			dir := flag("--out")
			if e := os.MkdirAll(dir, 0700); e != nil {
				return ok, e
			}
			trials := []map[string]any{}
			for i := range 5 {
				trials = append(trials, map[string]any{"trial_id": fmt.Sprintf("trial-%d", i), "repeat_index": i})
			}
			raw, _ := json.Marshal(map[string]any{"schema_version": 3, "plan_sha256": strings.Repeat("a", 64), "experiment": map[string]any{"profile": "context-speed.v1", "budget": map[string]any{"max_requests": 5, "max_wall_seconds": 3725}}, "trials": trials})
			if e := os.WriteFile(filepath.Join(dir, "plan.json"), raw, 0600); e != nil {
				return ok, e
			}
		case "run":
			if len(args) != 5 || args[1] != "--plan" || args[3] != "--output-root" {
				return ok, fmt.Errorf("wrong argv: %v", args)
			}
			root := flag("--output-root")
			if e := os.MkdirAll(root, 0700); e != nil {
				return ok, e
			}
			if e := os.WriteFile(filepath.Join(root, "batch.json"), []byte(`{}`), 0600); e != nil {
				return ok, e
			}
			if f.started != nil {
				close(f.started)
			}
			if f.hold != nil {
				select {
				case <-ctx.Done():
					return inferyard.Execution{ExitCode: 130}, ctx.Err()
				case <-f.hold:
				}
			}
		case "speed-results":
			if ctx.Err() != nil {
				return inferyard.Execution{}, ctx.Err()
			}
			f.mu.Lock()
			raw := append([]byte{}, f.result...)
			f.mu.Unlock()
			var response map[string]any
			if json.Unmarshal(raw, &response) != nil {
				return inferyard.Execution{ExitCode: 4, Stdout: raw}, nil
			}
			response["evidence_dir"] = flag("--batch")
			ok.Stdout, _ = json.Marshal(response)
			if response["status"] == "partial" {
				ok.ExitCode = 3
			}
		default:
			return ok, fmt.Errorf("unexpected command %v", args)
		}
		return ok, nil
	}
	return s
}
