package core

import (
	"context"
	"fmt"
	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const engineCheckFixture = `{"command":"engine check","status":"checked","completeness":"complete","run_id":null,"evidence_dir":null,"limitations":[],"details":{"eligible":true,"reason":null,"reported_version":"prism-b10743-adfffbe","expected_release":"prism-b10743-adfffbe","modes":{"cpu":true,"metal":true},"mode":null}}`

func ineligibleFixture(reason string) string {
	return strings.NewReplacer(
		`"eligible":true`, `"eligible":false`,
		`"reason":null`, `"reason":"`+reason+`"`,
		`"reported_version":"prism-b10743-adfffbe"`, `"reported_version":null`,
		`"modes":{"cpu":true,"metal":true}`, `"modes":null`,
	).Replace(engineCheckFixture)
}

func engineCheckService(t *testing.T, exitCode int, stdout string) (*Service, *[][]string) {
	t.Helper()
	calls := &[][]string{}
	s := wizardService(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if args[0] == "--version" {
			return inferyard.Execution{Stdout: []byte("inferyard fixture")}, nil
		}
		*calls = append(*calls, append([]string{}, args...))
		return inferyard.Execution{ExitCode: exitCode, Stdout: []byte(stdout)}, nil
	})
	return s, calls
}

func TestCheckEngineInvocationAndVerdicts(t *testing.T) {
	s, calls := engineCheckService(t, 0, engineCheckFixture)
	r, e := s.CheckEngine(context.Background(), "/engines/release/llama-server", "")
	if e != nil || !r.Eligible || r.Reason != nil || *r.ReportedVersion != "prism-b10743-adfffbe" ||
		r.ExpectedRelease != "prism-b10743-adfffbe" || r.Modes == nil || !r.Modes.Metal || r.Mode != nil {
		t.Fatalf("%+v %v", r, e)
	}
	want := []string{"engine", "check", "--engine", "/engines/release/llama-server"}
	if runtime.GOOS == "windows" {
		want = []string{"engine", "check", "--runtime-receipt", "/receipt/runtime-receipt.json"}
	}
	if runtime.GOOS == "windows" {
		s, calls = engineCheckService(t, 0, engineCheckFixture)
		if _, e = s.CheckEngine(context.Background(), "/engines/release/llama-server", "/receipt/runtime-receipt.json"); e != nil {
			t.Fatal(e)
		}
	}
	if !reflect.DeepEqual((*calls)[0], want) {
		t.Fatal((*calls)[0])
	}
	s, _ = engineCheckService(t, 0, ineligibleFixture("engine_version_too_old"))
	if _, e := s.CheckEngine(context.Background(), "/engines/old/llama-server", ""); e == nil || e.Code != "engine_check_invalid_response" {
		t.Fatalf("free reason accepted: %v", e)
	}
	s, _ = engineCheckService(t, 0, ineligibleFixture("invalid_library_manifest"))
	r, e = s.CheckEngine(context.Background(), "/engines/old/llama-server", "")
	if e != nil || r.Eligible || r.Reason == nil || *r.Reason != "invalid_library_manifest" || r.ExpectedRelease == "" {
		t.Fatalf("%+v %v", r, e)
	}
}

