package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func writeFakeManagedPolystrata(t *testing.T, dir, sourceURL, sha256Hex string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "serve"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "setup.py"), []byte("# setup\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "serve", "server.py"), []byte("# server\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	instPath := filepath.Join(dir, "installation.json")
	if err := os.WriteFile(instPath, []byte(`{"schema_version":3,"definition":"local-engine-installation.v1","family":"polystrata"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	receipt, err := json.Marshal(map[string]string{
		"definition":     "polystrata-runtime.v1",
		"source":         sourceURL,
		"archive_sha256": sha256Hex,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".inferyard-runtime.json"), receipt, 0o600); err != nil {
		t.Fatal(err)
	}
	return instPath
}

func TestCreatePolystrataSetupReusesManagedInstallationOnSecondModelInstall(t *testing.T) {
	ws := t.TempDir()
	enginesRoot := filepath.Join(ws, "engines")
	existingDir := filepath.Join(enginesRoot, "polystrata")
	instPath := writeFakeManagedPolystrata(t, existingDir, polystrataDefaultSourceURL, polystrataDefaultSourceSHA256)

	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	rawSettings, _ := json.Marshal(map[string]any{
		"workspace_root":       ws,
		"results_root":         filepath.Join(ws, "results"),
		"engine_installations": []string{instPath},
	})
	if err := os.WriteFile(settingsPath, rawSettings, 0o600); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var recorded [][]string
	s, err := New(t.Context(), Options{
		SettingsPath: settingsPath,
		Execute: func(ctx context.Context, path string, args ...string) (inferyard.Execution, error) {
			if out, ok := version(args); ok {
				return out, nil
			}
			if len(args) >= 4 && args[0] == "engine" && args[1] == "check" && args[2] == "--installation" {
				payload, _ := json.Marshal(map[string]any{
					"command": "engine check", "status": "checked", "completeness": "complete",
					"run_id": nil, "evidence_dir": nil, "limitations": []string{},
					"details": map[string]any{
						"definition": "local-engine-check.v2", "eligible": true, "reason": nil,
						"family": "polystrata", "declaration_sha256": strings.Repeat("c", 64),
						"verification_scope": "installation_runtime_closure_only",
						"model_requests_sent": 0, "ready_to_run": false,
					},
				})
				return inferyard.Execution{ExitCode: inferyard.ExitComplete, Stdout: payload}, nil
			}
			return inferyard.Execution{}, nil
		},
		ExecStream: func(ctx context.Context, onLine func(string), path string, args ...string) (inferyard.Execution, error) {
			mu.Lock()
			recorded = append(recorded, append([]string(nil), args...))
			mu.Unlock()
			payload, _ := json.Marshal(map[string]any{
				"command": "engine prepare-polystrata", "status": "completed", "completeness": "complete",
				"run_id": nil, "evidence_dir": nil, "limitations": []string{},
				"details": map[string]any{
					"installation_path": instPath,
					"model_config_path": filepath.Join(ws, "models", "polystrata-data", "strata-lfm25.json"),
					"log_path":          filepath.Join(existingDir, ".inferyard-setup.log"),
				},
			})
			return inferyard.Execution{ExitCode: inferyard.ExitComplete, Stdout: payload}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	s.settings.InferyardPath = ptr(os.Args[0])
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
	})

	task, apiErr := s.CreatePolystrataSetup(PolystrataSetupRequest{
		Family: "lfm25", Model: "UD-Q3_K_XL", Backend: "metal", ModelSource: "modelscope",
	})
	if apiErr != nil {
		t.Fatalf("CreatePolystrataSetup failed: %+v", apiErr)
	}
	canonicalExisting, _ := canonical(existingDir)
	if task.OutDir != canonicalExisting || task.OutName != "polystrata" {
		t.Fatalf("expected reuse of existing managed PolyStrata dir %q, got outDir=%q outName=%q", canonicalExisting, task.OutDir, task.OutName)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, cur := range s.Downloads() {
			if cur.TaskId == task.TaskId && cur.State != "downloading" {
				if cur.State != "done" {
					t.Fatalf("expected setup done, got %+v", cur)
				}
				goto checkedArgs
			}
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("timed out waiting for polystrata setup completion")

checkedArgs:
	mu.Lock()
	args := recorded[0]
	mu.Unlock()
	if !slices.Contains(args, "--resume") {
		t.Fatalf("expected --resume when reusing existing managed PolyStrata dir, got args=%v", args)
	}

	// Custom source or explicit OutName must still allocate a new directory instead of reusing existingDir.
	customTask, apiErr := s.CreatePolystrataSetup(PolystrataSetupRequest{
		Family: "qwen36", OutName: "polystrata-custom", Source: filepath.Join(t.TempDir(), "custom.tar.gz"),
	})
	if apiErr != nil {
		t.Fatalf("CreatePolystrataSetup with explicit OutName failed: %+v", apiErr)
	}
	if !strings.HasSuffix(customTask.OutDir, "polystrata-custom") {
		t.Fatalf("expected explicit OutName dir, got %q", customTask.OutDir)
	}
}

func TestExistingManagedPolystrataDirValidationBoundaries(t *testing.T) {
	ws := t.TempDir()
	enginesRoot := filepath.Join(ws, "engines")
	if err := os.MkdirAll(enginesRoot, 0o700); err != nil {
		t.Fatal(err)
	}

	// External directory outside workspace enginesRoot must never be reused.
	externalDir := filepath.Join(t.TempDir(), "external-polystrata")
	externalInst := writeFakeManagedPolystrata(t, externalDir, polystrataDefaultSourceURL, polystrataDefaultSourceSHA256)
	if _, _, ok := existingManagedPolystrataDir(enginesRoot, UserSettings{EngineInstallations: []string{externalInst}}, polystrataDefaultSourceURL); ok {
		t.Fatal("external PolyStrata installation must not be treated as managed dir")
	}

	// Managed directory with mismatched SHA-256 in receipt must not be reused.
	staleDir := filepath.Join(enginesRoot, "polystrata-stale")
	staleInst := writeFakeManagedPolystrata(t, staleDir, polystrataDefaultSourceURL, strings.Repeat("0", 64))
	if _, _, ok := existingManagedPolystrataDir(enginesRoot, UserSettings{EngineInstallations: []string{staleInst}}, polystrataDefaultSourceURL); ok {
		t.Fatal("stale receipt SHA-256 must not be reused")
	}
}
