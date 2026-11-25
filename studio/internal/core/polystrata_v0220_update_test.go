package core

import (
	"bytes"
	"context"
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

func setupPolystrataV0220InstallFixture(t *testing.T, dir, dataDir, buildVersion, sourceURL, archiveSHA string) string {
	t.Helper()
	mustWriteFile(t, filepath.Join(dir, "terminal", "setup.py"), []byte("print('ok')\n"), 0o644)
	mustWriteFile(t, filepath.Join(dir, "terminal", "serve", "server.py"), []byte("print('srv')\n"), 0o644)
	mustWriteFile(t, filepath.Join(dir, "engine", "CMakeLists.txt"), []byte("cmake_minimum_required(VERSION 3.20)\n"), 0o644)
	mustWriteFile(t, filepath.Join(dir, "engine", "build-metal", "BUILD-poly.json"), []byte(`{"version":"`+buildVersion+`","backend":"metal"}`), 0o644)
	mustWriteFile(t, filepath.Join(dir, "runtime-payload.bin"), bytes.Repeat([]byte("r"), 2048), 0o644)
	if sourceURL != "" {
		receipt, _ := json.Marshal(map[string]any{
			"schema_version": 1,
			"definition":     "polystrata-runtime.v1",
			"source":         sourceURL,
			"archive_sha256": archiveSHA,
		})
		mustWriteFile(t, filepath.Join(dir, ".inferyard-runtime.json"), receipt, 0o600)
	}
	instPath := filepath.Join(dir, "installation.json")
	instPayload, _ := json.Marshal(map[string]any{
		"schema_version":    3,
		"definition":        "local-engine-installation.v1",
		"family":            "polystrata",
		"working_directory": dir,
		"data_dir":          dataDir,
	})
	mustWriteFile(t, instPath, instPayload, 0o600)
	return instPath
}

func TestPolystrataV0220OneClickUpdateAndReceiptVersion(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	var remoteTag atomic.Value
	remoteTag.Store("v0.2.20-poly.5")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/repos/simplexai-labs/PolyStrata/releases/latest" {
			tag := remoteTag.Load().(string)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": tag,
				"html_url": "https://github.com/simplexai-labs/PolyStrata/releases/tag/" + tag,
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	ws := filepath.Join(base, "ws")
	dataDir := filepath.Join(ws, "models", "polystrata-data")
	shard1 := filepath.Join(dataDir, "models", "Q2_0", "Qwen3.8-00001-of-00002.gguf")
	mustWriteFile(t, shard1, bytes.Repeat([]byte("q"), 4096), 0o600)

	// Verify qwen36-UD-Q4_K_M inference directly as well.
	q36Dir := filepath.Join(base, "q36-inst")
	q36Shard := filepath.Join(dataDir, "models", "qwen36-UD-Q4_K_M", "Qwen3.6-35B-A3B-UD-Q4_K_M.gguf")
	mustWriteFile(t, q36Shard, []byte("gguf"), 0o600)
	writeStrataConfigFixture(t, filepath.Join(q36Dir, "terminal", "strata-qwen36.json"), "qwen36-ud-q4-k-m", []string{
		"--model", q36Shard, "--max-context", "16384",
	})
	if fam, mod, _, ctxLen := inferPolystrataUpdateSpec(q36Dir, dataDir); fam != "qwen36" || mod != "UD-Q4_K_M" || ctxLen != 16384 {
		t.Fatalf("inferPolystrataUpdateSpec(qwen36) = (%q, %q, %d), want (qwen36, UD-Q4_K_M, 16384)", fam, mod, ctxLen)
	}

	// Stage outdated v0.1.45-poly.10 installation with strata-q2_0.json.
	oldDir := filepath.Join(ws, "engines", "polystrata-old")
	mustWriteFile(t, filepath.Join(oldDir, "setup.py"), []byte("print('ok')\n"), 0o644)
	mustWriteFile(t, filepath.Join(oldDir, "serve", "server.py"), []byte("print('srv')\n"), 0o644)
	mustWriteFile(t, filepath.Join(oldDir, "engine-poly", "BUILD.json"), []byte(`{"version":"v0.1.45-poly.10","backend":"metal"}`), 0o644)
	oldReceipt, _ := json.Marshal(map[string]any{
		"schema_version": 1,
		"definition":     "polystrata-runtime.v1",
		"source":         "https://github.com/simplexai-labs/PolyStrata/archive/" + polystrataLegacyPinnedCommit + ".tar.gz",
	})
	mustWriteFile(t, filepath.Join(oldDir, ".inferyard-runtime.json"), oldReceipt, 0o600)
	oldInst := filepath.Join(oldDir, "installation.json")
	rawOldInst, _ := json.Marshal(map[string]any{
		"schema_version":    3,
		"definition":        "local-engine-installation.v1",
		"family":            "polystrata",
		"working_directory": oldDir,
		"data_dir":          dataDir,
	})
	mustWriteFile(t, oldInst, rawOldInst, 0o600)
	writeStrataConfigFixture(t, filepath.Join(oldDir, "strata-q2_0.json"), "qwen3.8-flash-next-q2_0", []string{
		"--native", shard1, "--max-context", "32768",
	})

	var gotArgs []string
	execDone := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc, err := New(ctx, Options{
		SettingsPath:  filepath.Join(base, "settings.json"),
		HostStatePath: filepath.Join(base, "host.json"),
		Execute: func(ctx context.Context, bin string, args ...string) (inferyard.Execution, error) {
			if len(args) >= 2 && args[0] == "engine" && args[1] == "check" {
				payload, _ := json.Marshal(map[string]any{
					"command":      "engine check",
					"status":       "checked",
					"completeness": "complete",
					"run_id":       nil,
					"evidence_dir": nil,
					"limitations":  []string{},
					"details": map[string]any{
						"definition":          "local-engine-check.v2",
						"eligible":            true,
						"reason":              nil,
						"family":              "polystrata",
						"declaration_sha256":  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
						"verification_scope":  "installation_runtime_closure_only",
						"model_requests_sent": 0,
						"ready_to_run":        false,
					},
				})
				return inferyard.Execution{ExitCode: 0, Stdout: payload}, nil
			}
			return assetTestExec(ctx, bin, args...)
		},
		ExecStream: func(_ context.Context, _ func(string), _ string, args ...string) (inferyard.Execution, error) {
			gotArgs = append([]string(nil), args...)
			var outDir string
			if idx := slices.Index(args, "--out"); idx >= 0 && idx+1 < len(args) {
				outDir = args[idx+1]
			}
			newInst := setupPolystrataV0220InstallFixture(t, outDir, dataDir, "0.2.20", polystrataDefaultSourceURL, polystrataDefaultSourceSHA256)
			cfgPath := writeStrataConfigFixture(t, filepath.Join(outDir, "terminal", "strata-q2_0.json"), "qwen3.8-flash-next-q2_0", []string{
				"--native", shard1, "--max-context", "32768",
			})
			payload, _ := json.Marshal(map[string]any{
				"command":      "engine prepare-polystrata",
				"status":       "complete",
				"completeness": "complete",
				"limitations":  []string{},
				"details": map[string]any{
					"installation_path": newInst,
					"model_config_path": cfgPath,
					"log_path":          filepath.Join(outDir, ".inferyard-setup.log"),
				},
			})
			close(execDone)
			return inferyard.Execution{ExitCode: 0, Stdout: payload}, nil
		},
		EngineUpdate: EngineUpdateOptions{
			HTTPClient:        srv.Client(),
			GitHubAPIBase:     srv.URL,
			CachePath:         filepath.Join(base, "update-cache.json"),
			CacheTTL:          6 * time.Hour,
			Platform:          "darwin/arm64",
			AllowInsecureHTTP: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Shutdown(context.Background())

	svc.settingsMu.Lock()
	svc.settings.InferyardPath = ptr(os.Args[0])
	svc.settings.WorkspaceRoot = ptr(ws)
	svc.settings.EngineInstallations = []string{oldInst}
	svc.settingsMu.Unlock()

	status, apiErr := svc.CheckEngineUpdates(ctx)
	if apiErr != nil {
		t.Fatalf("CheckEngineUpdates failed: %+v", apiErr)
	}
	var oldEngName string
	for name, info := range status.Updates {
		if info.Family == "polystrata" {
			oldEngName = name
			if !info.HasUpdate || info.InstalledVersion != "v0.1.45-poly.10" || info.LatestVersion != "v0.2.20-poly.5" {
				t.Fatalf("unexpected update status: %+v", info)
			}
			// Remote tag matches catalog v0.2.20-poly.5 -> must preserve SHA256-pinned commit archive!
			if info.DownloadURL != polystrataDefaultSourceURL || info.AssetBytes != polystrataDefaultSourceBytes {
				t.Fatalf("expected catalog SHA256-pinned URL %q (%dB), got %q (%dB)",
					polystrataDefaultSourceURL, polystrataDefaultSourceBytes, info.DownloadURL, info.AssetBytes)
			}
		}
	}
	if oldEngName == "" {
		t.Fatalf("expected polystrata entry in updates: %+v", status.Updates)
	}

	task, apiErr := svc.UpdateEngine(oldEngName)
	if apiErr != nil {
		t.Fatalf("UpdateEngine failed: %+v", apiErr)
	}
	if task.DisplayName != "PolyStrata · qwen · Q2_0" {
		t.Fatalf("expected inferred DisplayName 'PolyStrata · qwen · Q2_0', got %q", task.DisplayName)
	}
	select {
	case <-execDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for UpdateEngine setup execution")
	}
	for _, pair := range [][2]string{
		{"--family", "qwen"},
		{"--model", "Q2_0"},
		{"--context", "32768"},
		{"--backend", "metal"},
		{"--data-dir", dataDir},
		{"--source", polystrataDefaultSourceURL},
		{"--expected-sha256", polystrataDefaultSourceSHA256},
	} {
		idx := slices.Index(gotArgs, pair[0])
		if idx < 0 || idx+1 >= len(gotArgs) || gotArgs[idx+1] != pair[1] {
			t.Fatalf("expected %s %q in CLI args %v", pair[0], pair[1], gotArgs)
		}
	}

	wait(t, func() bool {
		tasks := svc.Downloads()
		return len(tasks) > 0 && tasks[0].State == "done"
	})

	// New installation writes "0.2.20" in BUILD-poly.json, but receipt identifies exact "v0.2.20-poly.5".
	newInst := filepath.Join(ws, "engines", "polystrata-v0.2.20-poly.5", "installation.json")
	if gotVer := readInstallationVersion(newInst); gotVer != "v0.2.20-poly.5" {
		t.Fatalf("readInstallationVersion = %q, want v0.2.20-poly.5", gotVer)
	}
	updatesAfter := svc.EngineUpdates()
	for name, info := range updatesAfter {
		if info.InstalledVersion == "v0.2.20-poly.5" && info.HasUpdate {
			t.Fatalf("expected new v0.2.20-poly.5 install %q to have HasUpdate=false, got %+v", name, info)
		}
	}

	// Advancing upstream to v0.2.20-poly.6 must NOT be masked by BUILD-poly.json's "0.2.20".
	remoteTag.Store("v0.2.20-poly.6")
	nextStatus, apiErr := svc.CheckEngineUpdates(ctx)
	if apiErr != nil {
		t.Fatalf("CheckEngineUpdates(v0.2.20-poly.6) failed: %+v", apiErr)
	}
	var sawPoly6Update bool
	for _, info := range nextStatus.Updates {
		if info.InstalledVersion == "v0.2.20-poly.5" && info.HasUpdate && info.LatestVersion == "v0.2.20-poly.6" {
			sawPoly6Update = true
		}
	}
	if !sawPoly6Update {
		t.Fatalf("expected v0.2.20-poly.5 install to detect v0.2.20-poly.6 update, got %+v", nextStatus.Updates)
	}
}

func TestPolystrataV0220RegistryUninstallAndConfigMigration(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	dataDir := filepath.Join(ws, "models", "polystrata-data")
	packDir := filepath.Join(dataDir, "packs", "q2_0")
	shard1 := filepath.Join(dataDir, "models", "Q2_0", "Qwen3.8-00001-of-00002.gguf")
	mustMkdir(t, ws, packDir)
	mustWriteFile(t, filepath.Join(packDir, "dense.bin"), bytes.Repeat([]byte("d"), 512), 0o600)
	mustWriteFile(t, shard1, bytes.Repeat([]byte("s"), 2048), 0o600)

	psDir := filepath.Join(ws, "engines", "polystrata-v0220")
	psInst := setupPolystrataV0220InstallFixture(t, psDir, dataDir, "0.2.20", polystrataDefaultSourceURL, polystrataDefaultSourceSHA256)

	hostPython := filepath.Join(base, "uv-python", "bin", "python3.12")
	mustWriteFile(t, hostPython, []byte("#!/bin/sh\n"), 0o755)
	mustWriteFile(t, filepath.Join(psDir, "terminal", ".venv", "pyvenv.cfg"), []byte("home = "+filepath.ToSlash(base)+"\n"), 0o644)
	if runtime.GOOS != "windows" {
		mustMkdir(t, filepath.Join(psDir, "terminal", ".venv", "bin"))
		if err := os.Symlink(hostPython, filepath.Join(psDir, "terminal", ".venv", "bin", "python3")); err != nil {
			t.Fatal(err)
		}
	}

	termCfg := writeStrataConfigFixture(t, filepath.Join(psDir, "terminal", "strata-q2_0.json"), "qwen3.8-flash-next-q2_0", []string{
		"--pack", packDir, "--native", shard1, "--max-context", "32768",
	})

	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.EngineInstallations = []string{psInst}
	s.mu.Unlock()

	reg := s.Registry()
	if len(reg.Engines) != 1 || !reg.Engines[0].Deletable {
		t.Fatalf("expected 1 deletable v0.2.20 PolyStrata engine, got %+v", reg.Engines)
	}
	if len(reg.Models) != 1 || reg.Models[0].Format != "strata_pack" {
		t.Fatalf("expected strata_pack model discovered from terminal/strata-q2_0.json, got %+v", reg.Models)
	}

	engName := reg.Engines[0].Name
	delInfo, apiErr := s.InspectDeleteAsset("engine", engName)
	if apiErr != nil || delInfo.Scope != "polystrata_engine" || !delInfo.KeepsModelData {
		t.Fatalf("InspectDeleteAsset failed: info=%+v err=%+v", delInfo, apiErr)
	}
	if apiErr := s.DeleteManagedAssetVerified("engine", engName, delInfo.TargetPath, delInfo.Fingerprint); apiErr != nil {
		t.Fatalf("DeleteManagedAssetVerified failed: %+v", apiErr)
	}
	if _, err := os.Stat(psDir); !os.IsNotExist(err) {
		t.Fatalf("expected engine directory %s deleted, err=%v", psDir, err)
	}
	if _, err := os.Stat(termCfg); !os.IsNotExist(err) {
		t.Fatalf("expected original terminal config gone, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "strata-q2_0.json")); err != nil {
		t.Fatalf("expected terminal/strata-q2_0.json migrated into dataDir: %v", err)
	}
}
