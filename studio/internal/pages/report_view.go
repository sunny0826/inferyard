package pages

import (
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/icons/reicon"
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
	detailCaseOpen       bool
	detailCaseIndex      int
}

func (s *viewState) readReport(reference string) (*core.ReportIndex, *core.ApiError) {
	if s.preview != nil {
		return s.preview.report(reference)
	}
	if s.service == nil {
		return nil, core.Err("not_found")
	}
	if nodeID, reportID, ok := remoteReportReferenceParts(reference); ok {
		return s.service.RemoteReportIndex(nodeID, reportID)
	}
	return s.service.ReportIndex(reference)
}

func (s *viewState) hasCachedReportExclusivity(reference string) bool {
	if _, _, remote := remoteReportReferenceParts(reference); remote {
		return false
	}
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
	if _, _, remote := remoteReportReferenceParts(reference); remote {
		return
	}
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
	_, _, remoteRef := remoteReportReferenceParts(reference)

	if s.preview != nil || (s.win == nil && s.updateWindow == nil) {
		index, e := s.readReport(reference)
		if s.preview == nil && s.service != nil && !remoteRef && reference != "" && !s.hasCachedReportExclusivity(reference) {
			s.storeReportExclusivity(reference, s.service.ReportExclusivity(reference))
		}
		s.openedReport = &reportViewState{reference: reference, index: index, failure: e}
		return
	}

	if s.service != nil && !remoteRef {
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
	needExcl := s.service != nil && !remoteRef && reference != "" && !s.hasCachedReportExclusivity(reference)
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
	s.subpageHeader(c, title)
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
			refreshIconButton(c, tr(c, "reports.detail.retry_read"), false).OnClick(func() { s.openReport(report.reference) })
		})
		return
	}
	if len(report.index.Runs) == 0 {
		emptyGuideCard(c, emptyGuideOptions{
			Icon:    reicon.FileText,
			Title:   tr(c, "reports.detail.empty_runs"),
			Surface: emptyCard,
		})
		return
	}
	run := report.index.Runs[report.run]
	t := tokens(c)
	applyMaterial(c, ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Stack).
		Padding(t.Density.Row, t.Density.Stack), materialSunken).Children(func() {
		modelIconBox(c, s.modelIconHint(reportModel(run)), 28, t.Radius.SM)
		ui.Row(c).Grow(1).MinWidth(200).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			ui.Text(c, id+" · "+reportText(run.Facets.BundleLabel)).
				FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary).SingleLine().Selectable()
			exclusivityBadge(c, s.reportExclusivity(report.reference))
			if run.Diagnostic {
				inlineBadge(c, tr(c, "reports.detail.diagnostic_badge"), stateWarning, badgeOptions{Size: sizeSM, Style: badgeSoft})
			}
		})
		if len(report.index.Runs) > 1 {
			report.runPicker(c)
		}
	})
	if len(report.index.Runs) > 1 {
		run = report.index.Runs[report.run]
	}
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
	s.reportCaseDrawer(c, report, run)
}

func reportSectionCard(c *ui.Context, icon func() *ui.SVG, title string, trailing func(), body func()) {
	t := tokens(c)
	applyMaterial(c, ui.Column(c).FillWidth().MinWidth(0).Gap(t.Density.CardGap).Padding(t.Card.Padding), materialCard).Children(func() {
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			if icon != nil {
				components.Icon(c, icon).Size(15, 15).TextColor(t.Color.FgSecondary).Shrink(0)
			}
			heading(c, title, 2)
			if trailing != nil {
				ui.Box(c).Grow(1)
				trailing()
			}
		})
		if body != nil {
			body()
		}
	})
}

func reportKVRow(c *ui.Context, label, value string, tabular, muted, emphasize bool) {
	t := tokens(c)
	ariaLabel := trf(c, "device.row.aria_label", "label", label, "value", value)
	valColor := t.Color.FgPrimary
	if muted || value == "" || value == "—" {
		valColor = t.Color.FgTertiary
	}
	renderVal := func() {
		el := ui.Text(c, value).TextColor(valColor).Selectable().Label(ariaLabel)
		if tabular {
			el.FontFeatures("tnum")
		}
		if emphasize && !muted && value != "—" {
			el.Bold()
		}
	}
	if compactLayout(c) {
		ui.Column(c).FillWidth().Gap(2).Padding(t.Density.Tight, 0).Children(func() {
			ui.Text(c, label).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			renderVal()
		})
		return
	}
	ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(t.Density.Stack).Padding(t.Density.Tight+1, 0).Children(func() {
		ui.Box(c).Width(c.Theme().Rem(9)).Shrink(0).Padding(1, 0, 0, 0).Children(func() {
			ui.Text(c, label).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		})
		ui.Box(c).Grow(1).MinWidth(0).Children(renderVal)
	})
}

