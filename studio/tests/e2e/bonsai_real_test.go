package e2e

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"inferyard/studio/internal/core"
)

func TestBonsaiRealLifecycle(t *testing.T) {
	if os.Getenv("INFERYARD_BONSAI_E2E") != "1" {
		t.Skip("skipping real-machine Bonsai E2E (set INFERYARD_BONSAI_E2E=1 to run)")
	}
	realHome, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(realHome) == "" {
		t.Fatalf("resolve real HOME: %v", err)
	}
	isolatedHome := filepath.Join(t.TempDir(), "home")
	if mkErr := os.MkdirAll(isolatedHome, 0o700); mkErr != nil {
		t.Fatal(mkErr)
	}
	t.Setenv("HOME", isolatedHome)

	cli := ResolveCLI(t)
	env := NewEnv(t, cli)
	env.Options.EngineStartTimeout = 3 * time.Minute
	ctx := context.Background()

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
	staged := stageLocalBonsai(t, realHome, env.Workspace)

	_ = service.RefreshAssets()
	_ = service.RefreshDevice()
	service.VerifyEngines()

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

	canonChosen, _ := filepath.EvalSymlinks(staged.ChosenModelPath)
	if canonChosen == "" {
		canonChosen = staged.ChosenModelPath
	}
	canonBin, _ := filepath.EvalSymlinks(staged.EngineBin)
	if canonBin == "" {
		canonBin = staged.EngineBin
	}

	var modelAsset core.ModelAsset
	var engineAsset core.EngineAsset
	regDeadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(regDeadline) {
		reg := service.Registry()
		checks := service.EngineChecks()
		for _, m := range reg.Models {
			mCanon, _ := filepath.EvalSymlinks(m.Path)
			if mCanon == "" {
				mCanon = m.Path
			}
			if m.Format == "gguf" && m.Available && (mCanon == canonChosen || m.Path == staged.ChosenModelPath) {
				modelAsset = m
			}
		}
		for _, e := range reg.Engines {
			eCanon, _ := filepath.EvalSymlinks(e.Path)
			if eCanon == "" {
				eCanon = e.Path
			}
			if e.Family == "llama-cpp" && (eCanon == canonBin || e.Path == staged.EngineBin) {
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
		t.Fatalf("Bonsai model/engine did not reach eligible state: registry=%+v checks=%+v", service.Registry(), service.EngineChecks())
	}
	if !strings.EqualFold(modelAsset.Architecture, "qwen35") {
		t.Fatalf("expected Bonsai GGUF architecture qwen35, got %+v", modelAsset)
	}
	if modelAsset.ContextLength == nil || *modelAsset.ContextLength < 16384 {
		t.Fatalf("expected Bonsai GGUF context length >= 16384, got %+v", modelAsset)
	}
	if engineAsset.Distribution != "PrismML" || !engineAsset.Managed {
		t.Fatalf("expected managed PrismML llama-cpp engine, got %+v", engineAsset)
	}
	if !modelAsset.Deletable || !engineAsset.Deletable {
		t.Fatalf("staged workspace Bonsai assets must be deletable: model=%+v engine=%+v", modelAsset, engineAsset)
	}

	// Stage 1: Memory budget check + Standalone start #1.
	for _, checkCtx := range []int64{4096, 16384} {
		budget := service.CheckServiceMemoryBudget(modelAsset.Name, engineAsset.Name, checkCtx)
		t.Logf("memory budget (ctx=%d): %+v", checkCtx, budget)
		if !budget.Known || !budget.Enough {
			t.Fatalf("CheckServiceMemoryBudget(ctx=%d) rejected local Bonsai model: %+v", checkCtx, budget)
		}
	}

	const standaloneCtx int64 = 4096
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
	waitEngineRunning(t, service, 3*time.Minute)
	running := service.Engine()
	if running.Family != "llama-cpp" || running.Origin != core.EngineOriginStandalone || running.ServedModel == "" {
		t.Fatalf("unexpected running Bonsai standalone state: %+v", running)
	}

	healthResp, err := http.Get(running.Endpoint + "/health")
	if err != nil {
		t.Fatalf("GET /health failed: %v", err)
	}
	healthBody, _ := io.ReadAll(healthResp.Body)
	_ = healthResp.Body.Close()
	if healthResp.StatusCode != http.StatusOK || !strings.Contains(string(healthBody), "ok") {
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
			if measuredGB >= 3.0 {
				break
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	if measuredGB < 3.0 {
		t.Fatalf("expected live RuntimeMemoryGB >= 3.0 GiB for Bonsai 27B, got %.2f GiB", measuredGB)
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

	// Stage 2: Cached serve-plan.json reuse + LANAccess on explicit port.
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
	waitEngineRunning(t, service, 3*time.Minute)
	if got := countServePrepare(); got != 1 {
		t.Fatalf("expected cached serve-plan.json reuse for Bonsai, serve prepare calls=%d", got)
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
	waitEngineRunning(t, service, 3*time.Minute)
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

	// Stage 3: Single-bundle real benchmark (zh-svg-pelican) -> studio.json report.
	preflightDir := filepath.Join(env.Workspace, "preflight", "device-bonsai")
	devRes, devErr := service.WizardExecute(ctx, core.WizardRequest{
		ModelName: modelAsset.Name,
		Command: commandOf([]string{
			"device-check", "--json", "--out", preflightDir,
		}),
	})
	if devErr != nil || devRes.ExitCode != 0 {
		t.Fatalf("wizard device-check failed: err=%+v res=%+v [%s]", devErr, devRes, lastStderr(env.Recorder))
	}

	candDir := filepath.Join(env.Workspace, "candidates", "zh-svg-pelican-1")
	createArgs := []string{
		"config", "create",
		"--workspace", env.Workspace,
		"--bundle", "zh-svg-pelican",
		"--engine", staged.EngineBin,
		"--preflight", filepath.Join(preflightDir, "device-preflight.json"),
		"--out", candDir,
	}
	if staged.ChosenSourceJSON != "" {
		createArgs = append(createArgs, "--model-source", staged.ChosenSourceJSON)
	}
	runWizard(t, service, ctx, commandOf(createArgs), env.Recorder)

	candToml := filepath.Join(candDir, "candidate.toml")
	if _, apiErr := service.StartEngine(candToml); apiErr != nil {
		t.Fatalf("StartEngine(%s) failed: %+v", candToml, apiErr)
	}
	waitEngineRunning(t, service, 3*time.Minute)
	benchEng := service.Engine()

	boundDir := filepath.Join(env.Workspace, "bound", "zh-svg-pelican-1")
	runWizard(t, service, ctx, commandOf([]string{
		"config", "bind",
		"--candidate", candToml,
		"--pid", strconv.FormatInt(benchEng.Pid, 10),
		"--endpoint", benchEng.Endpoint,
		"--out", boundDir,
	}), env.Recorder)

	allowSingle := true
	runTasks, apiErr := service.StartRuns(core.StartRunsRequest{
		ConfigPaths:       []string{filepath.Join(boundDir, "config.toml")},
		AllowSingleBundle: &allowSingle,
	})
	if apiErr != nil || len(runTasks) != 1 {
		t.Fatalf("StartRuns failed: tasks=%+v err=%+v", runTasks, apiErr)
	}
	waitTasksTerminal(t, service, 10*time.Minute, env.Recorder)

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

	// Stage 4: Safe managed deletion on staged CoW assets (host ~/.inferyard verified unchanged in Cleanup).
	modDelInfo, apiErr := service.InspectDeleteAsset("model", modelAsset.Name)
	if apiErr != nil {
		t.Fatalf("InspectDeleteAsset(model) failed: %+v", apiErr)
	}
	if modDelInfo.Scope != "standard" || modDelInfo.ReclaimableBytes == nil || *modDelInfo.ReclaimableBytes <= 0 {
		t.Fatalf("unexpected model delete inspection: %+v", modDelInfo)
	}
	if apiErr := service.DeleteManagedAssetVerified("model", modelAsset.Name, modDelInfo.TargetPath, modDelInfo.Fingerprint); apiErr != nil {
		t.Fatalf("DeleteManagedAssetVerified(model) failed: %+v", apiErr)
	}
	if _, err := os.Stat(modDelInfo.TargetPath); !os.IsNotExist(err) {
		t.Fatalf("expected staged Bonsai model removed at %s, stat err=%v", modDelInfo.TargetPath, err)
	}

	engDelInfo, apiErr := service.InspectDeleteAsset("engine", engineAsset.Name)
	if apiErr != nil {
		t.Fatalf("InspectDeleteAsset(engine) failed: %+v", apiErr)
	}
	if engDelInfo.Scope != "standard" || engDelInfo.ReclaimableBytes == nil || *engDelInfo.ReclaimableBytes <= 0 {
		t.Fatalf("unexpected engine delete inspection: %+v", engDelInfo)
	}
	if apiErr := service.DeleteManagedAssetVerified("engine", engineAsset.Name, engDelInfo.TargetPath, engDelInfo.Fingerprint); apiErr != nil {
		t.Fatalf("DeleteManagedAssetVerified(engine) failed: %+v", apiErr)
	}
	if _, err := os.Stat(staged.EngineDir); !os.IsNotExist(err) {
		t.Fatalf("expected staged Bonsai engine dir removed, stat err=%v", err)
	}
}
