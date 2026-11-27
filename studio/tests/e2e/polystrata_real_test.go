package e2e

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"inferyard/studio/internal/core"
)

func TestPolystrataRealLifecycle(t *testing.T) {
	if os.Getenv("INFERYARD_POLYSTRATA_E2E") != "1" {
		t.Skip("skipping real-machine PolyStrata E2E (set INFERYARD_POLYSTRATA_E2E=1 to run)")
	}
	cli := ResolveCLI(t)
	env := NewEnv(t, cli)
	env.Options.EngineStartTimeout = 5 * time.Minute
	ctx := context.Background()

	runInitOut := filepath.Join(filepath.Dir(env.Settings), "stale-polystrata", "installation.json")
	writeJSONFile(t, env.Settings, map[string]any{
		"engine_installations": []string{runInitOut},
	})

	service, err := core.New(ctx, env.Options)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		shutdownCtx, stop := context.WithTimeout(context.Background(), 45*time.Second)
		defer stop()
		_ = service.Shutdown(shutdownCtx)
	}()

	mustJSON := func(v any) json.RawMessage {
		data, _ := json.Marshal(v)
		return data
	}
	if _, apiErr := service.UpdateSettings(map[string]json.RawMessage{
		"inferyard_path": mustJSON(cli),
	}); apiErr != nil {
		t.Fatalf("UpdateSettings(inferyard_path): %+v", apiErr)
	}

	runWizard(t, service, ctx, "inferyard init --out "+quote(env.Workspace), env.Recorder)
	staged := stageLocalPolystrata(t, env.Workspace)

	if _, apiErr := service.RegisterEngineInstallation(ctx, staged.InstPath); apiErr != nil {
		t.Fatalf("RegisterEngineInstallation(%s) failed: %+v [%s]", staged.InstPath, apiErr, lastStderr(env.Recorder))
	}
	_ = service.RefreshAssets()
	_ = service.RefreshDevice()
	service.VerifyEngines()

	// Wait for DeviceCheck snapshot so memory budgeting exercises real hardware constraints.
	devDeadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(devDeadline) {
		dev := service.Device()
		if !dev.Refreshing && len(dev.Snapshot) > 0 {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	if len(service.Device().Snapshot) == 0 {
		t.Fatalf("device snapshot was not populated: %+v", service.Device())
	}

	var modelAsset core.ModelAsset
	var engineAsset core.EngineAsset
	regDeadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(regDeadline) {
		reg := service.Registry()
		checks := service.EngineChecks()
		for _, m := range reg.Models {
			if m.Format == "strata_pack" && m.Available {
				modelAsset = m
			}
		}
		for _, e := range reg.Engines {
			if e.Family == "polystrata" && e.Kind == "local_installation" {
				if chk, ok := checks.Engines[e.Name]; ok && chk.State == core.EngineCheckEligible {
					engineAsset = e
				}
			}
		}
		if modelAsset.Name != "" && engineAsset.Name != "" {
			break
		}
		service.VerifyEngines()
		time.Sleep(200 * time.Millisecond)
	}
	if modelAsset.Name == "" || engineAsset.Name == "" {
		t.Fatalf("PolyStrata model/engine did not reach eligible state: registry=%+v checks=%+v", service.Registry(), service.EngineChecks())
	}
	for _, m := range service.Registry().Models {
		if m.Format == "gguf" {
			t.Fatalf("raw GGUF shard inside polystrata-data leaked into Registry as standalone GGUF: %+v", m)
		}
	}
	if !modelAsset.Deletable || !engineAsset.Deletable {
		t.Fatalf("staged workspace PolyStrata assets must be deletable: model=%+v engine=%+v", modelAsset, engineAsset)
	}

	// Stage 1: Memory budget check + Standalone start #1 (including preset GPU layers & CPU threads).
	for _, checkCtx := range []int64{8192, 32768} {
		budget := service.CheckServiceMemoryBudget(modelAsset.Name, engineAsset.Name, checkCtx)
		t.Logf("memory budget (ctx=%d): %+v", checkCtx, budget)
		if !budget.Known || !budget.Enough {
			t.Fatalf("CheckServiceMemoryBudget(ctx=%d) rejected local PolyStrata model: %+v", checkCtx, budget)
		}
	}

	const standaloneCtx int64 = 8192
	gpuLayers := -1
	threads := 4
	started, apiErr := service.StartStandalone(ctx, core.StandaloneStartRequest{
		Model:       modelAsset.Name,
		Engine:      engineAsset.Name,
		ContextSize: standaloneCtx,
		GPULayers:   &gpuLayers,
		Threads:     &threads,
	})
	if apiErr != nil {
		t.Fatalf("StartStandalone #1 failed: %+v [%s]", apiErr, lastStderr(env.Recorder))
	}
	waitEngineRunning(t, service, 5*time.Minute)
	running := service.Engine()
	if running.Family != "polystrata" || running.Origin != core.EngineOriginStandalone || running.ServedModel == "" {
		t.Fatalf("unexpected running PolyStrata state: %+v", running)
	}

	// Verify /health and Studio's TestServiceEndpoint chat completion probe.
	healthResp, err := http.Get(running.Endpoint + "/health")
	if err != nil {
		t.Fatalf("GET /health failed: %v", err)
	}
	healthBody, _ := io.ReadAll(healthResp.Body)
	_ = healthResp.Body.Close()
	if healthResp.StatusCode != http.StatusOK || !strings.Contains(string(healthBody), `"loaded":true`) && !strings.Contains(string(healthBody), `"loaded": true`) {
		t.Fatalf("unexpected /health: status=%d body=%s", healthResp.StatusCode, string(healthBody))
	}

	probeRes := service.TestServiceEndpoint(ctx, running.Endpoint+"/v1", running.ServedModel)
	t.Logf("TestServiceEndpoint result: %+v", probeRes)
	if !probeRes.OK || probeRes.StatusCode != http.StatusOK || strings.TrimSpace(probeRes.Reply) == "" {
		logs, _ := service.ServiceLogTail(running.ServiceID, 40)
		t.Fatalf("TestServiceEndpoint failed or returned empty reply: %+v\nservice logs:\n%s", probeRes, strings.Join(logs, "\n"))
	}

	var measuredGB float64
	memDeadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(memDeadline) {
		st, ok := service.ManagedService(started.ServiceID)
		if ok && st.RuntimeMemoryGB != nil {
			measuredGB = *st.RuntimeMemoryGB
			if measuredGB >= 1.0 {
				break
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	if measuredGB < 1.0 {
		t.Fatalf("expected live RuntimeMemoryGB >= 1.0 GiB across PolyStrata server + worker processes, got %.2f GiB", measuredGB)
	}

	ownerFile := filepath.Join(env.Workspace, "engine-owners", started.ServiceID+".json")
	if _, err := os.Stat(ownerFile); err != nil {
		t.Fatalf("expected standalone owner file at %s: %v", ownerFile, err)
	}

	if _, stopErr := service.StopManagedService(started.ServiceID); stopErr != nil {
		t.Fatalf("StopManagedService #1 failed: %+v", stopErr)
	}
	waitManagedServiceStopped(t, service, started.ServiceID, 45*time.Second)
	if _, err := os.Stat(ownerFile); !os.IsNotExist(err) {
		t.Fatalf("expected owner file removed after stop, err=%v", err)
	}

	// Stage 2: Second start reuses serve-plan.json; third start verifies explicit Port + LANAccess.
	countServePrepare := func() int {
		n := 0
		for _, rec := range env.Recorder.Records() {
			if len(rec.Argv) >= 3 && rec.Argv[1] == "serve" && rec.Argv[2] == "prepare" {
				n++
			}
		}
		return n
	}
	if got := countServePrepare(); got != 1 {
		t.Fatalf("expected 1 serve prepare call after first start, got %d", got)
	}
	if _, apiErr := service.StartStandalone(ctx, core.StandaloneStartRequest{
		Model:       modelAsset.Name,
		Engine:      engineAsset.Name,
		ContextSize: standaloneCtx,
	}); apiErr != nil {
		t.Fatalf("StartStandalone #2 (cached plan) failed: %+v", apiErr)
	}
	waitEngineRunning(t, service, 5*time.Minute)
	if got := countServePrepare(); got != 1 {
		t.Fatalf("expected cached serve-plan.json reuse for PolyStrata, serve prepare calls=%d", got)
	}
	if _, stopErr := service.StopManagedService(started.ServiceID); stopErr != nil {
		t.Fatalf("StopManagedService #2 failed: %+v", stopErr)
	}
	waitManagedServiceStopped(t, service, started.ServiceID, 45*time.Second)

	lanPort := freePortInt(t)
	if _, apiErr := service.StartStandalone(ctx, core.StandaloneStartRequest{
		Model:       modelAsset.Name,
		Engine:      engineAsset.Name,
		ContextSize: standaloneCtx,
		Port:        lanPort,
		LANAccess:   true,
	}); apiErr != nil {
		t.Fatalf("StartStandalone #3 (LAN + custom port) failed: %+v [%s]", apiErr, lastStderr(env.Recorder))
	}
	waitEngineRunning(t, service, 5*time.Minute)
	lanRunning := service.Engine()
	if !lanRunning.LANAccess || lanRunning.Port != lanPort {
		t.Fatalf("expected LANAccess=true port=%d, got %+v", lanPort, lanRunning)
	}
	if lanProbe := service.TestServiceEndpoint(ctx, lanRunning.Endpoint+"/v1", lanRunning.ServedModel); !lanProbe.OK {
		t.Fatalf("LAN TestServiceEndpoint failed: %+v", lanProbe)
	}
	if _, stopErr := service.StopManagedService(started.ServiceID); stopErr != nil {
		t.Fatalf("StopManagedService #3 failed: %+v", stopErr)
	}
	waitManagedServiceStopped(t, service, started.ServiceID, 45*time.Second)

	// Stage 3: Studio Local Benchmark flow (with reasoning_effort=low) -> CreateReport -> ReportIndex.
	if _, apiErr := service.UpdateSettings(map[string]json.RawMessage{
		"reasoning_effort": mustJSON(core.ReasoningEffortLow),
	}); apiErr != nil {
		t.Fatalf("UpdateSettings(reasoning_effort): %+v", apiErr)
	}
	candidates, apiErr := service.PrepareLocalCandidates(ctx, core.LocalCandidateRequest{
		ModelName:  modelAsset.Name,
		EngineName: engineAsset.Name,
		Bundles:    []string{"zh-svg-pelican"},
	})
	if apiErr != nil {
		t.Fatalf("PrepareLocalCandidates failed: %+v [%s]", apiErr, lastStderr(env.Recorder))
	}
	runTasks, apiErr := service.StartCandidateRuns(core.CandidateRunsRequest{
		Candidates:        candidates,
		AllowSingleBundle: true,
	})
	if apiErr != nil || len(runTasks) != 1 {
		t.Fatalf("StartCandidateRuns failed: tasks=%+v err=%+v", runTasks, apiErr)
	}
	waitTasksTerminal(t, service, 25*time.Minute, env.Recorder)

	doneTasks := service.RunTasks()
	doneTask := doneTasks[len(doneTasks)-1]
	if doneTask.RunPath == nil || doneTask.Exit == nil || doneTask.Exit.Completeness == nil || *doneTask.Exit.Completeness != "complete" {
		t.Fatalf("unexpected benchmark task completion: %+v", doneTask)
	}
	if _, stopErr := service.StopEngine(); stopErr != nil {
		t.Fatalf("StopEngine after benchmark failed: %+v", stopErr)
	}
	stopBenchDeadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(stopBenchDeadline) {
		if service.Engine().State == "stopped" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	report, apiErr := service.CreateReport(core.CreateReportRequest{
		RunPaths: []string{*doneTask.RunPath},
	})
	if apiErr != nil {
		t.Fatalf("CreateReport failed: %+v [%s]", apiErr, lastStderr(env.Recorder))
	}
	reportIdx, apiErr := service.ReportIndex(report.ReportId)
	if apiErr != nil || reportIdx == nil || len(reportIdx.Runs) != 1 {
		t.Fatalf("ReportIndex(%s) failed: idx=%+v err=%+v", report.ReportId, reportIdx, apiErr)
	}
	assertReportExists(t, env.Workspace)

	// Stage 4: Safe managed deletion on staged CoW clone (host /Users/xd/.inferyard verified untouched in Cleanup).
	modDelInfo, apiErr := service.InspectDeleteAsset("model", modelAsset.Name)
	if apiErr != nil {
		t.Fatalf("InspectDeleteAsset(model) failed: %+v", apiErr)
	}
	if modDelInfo.Scope != "polystrata_model" || modDelInfo.ReclaimableBytes == nil || *modDelInfo.ReclaimableBytes <= 0 {
		t.Fatalf("unexpected model delete inspection: %+v", modDelInfo)
	}
	if apiErr := service.DeleteManagedAssetVerified("model", modelAsset.Name, modDelInfo.TargetPath, modDelInfo.Fingerprint); apiErr != nil {
		t.Fatalf("DeleteManagedAssetVerified(model) failed: %+v", apiErr)
	}
	if _, err := os.Stat(modDelInfo.TargetPath); !os.IsNotExist(err) {
		t.Fatalf("expected staged PolyStrata model config removed at %s, stat err=%v", modDelInfo.TargetPath, err)
	}
	for _, covered := range modelAsset.CoveredPaths {
		if rel, rErr := filepath.Rel(staged.DataDir, covered); rErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			if _, err := os.Stat(covered); !os.IsNotExist(err) {
				t.Fatalf("expected covered staged PolyStrata model weight removed at %s, stat err=%v", covered, err)
			}
		}
	}

	engDelInfo, apiErr := service.InspectDeleteAsset("engine", engineAsset.Name)
	if apiErr != nil {
		t.Fatalf("InspectDeleteAsset(engine) failed: %+v", apiErr)
	}
	if engDelInfo.Scope != "polystrata_engine" {
		t.Fatalf("unexpected engine delete inspection: %+v", engDelInfo)
	}
	if apiErr := service.DeleteManagedAssetVerified("engine", engineAsset.Name, engDelInfo.TargetPath, engDelInfo.Fingerprint); apiErr != nil {
		t.Fatalf("DeleteManagedAssetVerified(engine) failed: %+v", apiErr)
	}
	if _, err := os.Stat(staged.WorkDir); !os.IsNotExist(err) {
		t.Fatalf("expected staged PolyStrata engine dir removed, stat err=%v", err)
	}
}
