package core

import (
	"context"
	"fmt"
	"inferyard/studio/internal/inferyard"
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

func TestCheckEngineFailureMapping(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		exit      int
		code      string
	}{
		{"unknown command", `{"command":"unknown"}`, 2, "cli_outdated"},
		{"old parser", "invalid CLI arguments; use --help\n{\"command\":\"unknown\"}", 2, "cli_outdated"},
		{"blocked", `{"command":"engine check","status":"blocked","completeness":"incomplete","run_id":null,"evidence_dir":null,"limitations":["invalid_input"],"details":null}`, 2, "cli_blocked"},
		{"cancelled", `{"command":"engine check","status":"interrupted","completeness":"incomplete","run_id":null,"evidence_dir":null,"limitations":["cancelled"],"details":null}`, 130, "cancelled"},
		{"tool error", `{"command":"engine check","status":"error","completeness":"incomplete","run_id":null,"evidence_dir":null,"limitations":["engine_version_query_failed"],"details":null}`, 4, "engine_check_error"},
		{"invalid JSON", "secret diagnostic", 0, "engine_check_invalid_response"},
		{"installation scope", localCheckFixture(true, nil), 0, "engine_check_invalid_response"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := filepath.Join(t.TempDir(), "llama-server")
			receipt := filepath.Join(t.TempDir(), "runtime-receipt.json")
			want := []string{"engine", "check", "--engine", engine}
			if runtime.GOOS == "windows" {
				want = []string{"engine", "check", "--runtime-receipt", receipt}
			}
			s := wizardService(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
				if v, ok := version(args); ok {
					return v, nil
				}
				if !reflect.DeepEqual(args, want) {
					t.Fatalf("engine check input: got %v, want %v", args, want)
				}
				return inferyard.Execution{ExitCode: tc.exit, Stdout: []byte(tc.raw)}, nil
			})
			if _, e := s.checkEngineWithCLI(context.Background(), *s.settings.InferyardPath, engine, receipt); e == nil || e.Code != tc.code {
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
