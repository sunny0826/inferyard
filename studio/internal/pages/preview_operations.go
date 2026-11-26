//go:build ui_debug && !release

package pages

import (
	"time"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func (p *previewSession) dispatch(intent previewIntent) {
	intent.previewIntent()
	s := p.state
	s.failure, p.noticeKey = nil, ""
	switch op := intent.(type) {
	case previewManagedEngine:
		p.startService(false, op.candidate)
	case previewStandaloneStart:
		p.startPreviewStandalone(op)
	case previewSaveServicePreset:
		p.savePreviewServicePreset(op.preset)
	case previewDismissExitNotice:
		p.dismissPreviewExitNotice(op.id)
	case previewStopManagedService:
		p.stopPreviewManagedService(op.serviceID, op.after)
	case previewTestServiceEndpoint:
		p.testPreviewServiceEndpoint(op)
	case previewStopEngineThen:
		p.stopAllPreviewServices(op.after)
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
	case previewPolystrataSetup:
		p.startPolystrataSetup(op.req)
	case previewCancelDownload:
		p.cancelDownload(op.id)
	case previewRetryDownload:
		p.retryDownload(op.id)
	case previewBrowsePath:
		p.browse(op.key)
	case previewSettings:
		p.saveSettings(op.patch)
	case previewCleanWorkspaceArtifacts:
		p.cleanWorkspaceArtifacts()
	case previewInstallation:
		p.installation(op)
	case previewLocalBinding:
		p.bindLocal(op)
	case previewCreateReport:
		p.createReportLater(op.paths)
	case previewStartRun:
		s.resumeSource = nil
		p.beginBenchmark(previewBenchmark{model: s.model, engine: s.engine, bundles: []string{op.bundle}, resumePath: op.resumePath})
	case previewCancelRun:
		p.cancelRun(op.id)
	case previewUpdateCheck:
		p.checkUpdate()
	case previewUpdateStart:
		p.startUpdateDownload(true)
	case previewUpdateCancel:
		p.cancelUpdate()
	case previewUpdateApply:
		p.applyUpdate(op.confirmStopWorkloads)
	case previewUpdateRetry:
		p.retryUpdate()
	case previewStep:
		p.dispatchStep(op)
	default:
		p.showNotice()
	}
}

func (p *previewSession) retryDownload(id string) {
	for _, task := range p.state.downloads {
		if task.TaskId == id && (task.State == "failed" || task.State == "cancelled") {
			if p.state.downloadRetryActive(task) {
				return
			}
			if task.Kind == "polystrata" {
				if saved, ok := p.polystrataReqs[task.TaskId]; ok {
					p.startPolystrataSetup(saved)
					return
				}
				p.startPolystrataSetup(previewPolystrataRequestFromTask(task, p.state.defaultPolystrataSetupRequest()))
				return
			}
			p.startDownload(previewDownload{kind: task.Kind, source: task.SourceUrl})
			return
		}
	}
}

func (p *previewSession) createReportLater(paths []string) {
	s := p.state
	fail := p.takeFailure()
	s.busy = true
	p.after("report", 600*time.Millisecond, func() {
		s.busy = false
		if fail {
			s.failure = core.Err("report_unreadable")
			return
		}
		p.createReport(paths)
		p.noticeKey = "preview.notice.report_created"
	})
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
		p.stopPreviewEngine(nil)
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
			p.noticeKey = "preview.notice.device_refreshed"
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
				s.refreshFailure = l10n.TR("preview.error.refresh_failed")
				return
			}
			s.historyError, s.reportsError = nil, nil
			p.noticeKey = "preview.notice.page_refreshed"
		})
	default:
		p.showNotice()
	}
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
			detail.Progress.Cases = append(detail.Progress.Cases, previewSimulatedCase(emptyText(task.Bundle), i+1))
		}
		p.details[task.TaskId] = detail
		if s.detailID == task.TaskId {
			s.detail = detail
		}
		if run := p.historyRun(task.TaskId); run != nil {
			run.Status, run.Sealed, run.Completeness = "partial", false, nil
			p.scoreHistoryRun(task.TaskId)
		}
		s.task.bundles = []string{emptyText(task.Bundle)}
		s.busy = true
		p.tickRun(task.TaskId, 0, previewBenchmark{model: s.model, engine: s.engine, bundles: s.task.bundles}, []string{task.TaskId})
	}
	for _, task := range s.downloads {
		if task.State == "downloading" {
			if task.TaskId == "preview-polystrata-model-live" {
				p.tickPolystrataModelPreview(task.TaskId)
			} else if task.Kind == "polystrata" {
				p.tickPolystrataSetup(task.TaskId, false, 1)
			} else {
				p.tickDownload(task.TaskId, false, 0)
			}
		}
	}
	if s.updateState.State == core.UpdateDownloading {
		p.tickUpdateDownload(false, 2, false)
	}
}

var _ previewMode = (*previewSession)(nil)
