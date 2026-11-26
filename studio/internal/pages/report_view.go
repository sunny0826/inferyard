package pages

import (
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"inferyard/studio/internal/core"
)

type reportViewState struct {
	reference            string
	index                *core.ReportIndex
	failure              *core.ApiError
	loading              bool
	loadSeq              uint64
	run                  int
	requestsPage         int
	svgIndex             int
	svgStage             svgStageView
	svgLoadedKey         string
	svgStageErr          bool
	categories, requests pageTableState
}

func (s *viewState) readReport(reference string) (*core.ReportIndex, *core.ApiError) {
	if s.preview != nil {
		return s.preview.report(reference)
	}
	if s.service == nil {
		return nil, core.Err("not_found")
	}
	return s.service.ReportIndex(reference)
}

func (s *viewState) hasCachedReportExclusivity(reference string) bool {
	if s.reportExclusivities == nil || reference == "" {
		return false
	}
	if _, ok := s.reportExclusivities[reference]; ok {
		return true
	}
	base := filepath.Base(reference)
	if base != "" && base != "." && base != "/" {
		if _, ok := s.reportExclusivities[base]; ok {
			return true
		}
	}
	return false
}

func (s *viewState) storeReportExclusivity(reference, status string) {
	if reference == "" || status == "" {
		return
	}
	if s.reportExclusivities == nil {
		s.reportExclusivities = map[string]string{}
	}
	s.reportExclusivities[reference] = status
	if base := filepath.Base(reference); base != "" && base != "." && base != "/" {
		s.reportExclusivities[base] = status
	}
}

func (s *viewState) openReport(reference string) {
	if s.speed != nil {
		s.speed.opened = nil
	}
	s.historyDetailPath = ""
	s.closeOpenedReport()
	s.selected = "reports"

	if s.preview != nil || (s.win == nil && s.updateWindow == nil) {
		index, e := s.readReport(reference)
		if s.preview == nil && s.service != nil && reference != "" && !s.hasCachedReportExclusivity(reference) {
			s.storeReportExclusivity(reference, s.service.ReportExclusivity(reference))
		}
		s.openedReport = &reportViewState{reference: reference, index: index, failure: e}
		return
	}

	if s.service != nil {
		if cached, ok := s.service.CachedReportIndex(reference); ok {
			s.openedReport = &reportViewState{reference: reference, index: cached}
			if reference != "" && !s.hasCachedReportExclusivity(reference) {
				svc := s.service
				go func() {
					excl := svc.ReportExclusivity(reference)
					s.updateView(func() { s.storeReportExclusivity(reference, excl) })
				}()
			}
			return
		}
	}

	s.reportLoadSeq++
	seq := s.reportLoadSeq
	s.openedReport = &reportViewState{reference: reference, loading: true, loadSeq: seq}
	needExcl := s.service != nil && reference != "" && !s.hasCachedReportExclusivity(reference)
	svc := s.service
	go func() {
		index, e := s.readReport(reference)
		var excl string
		if needExcl && svc != nil {
			excl = svc.ReportExclusivity(reference)
		}
		s.updateView(func() {
			if excl != "" {
				s.storeReportExclusivity(reference, excl)
			}
			if s.openedReport == nil || s.openedReport.loadSeq != seq {
				return
			}
			s.openedReport.loading = false
			s.openedReport.index = index
			s.openedReport.failure = e
		})
	}()
}

func (s *viewState) reportView(c *ui.Context) {
	report := s.openedReport
	id := displayName(report.reference)
	title := trf(c, "reports.detail.title_with_id", "id", id)
	if !report.loading && report.failure == nil && len(report.index.Runs) > 0 {
		title = reportModel(report.index.Runs[report.run])
	}
	s.subpageHeader(c, title, tr(c, "reports.detail.back"), func() {
		s.closeOpenedReport()
		s.reportsTab = reportsTabList
	})
	if s.openedReport == nil {
		return
	}
	if report.loading {
		host := ui.Column(c.Key("report-loading-" + report.reference)).FillWidth().Gap(tokens(c).Density.Section)
		show := delayedVisible(c, host, "report-load", true)
		host.Children(func() {
			if show {
				pageLoadingSkeleton(c)
			}
		})
		return
	}
	if report.failure != nil {
		errorRecoveryBanner(c, report.failure, func() {
			button(c, tr(c, "reports.detail.retry_read")).OnClick(func() { s.openReport(report.reference) })
		})
		return
	}
	if len(report.index.Runs) == 0 {
		emptyGuideCard(c, emptyGuideOptions{
			Icon:    lucide.FileText,
			Title:   tr(c, "reports.detail.empty_runs"),
			Surface: emptyCard,
		})
		return
	}
	run := report.index.Runs[report.run]
	ui.Text(c, id+" · "+reportText(run.Facets.BundleLabel)).TextColor(c.Theme().TextMuted)
	if len(report.index.Runs) > 1 {
		report.runPicker(c)
		run = report.index.Runs[report.run]
	}
	ui.Row(c).Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.Row).Children(func() {
		exclusivityBadge(c, s.reportExclusivity(report.reference))
		if run.Diagnostic {
			statusBadge(c, tr(c, "reports.detail.diagnostic_badge"), stateColor(c, stateWarning))
		}
	})
	reportMetrics(c, run)
	s.reportSVGSection(c, report, run)
	reportSummaryGrid(c,
		func(c *ui.Context) { reportThroughput(c, run) },
		func(c *ui.Context) { reportOutputIntervals(c, run) },
	)
	report.categoryView(c, run)
	reportSummaryGrid(c,
		func(c *ui.Context) { reportProfileView(c, run) },
		func(c *ui.Context) { reportSourceView(c, run) },
	)
	report.requestView(c, run)
}