func TestCheckEngineFailureMapping(t *testing.T) {
	blocked := `{"command":"engine check","status":"blocked","completeness":"incomplete","run_id":null,"evidence_dir":null,"limitations":["invalid_input"],"details":null}`
	toolError := `{"command":"engine check","status":"error","completeness":"incomplete","run_id":null,"evidence_dir":null,"limitations":["engine_version_query_failed"],"details":null}`
	cancelled := `{"command":"engine check","status":"interrupted","completeness":"incomplete","run_id":null,"evidence_dir":null,"limitations":["cancelled"],"details":null}`
	for _, tc := range []struct {
		name, raw string
		exit      int
		code      string
	}{
		{"unknown command", `{"command":"unknown"}`, 2, "cli_outdated"},
		{"old parser", "invalid CLI arguments; use --help\n{\"command\":\"unknown\"}", 2, "cli_outdated"},
		{"blocked", blocked, 2, "cli_blocked"},
		{"cancelled", cancelled, 130, "cancelled"},
		{"tool error", toolError, 4, "engine_check_error"},
		{"invalid JSON", "secret diagnostic", 0, "engine_check_invalid_response"},
		{"missing details", `{"command":"engine check","status":"checked","completeness":"complete","run_id":null,"evidence_dir":null,"limitations":[],"details":null}`, 0, "engine_check_invalid_response"},
		{"old contract", `{"command":"engine check","status":"checked","completeness":"complete","run_id":null,"evidence_dir":null,"limitations":[],"details":{"eligible":true,"reason":null,"engine_release":"b6800","mode":"metal"}}`, 0, "engine_check_invalid_response"},
		{"eligible with reason", strings.NewReplacer(`"reason":null`, `"reason":"invalid_library_manifest"`).Replace(engineCheckFixture), 0, "engine_check_invalid_response"},
		{"extra detail", strings.NewReplacer(`"mode":null`, `"mode":null,"extra":1`).Replace(engineCheckFixture), 0, "engine_check_invalid_response"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := engineCheckService(t, tc.exit, tc.raw)
			if _, e := s.CheckEngine(context.Background(), "/engines/release/llama-server", ""); e == nil || e.Code != tc.code {
				t.Fatalf("%v", e)
			} else if tc.code == "cli_blocked" && !reflect.DeepEqual(e.Limitations, []string{"invalid_input"}) {
				t.Fatal(e)
			} else if tc.code == "engine_check_error" && !reflect.DeepEqual(e.Limitations, []string{"engine_version_query_failed"}) {
				t.Fatal(e)
			}
		})
	}
}

func checkableRegistry(t *testing.T, execute Executor) (*Service, string) {
	t.Helper()
	s := registryService(t)
	s.exec = execute
	root := s.registryRoots(*s.settings.WorkspaceRoot)[0]
	path := writeEngineBinary(t, filepath.Join(root, "release"), 0755, time.Unix(100, 0))
	return s, path
}

func TestVerifyEnginesCacheAndFingerprintInvalidation(t *testing.T) {
	var checks atomic.Int64
	s, path := checkableRegistry(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			return v, nil
		}
		checks.Add(1)
		return inferyard.Execution{Stdout: []byte(engineCheckFixture)}, nil
	})
	s.VerifyEngines()
	wait(t, func() bool { return checks.Load() == 1 })
	overview := s.EngineChecks()
	if !overview.Supported || overview.Checking != 0 || len(overview.Engines) != 1 {
		t.Fatalf("%+v", overview)
	}
	for _, eligibility := range overview.Engines {
		if eligibility.State != EngineCheckEligible || eligibility.Version != "prism-b10743-adfffbe" || !eligibility.Modes["metal"] {
			t.Fatalf("%+v", eligibility)
		}
	}
	// Cached by path+mtime+fingerprint: another scan without changes re-runs nothing.
	s.VerifyEngines()
	s.VerifyEngines()
	if checks.Load() != 1 {
		t.Fatal("cache miss without fingerprint change")
	}
	// A sibling library change must invalidate the verdict even when the engine
	// binary itself is untouched.
	dylib := filepath.Join(filepath.Dir(path), "libggml-metal.dylib")
	if err := os.WriteFile(dylib, []byte("lib"), 0644); err != nil {
		t.Fatal(err)
	}
	s.VerifyEngines()
	wait(t, func() bool { return checks.Load() == 2 })
	s.VerifyEngines()
	if checks.Load() != 2 {
		t.Fatal("cache miss after fingerprint settled")
	}
	if err := os.Chtimes(dylib, time.Unix(300, 0), time.Unix(300, 0)); err != nil {
		t.Fatal(err)
	}
	s.VerifyEngines()
	wait(t, func() bool { return checks.Load() == 3 })
}

