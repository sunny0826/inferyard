//go:build ui_debug && !release

package pages

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/l10n"
)

type previewLocalTask struct {
	op       previewBenchmark
	ids      []string
	index    int
	family   string
	pid      int64
	reused   bool
	usedPIDs map[int64]bool
	fail     bool
}

func previewLocalEngineVersion(family string) string {
	switch family {
	case "polystrata":
		return "v0.2.20-poly.5"
	case "llama-cpp":
		return "prism-b10743-adfffbe"
	case "mlx-lm":
		return "0.32.2"
	case "vllm":
		return "v0.31.0"
	case "sglang":
		return "e262b44"
	case "lmstudio":
		return "0.3.14"
	default:
		return "preview"
	}
}

func previewLocalEngines(s *viewState) {
	s.selected, s.benchFlowOpen, s.task.stage = "library", true, "prepare_candidate"
	s.libraryTab = 1
	s.registry.Models = []core.ModelAsset{
		{Name: "local.gguf", Path: "/preview/models/local.gguf", Format: "gguf", SizeBytes: previewValue(int64(4 << 30)), Available: true},
		{Name: "strata-qwen38-coder-iq1-m.json", Path: "/preview/models/strata-qwen38-coder-iq1-m.json", Format: "strata_pack", SizeBytes: previewValue(int64(58400000000)), Available: true},
		{Name: "local-weights", Path: "/preview/models/local-weights", Format: "directory", SizeBytes: previewValue(int64(3 << 30)), Available: true},
	}
	s.registry.TotalModelBytes = int64(7<<30) + 58400000000
	s.registry.Engines = nil
	s.settings.Settings.EngineInstallations = nil
	s.checks = core.EngineCheckOverview{Supported: true, Engines: map[string]core.EngineEligibility{}}
	for _, family := range []string{"polystrata", "llama-cpp", "mlx-lm", "vllm", "sglang", "lmstudio"} {
		name := l10n.TRf("preview.engine.local_name", "family", family)
		s.registry.Engines = append(s.registry.Engines, core.EngineAsset{Name: name, Family: family, Kind: "local_installation", Source: l10n.TR("preview.source.local_manifest")})
		s.checks.Engines[name] = core.EngineEligibility{State: core.EngineCheckEligible, Family: family, VerificationScope: "installation", Version: previewLocalEngineVersion(family)}
		s.settings.Settings.EngineInstallations = append(s.settings.Settings.EngineInstallations, "/preview/"+family+".json")
	}
	s.model, s.engine = "local-weights", l10n.TRf("preview.engine.local_name", "family", "mlx-lm")
	s.engineState = core.EngineState{State: "stopped"}
	s.downloads, s.tasks = nil, nil
}

func (p *previewSession) installation(op previewInstallation) {
	s := p.state
	if p.takeFailure() {
		s.failure = core.Err("local_engine_installation_rejected")
		return
	}
	switch op.action {
	case "register":
		if !filepath.IsAbs(op.path) {
			s.failure = core.Err("invalid_request")
			return
		}
		if slices.Contains(s.settings.Settings.EngineInstallations, op.path) {
			return
		}
		family := ""
		for _, name := range []string{"polystrata", "llama-cpp", "mlx-lm", "vllm", "sglang", "lmstudio"} {
			if strings.Contains(filepath.Base(op.path), name) {
				family = name
				break
			}
		}
		if family == "" {
			s.failure = &core.ApiError{Code: "preview_installation_fixture_required", Message: l10n.TR("preview.local.fixture_required")}
			return
		}
		name := family + " · " + filepath.Base(op.path)
		s.settings.Settings.EngineInstallations = append(s.settings.Settings.EngineInstallations, op.path)
		s.registry.Engines = append(s.registry.Engines, core.EngineAsset{Name: name, Family: family, Kind: "local_installation", Source: l10n.TR("preview.source.local_manifest")})
		if s.checks.Engines == nil {
			s.checks.Engines = map[string]core.EngineEligibility{}
		}
		s.checks.Engines[name] = core.EngineEligibility{State: core.EngineCheckEligible, Family: family, VerificationScope: "installation"}
	case "remove":
		s.settings.Settings.EngineInstallations = slices.DeleteFunc(s.settings.Settings.EngineInstallations, func(path string) bool { return path == op.path })
		s.registry.Engines = slices.DeleteFunc(s.registry.Engines, func(e core.EngineAsset) bool {
			return e.Kind == "local_installation" && (e.Name == e.Family+" · "+filepath.Base(op.path) || op.path == "/preview/"+e.Family+".json")
		})
	case "recheck":
		for _, e := range s.registry.Engines {
			s.checks.Engines[e.Name] = core.EngineEligibility{State: core.EngineCheckEligible, Family: e.Family, VerificationScope: "installation"}
		}
	default:
		p.showNotice()
		return
	}
	p.noticeKey = "preview.notice.engines_updated"
}

