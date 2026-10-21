//go:build ui_debug && !release

package pages

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"inferyard/studio/internal/core"
)

type previewLocalTask struct {
	op       previewBenchmark
	ids      []string
	index    int
	family   string
	pid      int64
	usedPIDs map[int64]bool
	fail     bool
}

func previewLocalEngines(s *viewState) {
	s.selected, s.task.stage = "dashboard", "prepare_candidate"
	s.registry.Models = []core.ModelAsset{{Name: "local.gguf", Format: "gguf", Available: true}, {Name: "local-weights", Format: "directory", Available: true}}
	s.registry.Engines = nil
	s.settings.Settings.EngineInstallations = nil
	s.checks = core.EngineCheckOverview{Supported: true, Engines: map[string]core.EngineEligibility{}}
	for _, family := range []string{"llama-cpp", "mlx-lm", "vllm", "sglang", "lmstudio"} {
		name := family + " · 模拟安装"
		s.registry.Engines = append(s.registry.Engines, core.EngineAsset{Name: name, Family: family, Kind: "local_installation", Source: "模拟安装声明"})
		s.checks.Engines[name] = core.EngineEligibility{State: core.EngineCheckEligible, Family: family, VerificationScope: "installation", Version: "preview"}
		s.settings.Settings.EngineInstallations = append(s.settings.Settings.EngineInstallations, "/preview/"+family+".json")
	}
	s.model, s.engine = "local-weights", "mlx-lm · 模拟安装"
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
		for _, name := range []string{"llama-cpp", "mlx-lm", "vllm", "sglang", "lmstudio"} {
			if strings.Contains(filepath.Base(op.path), name) {
				family = name
				break
			}
		}
		if family == "" {
			s.failure = &core.ApiError{Code: "preview_installation_fixture_required", Message: "模拟注册请使用文件名含 llama-cpp、mlx-lm、vllm、sglang 或 lmstudio 的 JSON 路径。"}
			return
		}
		name := family + " · " + filepath.Base(op.path)
		s.settings.Settings.EngineInstallations = append(s.settings.Settings.EngineInstallations, op.path)
		s.registry.Engines = append(s.registry.Engines, core.EngineAsset{Name: name, Family: family, Kind: "local_installation", Source: "模拟安装声明"})
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
	p.notice = "模拟引擎列表已更新，仅本窗口有效。"
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
	pid, _ := strconv.ParseInt(s.localPID, 10, 64)
	if !retry {
		p.local.pid = pid
		s.task.runs = nil
	}
	s.busy, s.failure, s.task.failure = true, nil, nil
	s.task.stage, s.task.bundles = "prepare_candidate", allBundles
	s.task.start = startChainResult{workspace: s.workspace, localRequest: &core.LocalCandidateRequest{ModelName: op.model, EngineName: op.engine, Bundles: allBundles}}
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
			p.startLocalBinding(l.pid)
			return
		}
		task.PreparationStage, task.AwaitingService = "awaiting_service", true
		p.copyLocalTask(id)
		return
	}
	s.engineState.State = "stopping"
	p.copyLocalTask(id)
	p.after("task", 400*time.Millisecond, func() {
		task.PreparationStage = "starting_engine"
		p.copyLocalTask(id)
		l.pid = int64(5000 + p.serial + index)
		s.engineState = core.EngineState{State: "starting", Local: true, Pid: l.pid, StartTicks: l.pid * 10, Candidate: emptyText(task.CandidatePath), Endpoint: "http://127.0.0.1:48857"}
		p.after("task", 400*time.Millisecond, func() { p.startLocalBinding(l.pid) })
	})
}

func (p *previewSession) bindLocal(op previewLocalBinding) {
	l := p.local
	if l == nil || l.family != "lmstudio" || l.index >= len(l.ids) || l.ids[l.index] != op.taskID || !p.details[op.taskID].Task.AwaitingService {
		p.state.failure = core.Conflict("invalid_state")
		return
	}
	if op.pid <= 0 || op.endpoint != p.state.localEndpoint {
		p.state.failure = core.Err("candidate_endpoint_mismatch")
		return
	}
	if l.usedPIDs[op.pid] {
		p.state.failure = core.Err("local_engine_service_reused")
		return
	}
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
			p.failLocalScope(core.Err("cli_blocked"))
			return
		}
		l.usedPIDs[pid] = true
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
	s.busy, s.failure, s.task.failure = false, failure, failure
	if l.family != "lmstudio" {
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