func TestVerifyEnginesTransientFailureIsRetried(t *testing.T) {
	toolError := `{"command":"engine check","status":"error","completeness":"incomplete","run_id":null,"evidence_dir":null,"limitations":["engine_version_query_failed"],"details":null}`
	var checks atomic.Int64
	s, path := checkableRegistry(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			return v, nil
		}
		checks.Add(1)
		return inferyard.Execution{ExitCode: 4, Stdout: []byte(toolError)}, nil
	})
	// The cache is keyed by canonical path.
	path, err := canonical(path)
	if err != nil {
		t.Fatal(err)
	}
	s.VerifyEngines()
	wait(t, func() bool { return checks.Load() == 1 })
	overview := s.EngineChecks()
	for _, eligibility := range overview.Engines {
		if eligibility.State != EngineCheckUnknown || eligibility.Reason != "engine_version_query_failed" {
			t.Fatalf("%+v", eligibility)
		}
	}
	// The backoff keeps a failing engine from spinning the verify/event loop…
	s.VerifyEngines()
	if checks.Load() != 1 {
		t.Fatal("transient failure retried inside the backoff window")
	}
	// …and an elapsed backoff retries the check on a later scan.
	s.checkMu.Lock()
	entry := s.checks[path]
	entry.failedAt = time.Now().Add(-2 * time.Minute)
	s.checks[path] = entry
	s.checkMu.Unlock()
	s.VerifyEngines()
	wait(t, func() bool { return checks.Load() == 2 })
}

func TestVerifyEnginesIneligibleAndOutdatedCLI(t *testing.T) {
	s, _ := checkableRegistry(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			return v, nil
		}
		return inferyard.Execution{Stdout: []byte(ineligibleFixture("invalid_library_manifest"))}, nil
	})
	s.VerifyEngines()
	wait(t, func() bool { return s.EngineChecks().Checking == 0 && len(s.EngineChecks().Engines) == 1 })
	for _, eligibility := range s.EngineChecks().Engines {
		if eligibility.State != EngineCheckIneligible || eligibility.Reason != "invalid_library_manifest" {
			t.Fatalf("%+v", eligibility)
		}
	}

	// A CLI without engine check flips Supported off and shows 未核验, without
	// one error per engine.
	var calls atomic.Int64
	s, _ = checkableRegistry(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			return v, nil
		}
		calls.Add(1)
		return inferyard.Execution{ExitCode: 2, Stdout: []byte(`{"command":"unknown"}`)}, nil
	})
	writeEngineBinary(t, filepath.Join(s.registryRoots(*s.settings.WorkspaceRoot)[0], "older"), 0755, time.Unix(50, 0))
	s.VerifyEngines()
	wait(t, func() bool { return !s.EngineChecks().Supported })
	overview := s.EngineChecks()
	if overview.Checking != 0 || len(overview.Engines) != 2 {
		t.Fatalf("%+v", overview)
	}
	for name, eligibility := range overview.Engines {
		if eligibility.State != EngineCheckUnknown {
			t.Fatalf("%s %+v", name, eligibility)
		}
	}
	before := calls.Load()
	s.VerifyEngines()
	if calls.Load() != before {
		t.Fatal("outdated CLI keeps spawning checks")
	}
	if got := fmt.Sprint(overview.Engines); strings.Contains(got, *s.settings.WorkspaceRoot) {
		t.Fatal("path leaked into overview", got)
	}
}

func TestQueuedEngineChecksDoNotBlockOverview(t *testing.T) {
	gate := make(chan struct{})
	defer close(gate)
	var started atomic.Int64
	s, _ := checkableRegistry(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			return v, nil
		}
		started.Add(1)
		<-gate
		return inferyard.Execution{Stdout: []byte(engineCheckFixture)}, nil
	})
	root := s.registryRoots(*s.settings.WorkspaceRoot)[0]
	for _, name := range []string{"second", "third", "fourth"} {
		writeEngineBinary(t, filepath.Join(root, name), 0755, time.Unix(100, 0))
	}
	s.VerifyEngines()
	wait(t, func() bool { return started.Load() == 2 })
	done := make(chan EngineCheckOverview, 1)
	go func() { done <- s.EngineChecks() }()
	select {
	case overview := <-done:
		if overview.Checking != 4 || started.Load() != 2 {
			t.Fatalf("%+v started=%d", overview, started.Load())
		}
	case <-time.After(time.Second):
		t.Fatal("queued check held checkMu while waiting for semaphore")
	}
}
