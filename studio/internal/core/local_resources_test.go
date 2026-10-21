package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"inferyard/studio/internal/inferyard"
)

func localCheckFixture(eligible bool, reason any) string {
	details := map[string]any{"definition": "local-engine-check.v2", "eligible": eligible, "reason": reason,
		"family": "mlx-lm", "declaration_sha256": strings.Repeat("a", 64), "verification_scope": "installation_runtime_closure_only", "model_requests_sent": 0, "ready_to_run": false}
	if !eligible {
		details["declaration_sha256"] = nil
	}
	raw, _ := json.Marshal(map[string]any{"command": "engine check", "status": "checked", "completeness": "complete", "run_id": nil, "evidence_dir": nil, "limitations": []string{}, "details": details})
	return string(raw)
}

func writeInstallation(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(`{"family":"mlx-lm","executable":"/same/python","environment":{"SECRET":"never copy"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	resolved, err := canonical(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestRegisterInstallationCanonicalIdempotentAndRemoveReference(t *testing.T) {
	s, p := boundTestService(t, func(context.Context) error { return nil })
	path := writeInstallation(t, t.TempDir(), "installation.json")
	alias := filepath.Join(t.TempDir(), "alias.json")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	s.exec = func(ctx context.Context, cli string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			return v, nil
		}
		calls = append(calls, append([]string{}, args...))
		if cli != p.CLI || !reflect.DeepEqual(args, []string{"engine", "check", "--installation", path}) {
			t.Errorf("unexpected action: %s %v", cli, args)
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) < 4*time.Minute || time.Until(deadline) > installationCheckTimeout {
			t.Errorf("invalid deadline %v", deadline)
		}
		return inferyard.Execution{Stdout: []byte(localCheckFixture(true, nil))}, nil
	}
	for _, input := range []string{alias, path} {
		response, failure := s.RegisterEngineInstallation(context.Background(), input)
		if failure != nil || !reflect.DeepEqual(response.Settings.EngineInstallations, []string{path}) {
			t.Fatalf("%+v %v", response, failure)
		}
	}
	if !p.matches(s.userSettings()) || len(calls) != 2 {
		t.Fatal("bound paths changed or unexpected operations", calls)
	}
	raw, err := os.ReadFile(s.settingsPath)
	if err != nil || strings.Contains(string(raw), "never copy") || strings.Contains(string(raw), "same/python") {
		t.Fatal(string(raw), err)
	}
	registry := s.Registry()
	if len(registry.Engines) != 1 || registry.Engines[0].Name != "mlx-lm · installation.json" || registry.Engines[0].Kind != "local_installation" {
		t.Fatal(registry)
	}
	if _, failure := s.candidateEngineArgs([]string{"config", "create"}, registry.DefaultEngine, "darwin"); failure == nil || failure.Code != "local_engine_requires_explicit_candidate" {
		t.Fatal(failure)
	}
	response, failure := s.RemoveEngineInstallation(alias)
	if failure != nil || len(response.Settings.EngineInstallations) != 0 {
		t.Fatal(response, failure)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("declaration removed", err)
	}
	if len(calls) != 2 {
		t.Fatal("removal invoked runtime", calls)
	}
}

func TestRegisterInstallationFailureDoesNotPersist(t *testing.T) {
	for _, tc := range []struct {
		name, stdout, code string
		mutate             bool
	}{
		{"ineligible", localCheckFixture(false, "local_engine_executable_invalid"), "local_engine_executable_invalid", false},
		{"malformed", `{"secret":"never display"}`, "engine_check_invalid_response", false},
		{"legacy", engineCheckFixture, "engine_check_invalid_response", false},
		{"unknown reason", localCheckFixture(false, "/secret/never display"), "local_engine_installation_ineligible", false},
		{"no family", strings.Replace(localCheckFixture(false, "local_engine_json_unreadable"), `"family":"mlx-lm"`, `"family":null`, 1), "local_engine_json_unreadable", false},
		{"changed", localCheckFixture(true, nil), "local_engine_installation_changed", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := registryService(t)
			path := writeInstallation(t, t.TempDir(), "installation.json")
			s.exec = func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
				if v, ok := version(args); ok {
					return v, nil
				}
				if tc.mutate {
					if err := os.WriteFile(path, []byte(`{"family":"vllm"}`), 0600); err != nil {
						t.Fatal(err)
					}
				}
				return inferyard.Execution{Stdout: []byte(tc.stdout), Stderr: []byte("never display stderr")}, nil
			}
			_, failure := s.RegisterEngineInstallation(context.Background(), path)
			if failure == nil || failure.Code != tc.code || len(s.userSettings().EngineInstallations) != 0 {
				t.Fatal(failure, s.userSettings())
			}
			raw, _ := json.Marshal(failure)
			if strings.Contains(string(raw), "never display") {
				t.Fatal(string(raw))
			}
			if _, err := os.Stat(s.settingsPath); !os.IsNotExist(err) {
				t.Fatal("failed registration saved settings", err)
			}
		})
	}
}

func TestInstallationChecksIndependentAndContentInvalidation(t *testing.T) {
	s := registryService(t)
	a := writeInstallation(t, filepath.Join(t.TempDir(), "one"), "installation.json")
	b := writeInstallation(t, filepath.Join(t.TempDir(), "two"), "installation.json")
	s.settings.EngineInstallations = []string{a, b}
	var mu sync.Mutex
	calls := map[string]int{}
	s.exec = func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			return v, nil
		}
		if len(args) != 4 || args[0] != "engine" || args[1] != "check" || args[2] != "--installation" {
			t.Errorf("unexpected action %v", args)
			return inferyard.Execution{}, nil
		}
		mu.Lock()
		calls[args[3]]++
		mu.Unlock()
		if args[3] == b {
			return inferyard.Execution{Stdout: []byte(localCheckFixture(false, "local_engine_executable_invalid"))}, nil
		}
		return inferyard.Execution{Stdout: []byte(localCheckFixture(true, nil))}, nil
	}
	verify := func(wantA, wantB int) {
		t.Helper()
		s.VerifyEngines()
		wait(t, func() bool {
			mu.Lock()
			defer mu.Unlock()
			return calls[a] == wantA && calls[b] == wantB && s.EngineChecks().Checking == 0
		})
	}
	verify(1, 1)
	registry := s.Registry()
	if len(registry.Engines) != 2 || registry.Engines[0].Name == registry.Engines[1].Name {
		t.Fatal(registry)
	}
	for _, entry := range s.registry().engines {
		got := s.EngineChecks().Engines[entry.engine.Name]
		if got.VerificationScope != "installation_runtime_closure_only" || got.Family != "mlx-lm" {
			t.Fatal(got)
		}
		if entry.path == a && (got.State != EngineCheckEligible || got.DeclarationSHA256 == nil) {
			t.Fatal(got)
		}
		if entry.path == b && (got.State != EngineCheckIneligible || got.DeclarationSHA256 != nil) {
			t.Fatal(got)
		}
	}
	verify(1, 1)
	info, _ := os.Stat(a)
	raw, _ := os.ReadFile(a)
	raw = []byte(strings.Replace(string(raw), "never copy", "other data", 1))
	if err := os.WriteFile(a, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(a, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	verify(2, 1)
	s.InvalidateEngineChecks()
	wait(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return calls[a] == 3 && calls[b] == 2 && s.EngineChecks().Checking == 0
	})
}

func TestLegacyEngineCheckRejectsLocalVerdictShape(t *testing.T) {
	s, _ := engineCheckService(t, 0, localCheckFixture(true, nil))
	_, failure := s.checkEngineWithCLI(context.Background(), *s.settings.InferyardPath, "/engines/llama-server", "")
	if failure == nil || failure.Code != "engine_check_invalid_response" {
		t.Fatal(failure)
	}
}
