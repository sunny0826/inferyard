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
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

func TestPolystrataManagedCoexistingVersionsDeletableAndUpgradeAutoCleansOld(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	dataDir := filepath.Join(ws, "models", "polystrata-data")
	packDir := filepath.Join(dataDir, "packs", "q2_0")
	shard1 := filepath.Join(dataDir, "models", "Q2_0", "Qwen3.8-00001-of-00002.gguf")
	shard2 := filepath.Join(dataDir, "models", "Q2_0", "Qwen3.8-00002-of-00002.gguf")
	extraGGUF := filepath.Join(dataDir, "models", "gemma4-Q4_K_M", "gemma4.gguf")
	mustMkdir(t, ws, packDir)
	mustWriteFile(t, filepath.Join(packDir, "dense.bin"), bytes.Repeat([]byte("d"), 256), 0o600)
	mustWriteFile(t, shard1, bytes.Repeat([]byte("1"), 1024), 0o600)
	mustWriteFile(t, shard2, bytes.Repeat([]byte("2"), 1024), 0o600)
	mustWriteFile(t, extraGGUF, bytes.Repeat([]byte("g"), 512), 0o600)

	// Part 1: Coexisting poly.5 + poly.6 with shared dataDir/strata-q2_0.json whose exe/cwd/vision.exe point to poly.5.
	poly5Dir := filepath.Join(ws, "engines", "polystrata-v0.2.20-poly.5")
	poly6Dir := filepath.Join(ws, "engines", "polystrata-v0.2.20-poly.6")
	poly5Inst := setupPolystrataV0220InstallFixture(t, poly5Dir, dataDir, "0.2.20", polystrataDefaultSourceURL, polystrataDefaultSourceSHA256)
	poly6Inst := setupPolystrataV0220InstallFixture(t, poly6Dir, dataDir, "0.2.20",
		"https://github.com/simplexai-labs/PolyStrata/archive/refs/tags/v0.2.20-poly.6.tar.gz",
		"9fd665c72c5be294b71fbddd80c039cda65df74df1665021febaf4bb8951c330")

	writeFullCfg := func(path, exeDir string) {
		raw, _ := json.Marshal(map[string]any{
			"exe":        filepath.Join(exeDir, "engine", "build-metal", "strata"),
			"cwd":        filepath.Join(exeDir, "engine"),
			"model_name": "qwen3.8-flash-next-q2_0",
			"log":        filepath.Join(exeDir, "terminal", "strata-q2_0.log"),
			"args": []string{
				"--pack", packDir, "--native", shard1, "--ple-gguf", shard2,
				"--expert-profile", filepath.Join(exeDir, "terminal", "data", "expert-profile.bin"),
				"--max-context", "32768", "--vision",
			},
			"vision": map[string]any{
				"exe":   filepath.Join(exeDir, "engine", "build-vision", "bin", "strata-vision"),
				"model": shard1,
			},
		})
		mustWriteFile(t, path, raw, 0o600)
	}
	writeFullCfg(filepath.Join(poly5Dir, "terminal", "strata-q2_0.json"), poly5Dir)
	writeFullCfg(filepath.Join(poly6Dir, "terminal", "strata-q2_0.json"), poly6Dir)
	sharedCfg := filepath.Join(dataDir, "strata-q2_0.json")
	writeFullCfg(sharedCfg, poly5Dir)

	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.EngineInstallations = []string{poly5Inst, poly6Inst}
	s.mu.Unlock()

	reg := s.Registry()
	var poly5Name string
	for _, eng := range reg.Engines {
		if !eng.Deletable {
			t.Fatalf("expected coexisting managed PolyStrata %q (%s) to be Deletable, got %+v", eng.Name, eng.Path, eng)
		}
		if eng.Path == poly5Inst {
			poly5Name = eng.Name
		}
	}
	if poly5Name == "" {
		t.Fatalf("missing poly.5 in registry: %+v", reg.Engines)
	}
	delInfo, apiErr := s.InspectDeleteAsset("engine", poly5Name)
	if apiErr != nil {
		t.Fatalf("InspectDeleteAsset(poly.5) failed: %+v", apiErr)
	}
	if apiErr := s.DeleteManagedAssetVerified("engine", poly5Name, delInfo.TargetPath, delInfo.Fingerprint); apiErr != nil {
		t.Fatalf("DeleteManagedAssetVerified(poly.5) failed: %+v", apiErr)
	}
	if _, err := os.Stat(poly5Dir); !os.IsNotExist(err) {
		t.Fatalf("expected old poly.5 dir removed, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "strata-q2_0-retained-1.json")); !os.IsNotExist(err) {
		t.Fatalf("must not create redundant strata-q2_0-retained-1.json when surviving model exists")
	}

	// Part 2: Managed one-click UpdateEngine (including failure -> RetryDownload) auto-cleans old managed dir.
	_ = os.Remove(sharedCfg)
	_ = os.RemoveAll(poly6Dir)
	poly5Inst = setupPolystrataV0220InstallFixture(t, poly5Dir, dataDir, "0.2.20", polystrataDefaultSourceURL, polystrataDefaultSourceSHA256)
	writeFullCfg(filepath.Join(poly5Dir, "terminal", "strata-q2_0.json"), poly5Dir)
	writeStrataConfigFixture(t, filepath.Join(poly5Dir, "terminal", "strata-gemma4.json"), "gemma4-q4_k_m", []string{
		"--model", extraGGUF, "--max-context", "16384",
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/repos/simplexai-labs/PolyStrata/releases/latest" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v0.2.20-poly.6",
				"html_url": "https://github.com/simplexai-labs/PolyStrata/releases/tag/v0.2.20-poly.6",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	var attempts atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	upSvc, err := New(ctx, Options{
		SettingsPath:  filepath.Join(base, "up-settings.json"),
		HostStatePath: filepath.Join(base, "up-host.json"),
		Execute: func(ctx context.Context, bin string, args ...string) (inferyard.Execution, error) {
			if len(args) >= 2 && args[0] == "engine" && args[1] == "check" {
				payload, _ := json.Marshal(map[string]any{
					"command": "engine check", "status": "checked", "completeness": "complete",
					"run_id": nil, "evidence_dir": nil, "limitations": []string{},
					"details": map[string]any{
						"definition": "local-engine-check.v2", "eligible": true, "reason": nil, "family": "polystrata",
						"declaration_sha256":  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
						"verification_scope":  "installation_runtime_closure_only",
						"model_requests_sent": 0, "ready_to_run": false,
					},
				})
				return inferyard.Execution{ExitCode: 0, Stdout: payload}, nil
			}
			return assetTestExec(ctx, bin, args...)
		},
		ExecStream: func(_ context.Context, _ func(string), _ string, args ...string) (inferyard.Execution, error) {
			n := attempts.Add(1)
			if n == 1 {
				return inferyard.Execution{ExitCode: 2, Stdout: []byte(`{"command":"engine prepare-polystrata","status":"failed"}`)}, nil
			}
			var outDir string
			if idx := slices.Index(args, "--out"); idx >= 0 && idx+1 < len(args) {
				outDir = args[idx+1]
			}
			newInst := setupPolystrataV0220InstallFixture(t, outDir, dataDir, "0.2.20",
				"https://github.com/simplexai-labs/PolyStrata/archive/refs/tags/v0.2.20-poly.6.tar.gz",
				"9fd665c72c5be294b71fbddd80c039cda65df74df1665021febaf4bb8951c330")
			writeFullCfg(filepath.Join(outDir, "terminal", "strata-q2_0.json"), outDir)
			payload, _ := json.Marshal(map[string]any{
				"command": "engine prepare-polystrata", "status": "complete", "completeness": "complete",
				"run_id": nil, "evidence_dir": nil, "limitations": []string{},
				"details": map[string]any{
					"installation_path": newInst,
					"model_config_path": filepath.Join(outDir, "terminal", "strata-q2_0.json"),
					"log_path":          filepath.Join(outDir, ".inferyard-setup.log"),
				},
			})
			return inferyard.Execution{ExitCode: 0, Stdout: payload}, nil
		},
		EngineUpdate: EngineUpdateOptions{
			HTTPClient: srv.Client(), GitHubAPIBase: srv.URL,
			CachePath: filepath.Join(base, "up-cache.json"), CacheTTL: 6 * time.Hour,
			Platform: "darwin/arm64", AllowInsecureHTTP: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer upSvc.Shutdown(context.Background())

	upSvc.settingsMu.Lock()
	upSvc.settings.InferyardPath = ptr(os.Args[0])
	upSvc.settings.WorkspaceRoot = ptr(ws)
	upSvc.settings.EngineInstallations = []string{poly5Inst}
	upSvc.settingsMu.Unlock()

	if _, apiErr := upSvc.CheckEngineUpdates(ctx); apiErr != nil {
		t.Fatalf("CheckEngineUpdates: %+v", apiErr)
	}
	firstTask, apiErr := upSvc.UpdateEngine("PolyStrata · v0.2.20-poly.5")
	if apiErr != nil {
		t.Fatalf("UpdateEngine: %+v", apiErr)
	}
	wait(t, func() bool {
		for _, d := range upSvc.Downloads() {
			if d.TaskId == firstTask.TaskId && d.State == "failed" {
				return true
			}
		}
		return false
	})
	if _, err := os.Stat(poly5Dir); err != nil {
		t.Fatalf("failed upgrade must keep old managed PolyStrata intact: %v", err)
	}

	retriedTask, apiErr := upSvc.RetryDownload(firstTask.TaskId)
	if apiErr != nil {
		t.Fatalf("RetryDownload: %+v", apiErr)
	}
	wait(t, func() bool {
		for _, d := range upSvc.Downloads() {
			if d.TaskId == retriedTask.TaskId {
				if d.State == "failed" {
					t.Fatalf("retried upgrade unexpectedly failed: %+v", d.Error)
				}
				return d.State == "done"
			}
		}
		return false
	})

	if _, err := os.Stat(poly5Dir); !os.IsNotExist(err) {
		t.Fatalf("expected old managed PolyStrata dir auto-removed after retry success, stat err=%v", err)
	}
	afterReg := upSvc.Registry()
	if len(afterReg.Engines) != 1 || afterReg.Engines[0].Name != "PolyStrata · v0.2.20-poly.6" || !afterReg.Engines[0].Deletable {
		t.Fatalf("expected single deletable PolyStrata · v0.2.20-poly.6 after upgrade, got %+v", afterReg.Engines)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "strata-q2_0.json")); !os.IsNotExist(err) {
		t.Fatalf("covered q2_0 config should not be duplicated into dataDir during upgrade cleanup")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "strata-gemma4.json")); err != nil {
		t.Fatalf("uncovered secondary model strata-gemma4.json must migrate to dataDir: %v", err)
	}
}