func (p *previewSession) beginLocalBenchmark(op previewBenchmark, retry bool) {
	s := p.state
	if s.busy || s.running() != nil || len(op.bundles) == 0 || s.localEngine() == nil || !s.preflightReady() {
		return
	}
	family := s.localEngine().Family
	used := map[int64]bool{}
	allBundles := slices.Clone(op.bundles)
	if retry && p.local != nil {
		used = p.local.usedPIDs
		allBundles = slices.Clone(s.task.bundles)
	}
	p.cancelEvents("task")
	p.local = &previewLocalTask{op: op, family: family, usedPIDs: used, fail: p.takeFailure()}
	pid := int64(0)
	endpoint := s.localEndpoint
	if op.reusedService != nil && family != "lmstudio" {
		pid = op.reusedService.Pid
		endpoint = op.reusedService.Endpoint
		p.local.pid = pid
		p.local.reused = true
		if !retry {
			s.task.runs = nil
		}
	} else if !retry {
		pid, _ = strconv.ParseInt(s.localPID, 10, 64)
		p.local.pid = pid
		s.task.runs = nil
	} else {
		s.localPID = ""
	}
	s.busy, s.busyAction, s.failure, s.task.failure = true, "prepare-benchmark", nil, nil
	s.task.stage, s.task.bundles = "prepare_candidate", allBundles
	s.task.start = startChainResult{
		workspace:       s.workspace,
		localRequest:    &core.LocalCandidateRequest{ModelName: op.model, EngineName: op.engine, Bundles: allBundles, ServicePID: pid, Endpoint: endpoint},
		existingService: op.reusedService,
	}
	for _, bundle := range allBundles {
		s.task.start.localCandidates = append(s.task.start.localCandidates, core.CandidateRun{Bundle: bundle, Family: family, CandidatePath: filepath.Join(s.workspace, "candidates", p.nextID("candidate"))})
	}
	s.progress.DefaultScopeComplete = false
	p.after("task", 400*time.Millisecond, func() {
		p.local.ids = p.queueRuns(op)
		for i, id := range p.local.ids {
			task := &p.details[id].Task
			task.EngineFamily = family
			task.CandidatePath = previewValue(filepath.Join(s.workspace, "candidates", id))
			p.copyLocalTask(id)
			for j := range s.progress.Bundles {
				b := &s.progress.Bundles[j]
				if b.Bundle == op.bundles[i] {
					b.CandidatePath, b.Phase, b.NextAction = task.CandidatePath, "bound_missing", "bind"
				}
			}
		}
		p.advanceLocalRun(0)
	})
}

func (p *previewSession) copyLocalTask(id string) {
	for i := range p.state.tasks {
		if p.state.tasks[i].TaskId == id {
			p.state.tasks[i] = p.details[id].Task
			return
		}
	}
}

func (p *previewSession) advanceLocalRun(index int) {
	l, s := p.local, p.state
	if index >= len(l.ids) {
		p.finishBenchmark()
		return
	}
	l.index = index
	s.runProgress = nil
	id := l.ids[index]
	task := &p.details[id].Task
	task.Queued, task.PreparationStage = false, "switching_service"
	s.task.stage = "run"
	if l.family == "lmstudio" {
		if index == 0 && l.pid > 0 {
			s.reserveLocalPID(id, l.pid)
			p.startLocalBinding(l.pid)
			return
		}
		task.PreparationStage, task.AwaitingService = "awaiting_service", true
		p.copyLocalTask(id)
		return
	}
	if l.reused && l.pid > 0 {
		p.startLocalBinding(l.pid)
		return
	}
	s.engineState.State = "stopping"
	p.copyLocalTask(id)
	p.after("task", 400*time.Millisecond, func() {
		task.PreparationStage = "starting_engine"
		p.copyLocalTask(id)
		l.pid = int64(5000 + p.serial + index)
		s.engineState = p.previewEngineMetadata("starting", l.pid, true, emptyText(task.CandidatePath))
		s.engineState.StartTicks = l.pid * 10
		p.after("task", 400*time.Millisecond, func() { p.startLocalBinding(l.pid) })
	})
}