func reportSummaryGrid(c *ui.Context, cards ...func(*ui.Context)) {
	if compactLayout(c) {
		for _, build := range cards {
			build(c)
		}
		return
	}
	ui.Grid(c).Columns(2).Gap(tokens(c).Density.Stack).Children(func() {
		for _, build := range cards {
			build(c)
		}
	})
}

func reportSourceView(c *ui.Context, run core.ReportRun) {
	card(c, tr(c, "reports.detail.source.title"), func() {
		deviceRow(c, tr(c, "reports.detail.source.run"), reportText(run.Source.RunID))
		deviceRow(c, tr(c, "reports.detail.source.stop_reason"), stopReasonLabelFor(c, run.Summary.StopReason))
	})
}

func stopReasonLabel(raw string) string {
	return stopReasonLabelFor(nil, raw)
}

func stopReasonLabelFor(c *ui.Context, raw string) string {
	switch raw {
	case "":
		return "—"
	case "plan_finished", "completed":
		return tr(c, "reports.stop_reason.completed")
	case "user_cancelled", "cancelled":
		return tr(c, "reports.stop_reason.user_cancelled")
	case "run_in_progress":
		return tr(c, "reports.stop_reason.run_in_progress")
	case "interrupted_checkpoint":
		return tr(c, "reports.stop_reason.interrupted_checkpoint")
	case "diagnostic_run_not_formal":
		return tr(c, "reports.stop_reason.diagnostic_run_not_formal")
	case "check_only_no_formal_cases":
		return tr(c, "reports.stop_reason.check_only_no_formal_cases")
	case "unscorable_case":
		return tr(c, "reports.stop_reason.unscorable_case")
	case "service_stop_unconfirmed":
		return tr(c, "reports.stop_reason.service_stop_unconfirmed")
	default:
		return raw
	}
}

func (r *reportViewState) runPicker(c *ui.Context) {
	pickerLabel := tr(c, "reports.detail.select_run")
	ui.Field(c, pickerLabel, func() {
		selectRun := studioSelect(c, &r.run)
		selectRun.Trigger.Label(pickerLabel).Value(reportRunLabel(r.index.Runs[r.run])).Children(func() {
			ui.Text(c, reportRunLabel(r.index.Runs[r.run])).SingleLine()
		})
		selectRun.Popup(func(panel ui.Element) {
			for i, run := range r.index.Runs {
				selectRun.Item(i).Label(reportRunLabel(run)).Children(func() {
					ui.Text(c, reportRunLabel(run)).SingleLine()
				})
			}
		})
		if selectRun.Trigger.Changed() {
			r.categories, r.requests = pageTableState{}, pageTableState{}
			r.requestsPage = 0
			r.svgIndex = 0
			r.releaseSVGStage()
		}
	})
}

func reportRunLabel(run core.ReportRun) string {
	return reportText(run.Facets.BundleLabel) + " · " + reportText(run.Source.RunID)
}

func reportModel(run core.ReportRun) string {
	if run.Profile.Title != nil && *run.Profile.Title != "" {
		return displayValue(*run.Profile.Title)
	}
	return reportText(run.Model)
}

func reportProfileView(c *ui.Context, run core.ReportRun) {
	card(c, tr(c, "reports.detail.profile.title"), func() {
		deviceRow(c, tr(c, "reports.detail.profile.model"), reportModel(run))
		engine := displayName(emptyText(run.Profile.Engine.BinaryPath))
		if engine == "" {
			engine = run.Profile.Engine.Adapter
		}
		backend := strings.ToUpper(run.Profile.Engine.Backend)
		if backend == "METAL" {
			backend = "Metal"
		}
		deviceRow(c, tr(c, "reports.detail.profile.engine"), trf(c, "reports.detail.profile.engine_value", "engine", reportText(engine), "backend", reportText(backend), "threads", reportNumber(run.Threads)))
		machine := run.Profile.Machine
		deviceRow(c, tr(c, "reports.detail.profile.machine"), trf(c, "reports.detail.profile.machine_value", "cpu", reportText(emptyText(machine.CPU)), "cores", reportNumber(machine.LogicalCPUs), "memory", reportScaled(machine.MemoryTotalBytes, 1<<30, "GiB")))
		deviceRow(c, tr(c, "reports.detail.profile.bundle"), reportText(run.Facets.BundleLabel))
	})
}
