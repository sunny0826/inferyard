//go:build ui_debug && !release

package pages

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"inferyard/studio/internal/core"
)

func (p *previewSession) dispatch(intent previewIntent) {
	s := p.state
	s.failure, p.notice = nil, ""
	switch op := intent.(type) {
	case previewManagedEngine:
		p.startService(false, op.candidate)
	case previewSpeedStart:
		p.startSpeed(op)
	case previewSpeedCancel:
		p.cancelSpeed(nil)
	case previewSpeedRead:
		p.readSpeed(op.id)
	case previewBenchmark:
		p.beginBenchmark(op)
	case previewDownload:
		p.startDownload(op)
	case previewCancelDownload:
		p.cancelDownload(op.id)
	case previewRetryDownload:
		for _, task := range s.downloads {
			if task.TaskId == op.id && terminal(task.State) {
				p.startDownload(previewDownload{kind: task.Kind, source: task.SourceUrl})
				break
			}
		}
	case previewSettings:
		p.saveSettings(op.patch)
	case previewInstallation:
		p.installation(op)
	case previewLocalBinding:
		p.bindLocal(op)
	case previewCreateReport:
		fail := p.takeFailure()
		s.busy = true
		p.after("report", 600*time.Millisecond, func() {
			s.busy = false
			if fail {
				s.failure = core.Err("report_unreadable")
				return
			}
			p.createReport(op.paths)
			p.notice = "模拟报告已生成。"
		})
	case previewStartRun:
		s.resumeSource = nil
		p.beginBenchmark(previewBenchmark{model: s.model, engine: s.engine, bundles: []string{op.bundle}, resumePath: op.resumePath})
	case previewCancelRun:
		p.cancelRun(op.id)
	case previewStep:
		p.dispatchStep(op)
	default:
		p.showNotice()
	}
}

func (p *previewSession) dispatchStep(op previewStep) {
	s := p.state
	switch op {
	case previewPrepareEnvironment:
		p.prepareEnvironment()
	case previewCheckDevice:
		p.checkDevice()
	case previewStartEngine, previewRetryBenchmark:
		if op == previewRetryBenchmark && p.local != nil {
			p.retryLocalBenchmark()
			return
		}
		if s.task.stage == "bind" {
			names := s.task.bundles
			if len(names) == 0 {
				names = s.checkedBundles()
			}
			p.beginBenchmark(previewBenchmark{model: s.model, engine: s.engine, bundles: names})
		} else {
			p.startService(true, s.candidate)
		}
	case previewBind:
		p.startService(true, s.candidate)
	case previewStopEngine:
		if s.running() != nil || (s.speed != nil && s.speed.current != nil && s.speed.current.active()) {
			s.failure = core.Err("run_active")
			return
		}
		if p.takeFailure() {
			s.failure = core.Err("engine_stop_failed")
			return
		}
		p.cancelEvents("service")
		s.engineState.State = "stopping"
		p.after("service", 400*time.Millisecond, func() {
			s.engineState = core.EngineState{State: "stopped"}
		})
	case previewRefreshDevice:
		fail := p.takeFailure()
		s.device.Refreshing, s.device.LastError = true, nil
		p.after("device", 600*time.Millisecond, func() {
			s.device.Refreshing = false
			if fail {
				s.device.LastError = core.Err("device_preflight_blocked")
				return
			}
			s.device.Snapshot = previewBase(p.ctx).device.Snapshot
			s.device.CapturedAt = previewValue(p.timestamp())
			if s.progress != nil && s.progress.DevicePreflight.Status == "blocked" {
				s.progress.DevicePreflight.Status, s.progress.NextAction = "ok", "prepare_candidate"
			}
			p.notice = "模拟设备信息已更新。"
		})
	case previewRefreshDiscovery:
		fail := p.takeFailure()
		s.discovering = true
		p.after("discovery", 500*time.Millisecond, func() {
			s.discovering, s.discoveryLoaded = false, true
			s.discoveryError = nil
			if fail {
				s.discoveryError = core.Err("service_discovery_error")
			}
		})
	case previewReload:
		fail := p.takeFailure()
		s.loaded, s.refreshing = true, true
		p.after("reload", 400*time.Millisecond, func() {
			s.refreshing, s.refreshFailure = false, ""
			if fail {
				s.refreshFailure = "模拟刷新失败，可重试。"
				return
			}
			s.historyError, s.reportsError = nil, nil
			p.notice = "模拟页面数据已刷新。"
		})
	case previewVerifyReport:
		if s.progress != nil {
			if p.takeFailure() {
				s.failure = &core.ApiError{Code: "preview_verify_failed", Message: "模拟核验失败，可重试。"}
				return
			}
			s.progress.Report.Status = "verified"
			p.notice = "模拟核验完成。"
		}
	default:
		p.showNotice()
	}
}