func (p *previewSession) bindLocal(op previewLocalBinding) {
	l := p.local
	if l == nil || l.family != "lmstudio" || l.index >= len(l.ids) || l.ids[l.index] != op.taskID || !p.details[op.taskID].Task.AwaitingService {
		p.state.failure = core.Conflict("invalid_state")
		return
	}
	expected := p.state.localExpectedEndpoint()
	if op.pid <= 0 || expected != "" && op.endpoint != expected {
		p.state.failure, p.state.handoffFailure = core.Err("candidate_endpoint_mismatch"), core.Err("candidate_endpoint_mismatch")
		return
	}
	if l.usedPIDs[op.pid] {
		p.state.failure, p.state.handoffFailure = core.Err("local_engine_service_reused"), core.Err("local_engine_service_reused")
		return
	}
	p.state.handoffFailure = nil
	if p.state.localPendingPIDs == nil {
		p.state.localPendingPIDs = map[string]int64{}
	}
	p.state.localPendingPIDs[op.taskID] = op.pid
	l.fail = p.takeFailure()
	p.startLocalBinding(op.pid)
}

func (p *previewSession) startLocalBinding(pid int64) {
	l, s := p.local, p.state
	id := l.ids[l.index]
	task := &p.details[id].Task
	task.AwaitingService, task.PreparationStage = false, "binding"
	if l.family != "lmstudio" {
		s.engineState.State = "running"
	}
	p.copyLocalTask(id)
	p.after("task", 400*time.Millisecond, func() {
		if l.fail {
			s.handoffFailure = core.Err("cli_blocked")
			delete(s.localPendingPIDs, id)
			p.failLocalScope(core.Err("cli_blocked"))
			return
		}
		l.usedPIDs[pid] = true
		s.markServiceUsed(pid)
		delete(s.localPendingPIDs, id)
		s.handoffFailure, s.bindFailure, s.failure = nil, nil, nil
		task.PreparationStage = ""
		p.copyLocalTask(id)
		p.startRun(l.index, l.op, l.ids)
	})
}

func (p *previewSession) failLocalScope(failure *core.ApiError) {
	l, s := p.local, p.state
	for i := l.index; i < len(l.ids); i++ {
		id := l.ids[i]
		p.terminalRun(id, "failed")
		err := failure
		if i > l.index {
			err = core.Err("previous_scope_incomplete")
		}
		p.details[id].Task.Error = err
		p.copyLocalTask(id)
	}
	s.busy, s.busyAction, s.failure, s.task.failure = false, "", failure, failure
	if l.family != "lmstudio" && !l.reused {
		s.engineState.State = "stopped"
	}
}

func (p *previewSession) retryLocalBenchmark() {
	if p.local == nil || p.state.busy || p.state.task.failure == nil {
		return
	}
	op := p.local.op
	op.bundles = nil
	for _, bundle := range p.state.task.bundles {
		complete := false
		for _, task := range p.state.tasks {
			if emptyText(task.Bundle) == bundle && task.State == "done" && slices.Contains(p.state.task.runs, emptyText(task.RunPath)) {
				complete = true
			}
		}
		if !complete {
			op.bundles = append(op.bundles, bundle)
		}
	}
	p.beginLocalBenchmark(op, true)
}

func (p *previewSession) detectedEngines() []inferyard.DetectedEngine {
	lmsModels := "/Users/demo/.lmstudio/models"
	psDataDir := "/Users/demo/PolyStrata-data"
	psVersion := "v0.2.20-poly.5"
	return []inferyard.DetectedEngine{
		{
			Family:           "polystrata",
			Executable:       "/Users/demo/PolyStrata/.venv/bin/python",
			WorkingDirectory: "/Users/demo/PolyStrata",
			RuntimeRoots:     []string{"/Users/demo/PolyStrata/.venv", "/Users/demo/PolyStrata/serve"},
			DataDir:          &psDataDir,
			Version:          &psVersion,
			Label:            "polystrata · v0.2.20-poly.5 (/Users/demo/PolyStrata)",
		},
		{
			Family:           "llama-cpp",
			Executable:       "/opt/homebrew/bin/llama-server",
			WorkingDirectory: "/opt/homebrew/bin",
			RuntimeRoots:     []string{"/opt/homebrew/bin"},
			Label:            "llama-cpp · /opt/homebrew/bin/llama-server",
		},
		{
			Family:           "mlx-lm",
			Executable:       "/Users/demo/.local/share/uv/tools/mlx-lm/bin/python",
			WorkingDirectory: "/Users/demo/.local/share/uv/tools/mlx-lm",
			RuntimeRoots:     []string{"/Users/demo/.local/share/uv/tools/mlx-lm"},
			Label:            "mlx-lm · mlx-lm (uv tool)",
		},
		{
			Family:           "lmstudio",
			Executable:       "/Users/demo/.lmstudio/bin/lms",
			WorkingDirectory: "/Users/demo/.lmstudio/bin",
			RuntimeRoots:     []string{"/Users/demo/.lmstudio/bin"},
			ModelsRoot:       &lmsModels,
			Label:            "lmstudio · /Users/demo/.lmstudio/bin/lms",
		},
	}
}