func reportKVDivider(c *ui.Context) {
	ui.Box(c).FillWidth().Height(1).Background(tokens(c).Color.BorderSubtle)
}

func stopReasonStateKind(raw string) stateKind {
	switch raw {
	case "plan_finished", "completed":
		return stateSuccess
	case "run_in_progress":
		return stateActive
	case "user_cancelled", "cancelled", "interrupted_checkpoint", "diagnostic_run_not_formal", "unscorable_case", "service_stop_unconfirmed":
		return stateWarning
	default:
		return stateNeutral
	}
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
	t := tokens(c)
	reportSectionCard(c, reicon.Server, tr(c, "reports.detail.source.title"), nil, func() {
		ui.Column(c).FillWidth().Gap(0).Children(func() {
			reportKVRow(c, tr(c, "reports.detail.source.run"), reportText(run.Source.RunID), true, run.Source.RunID == "", false)
			reportKVDivider(c)
			label := tr(c, "reports.detail.source.stop_reason")
			val := stopReasonLabelFor(c, run.Summary.StopReason)
			if compactLayout(c) {
				ui.Column(c).FillWidth().Gap(2).Padding(t.Density.Tight, 0).Children(func() {
					ui.Text(c, label).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
					if run.Summary.StopReason == "" {
						ui.Text(c, val).TextColor(t.Color.FgTertiary)
					} else {
						inlineBadge(c, val, stopReasonStateKind(run.Summary.StopReason), badgeOptions{Size: sizeSM, Style: badgeDot})
					}
				})
				return
			}
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Stack).Padding(t.Density.Tight+1, 0).Children(func() {
				ui.Box(c).Width(c.Theme().Rem(9)).Shrink(0).Children(func() {
					ui.Text(c, label).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				})
				if run.Summary.StopReason == "" {
					ui.Text(c, val).TextColor(t.Color.FgTertiary)
				} else {
					inlineBadge(c, val, stopReasonStateKind(run.Summary.StopReason), badgeOptions{Size: sizeSM, Style: badgeDot})
				}
			})
		})
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
	t := tokens(c)
	pickerLabel := tr(c, "reports.detail.select_run")
	ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Row).Shrink(0).Children(func() {
		ui.Text(c, pickerLabel).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).SingleLine().Shrink(0)
		selectRun := studioSelect(c, &r.run)
		selectRun.Trigger.Label(pickerLabel).Value(reportRunLabel(r.index.Runs[r.run])).Children(func() {
			ui.Text(c, reportRunLabel(r.index.Runs[r.run])).FontSize(t.Type.Caption).SingleLine()
		})
		selectRun.Popup(func(panel ui.Element) {
			components.ScrollView(c, components.ScrollViewOptions{}).Label(pickerLabel).MaxHeight(260).Children(func() {
				for i, run := range r.index.Runs {
					selectRun.Item(i).Label(reportRunLabel(run)).Children(func() {
						ui.Text(c, reportRunLabel(run)).SingleLine()
					})
				}
			})
		})
		if selectRun.Trigger.Changed() {
			r.categories, r.requests = pageTableState{}, pageTableState{}
			r.requestsPage = 0
			r.detailCaseOpen, r.detailCaseIndex = false, -1
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
	reportSectionCard(c, reicon.Cpu, tr(c, "reports.detail.profile.title"), nil, func() {
		ui.Column(c).FillWidth().Gap(0).Children(func() {
			reportKVRow(c, tr(c, "reports.detail.profile.model"), reportModel(run), false, false, true)
			reportKVDivider(c)
			engine := displayName(emptyText(run.Profile.Engine.BinaryPath))
			if engine == "" {
				engine = run.Profile.Engine.Adapter
			}
			backend := strings.ToUpper(run.Profile.Engine.Backend)
			if backend == "METAL" {
				backend = "Metal"
			}
			reportKVRow(c, tr(c, "reports.detail.profile.engine"), trf(c, "reports.detail.profile.engine_value", "engine", reportText(engine), "backend", reportText(backend), "threads", reportNumber(run.Threads)), true, false, false)
			reportKVDivider(c)
			machine := run.Profile.Machine
			reportKVRow(c, tr(c, "reports.detail.profile.machine"), trf(c, "reports.detail.profile.machine_value", "cpu", reportText(emptyText(machine.CPU)), "cores", reportNumber(machine.LogicalCPUs), "memory", reportScaled(machine.MemoryTotalBytes, 1<<30, "GiB")), true, false, false)
			reportKVDivider(c)
			reportKVRow(c, tr(c, "reports.detail.profile.bundle"), reportText(run.Facets.BundleLabel), false, run.Facets.BundleLabel == "", false)
		})
	})
}