func (p *previewSession) prepareEnvironment() {
	s := p.state
	if s.busy {
		return
	}
	fail := p.takeFailure()
	s.busy, s.cliInstallStatus = true, "正在模拟准备环境…"
	p.after("environment", 600*time.Millisecond, func() {
		s.busy, s.cliInstallStatus = false, ""
		if fail {
			s.failure = core.Err("install_failed")
			return
		}
		s.ready.CLI.Status = "ok"
		s.ready.Workspace.Exists, s.ready.Workspace.Initialized = true, true
		s.ready.Results.Exists, s.ready.Models.Exists = true, true
		s.progress = previewBase(p.ctx).progress
		s.progress.Workspace = s.workspace
		s.progress.NextAction, s.progress.DevicePreflight.Status = "device_check", "missing"
		s.task.stage = "prepare_candidate"
		p.checkDevice()
	})
}

func (p *previewSession) checkDevice() {
	s := p.state
	if s.busy || s.progress == nil || s.progress.DevicePreflight.Status != "missing" || s.needsTaskAssets() {
		return
	}
	fail := p.takeFailure()
	s.busy = true
	p.after("environment", 600*time.Millisecond, func() {
		s.busy = false
		if fail {
			s.failure = core.Err("device_preflight_blocked")
			s.progress.DevicePreflight.Status = "blocked"
			return
		}
		s.progress.NextAction, s.progress.DevicePreflight.Status = "prepare_candidate", "ok"
		s.progress.DevicePreflight.Path = previewValue(filepath.Join(s.workspace, "preflight"))
		p.notice = "模拟环境与设备检查已完成。"
	})
}

func (p *previewSession) saveSettings(patch map[string]json.RawMessage) {
	s := p.state
	data, _ := json.Marshal(s.settings.Settings)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(data, &fields)
	for key, value := range patch {
		fields[key] = value
	}
	data, _ = json.Marshal(fields)
	var value core.UserSettings
	_ = json.Unmarshal(data, &value)
	workspaceChanged := emptyText(value.WorkspaceRoot) != emptyText(s.settings.Settings.WorkspaceRoot)
	if workspaceChanged && (s.running() != nil || engineLocked(s.engineState.State) || s.engineState.State == "orphaned") {
		s.failure = core.Err("workspace_busy")
		return
	}
	if p.takeFailure() {
		s.failure = &core.ApiError{Code: "preview_save_failed", Message: "模拟保存失败，可再次点击保存。"}
		return
	}
	s.settings.Settings = value
	s.settings.Effective = core.EffectiveSettings{WorkspaceRoot: value.WorkspaceRoot, ResultsRoot: value.ResultsRoot, ModelsRoot: value.ModelsRoot, InferyardPath: value.InferyardPath}
	s.loadSettings(s.settings)
	s.ready.Workspace.Path, s.ready.Results.Path, s.ready.Models.Path = value.WorkspaceRoot, value.ResultsRoot, value.ModelsRoot
	if workspaceChanged {
		p.cancelEvents("environment")
		p.resetTask(s.model, s.engine)
		s.progress, s.task.stage = nil, ""
		s.ready.Workspace.Initialized = false
	}
	p.notice = "设置已保存到本次模拟会话。"
}

func (p *previewSession) startService(bind bool, candidate string) {
	s := p.state
	if s.busy || s.running() != nil {
		return
	}
	fail := p.takeFailure()
	s.busy = bind
	s.engineState = core.EngineState{State: "starting", Pid: 42, Endpoint: "http://127.0.0.1:48857", Candidate: candidate}
	p.after("service", 800*time.Millisecond, func() {
		if bind {
			s.busy = false
		}
		if fail {
			s.engineState.State, s.engineState.Error = "failed", core.Err("engine_start_failed")
			s.failure = s.engineState.Error
			return
		}
		s.engineState.State = "running"
		if bind && s.progress != nil {
			s.progress.NextAction = "run"
			for i := range s.progress.Bundles {
				b := &s.progress.Bundles[i]
				if b.Phase != "run_complete" {
					b.Phase, b.NextAction = "run_missing", "run"
				}
			}
		}
	})
}

func (p *previewSession) activateScene() {
	s := p.state
	if s.detail != nil {
		p.details[s.detailID] = s.detail
	}
	if task := s.running(); task != nil {
		task.Kind, task.RunId = "run", previewValue(task.TaskId)
		detail := previewDetailFixture(task.TaskId)
		detail.Task = *task
		detail.Progress = s.runProgress
		for i := int64(0); i < detail.Progress.Done; i++ {
			detail.Progress.Cases = append(detail.Progress.Cases, core.CaseProgress{CaseId: fmt.Sprintf("preview-case-%03d", i+1), State: "completed", Category: previewValue("模拟分类"), QualityState: previewValue("pass"), DurationMs: previewValue(1240.0)})
		}
		p.details[task.TaskId] = detail
		if s.detailID == task.TaskId {
			s.detail = detail
		}
		for i := range s.history.StandaloneRuns {
			if s.history.StandaloneRuns[i].RunId == task.TaskId {
				s.history.StandaloneRuns[i].Status, s.history.StandaloneRuns[i].Sealed = "partial", false
			}
		}
		s.task.bundles = []string{emptyText(task.Bundle)}
		s.busy = true
		p.tickRun(task.TaskId, 0, previewBenchmark{model: s.model, engine: s.engine, bundles: s.task.bundles}, []string{task.TaskId})
	}
	for _, task := range s.downloads {
		if task.State == "downloading" {
			p.tickDownload(task.TaskId, false, 0)
		}
	}
}

var _ previewMode = (*previewSession)(nil)
