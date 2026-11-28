package pages

import (
	"context"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/icons/reicon"
	"inferyard/studio/internal/l10n"
)

func (s *viewState) unifiedReportCard(c *ui.Context, report core.UnifiedReportSummary) {
	modelHint := ""
	if !report.Unreadable {
		modelHint = s.modelIconHint(reportHeadingFor(c, report.ReportSummary))
		if len(report.RunSummaries) > 0 {
			modelHint += " " + s.modelIconHint(reportText(report.RunSummaries[0].Model))
		}
	}
	exclusivity := ""
	if !report.IsRemote {
		exclusivity = s.reportExclusivity(report.ReportId)
	}
	showSourceBadges := report.IsRemote || len(s.nodes.snapshots) > 1 || s.hasCachedRemoteReports()
	footerNotice := ""
	if report.IsRemote && !report.SourceOnline {
		footerNotice = tr(c, "nodes.reports.offline")
	}
	renderReportCard(
		c,
		report.ReportSummary,
		"unified-report-"+report.ReportRef,
		modelHint,
		exclusivity,
		func() {
			if !showSourceBadges {
				return
			}
			if report.IsRemote {
				inlineBadge(c, trf(c, "nodes.reports.remote", "node", report.NodeName), stateInfo, badgeOptions{Size: sizeSM})
				verifiedKind, verifiedLabel := stateNeutral, tr(c, "nodes.reports.unverified")
				if report.VerifiedFromRaw {
					verifiedKind, verifiedLabel = stateSuccess, tr(c, "nodes.reports.verified")
				}
				inlineBadge(c, verifiedLabel, verifiedKind, badgeOptions{Size: sizeSM})
			} else {
				inlineBadge(c, tr(c, "nodes.reports.local"), stateNeutral, badgeOptions{Size: sizeSM})
			}
		},
		func() {
			if !report.IsRemote {
				return
			}
			if outlineIconButton(c, reicon.Play, tr(c, "nodes.reports.resume")).Disabled(s.busy || !report.CanResume).Clicked() {
				s.runRemoteReport(report, "resume")
			}
			if outlineIconButton(c, reicon.RotateCw, tr(c, "nodes.reports.rerun")).Disabled(s.busy || !report.CanRerun).Clicked() {
				s.runRemoteReport(report, "rerun")
			}
		},
		footerNotice,
		func() { s.openReport(report.ReportRef) },
	)
}

func (s *viewState) runRemoteReport(report core.UnifiedReportSummary, action string) {
	nodeID, reportID, ok := remoteReportReferenceParts(report.ReportRef)
	if !ok || s.busy {
		return
	}
	run := func(ctx context.Context) ([]core.RunTask, *core.ApiError) {
		if action == "resume" {
			if s.nodes.resumeRemoteFn != nil {
				return s.nodes.resumeRemoteFn(ctx, nodeID, reportID)
			}
			return s.service.ResumeRemoteReport(ctx, nodeID, reportID)
		}
		if s.nodes.rerunRemoteFn != nil {
			return s.nodes.rerunRemoteFn(ctx, nodeID, reportID)
		}
		return s.service.RerunRemoteReport(ctx, nodeID, reportID)
	}
	s.workAs("remote-report-"+action, func() (string, *core.ApiError) {
		_, err := run(s.nodeContext())
		if err != nil {
			return "", err
		}
		if action == "resume" {
			return l10n.TR("nodes.reports.resumed"), nil
		}
		return l10n.TR("nodes.reports.rerunning"), nil
	})
}

func (s *viewState) openReportComparePicker() {
	reports := s.reportSummaries()
	if len(reports) < 2 {
		return
	}
	if s.nodes.compareLeftRef == "" {
		s.nodes.compareLeftRef = reports[0].ReportRef
	}
	if s.nodes.compareRightRef == "" || s.nodes.compareRightRef == s.nodes.compareLeftRef {
		s.nodes.compareRightRef = ""
		for _, report := range reports[1:] {
			if report.ReportRef != s.nodes.compareLeftRef {
				s.nodes.compareRightRef = report.ReportRef
				break
			}
		}
	}
	s.nodes.comparePickerOpen = true
}

func (s *viewState) reportComparePicker(c *ui.Context) {
	t := tokens(c)
	studioDialog(c.Key("report-compare-picker"), &s.nodes.comparePickerOpen, components.DialogOptions{Label: tr(c, "nodes.compare.title")}, func(panel ui.Element) {
		panel.Width(c.Theme().Space(140)).MaxWidthPercent(92).Children(func() {
			ui.Column(c).FillWidth().Gap(t.Density.CardGap).Children(func() {
				heading(c, tr(c, "nodes.compare.title"), 2)
				s.reportRefSelect(c, tr(c, "nodes.compare.left"), &s.nodes.compareLeftRef)
				s.reportRefSelect(c, tr(c, "nodes.compare.right"), &s.nodes.compareRightRef)
				ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
				ui.Row(c).FillWidth().Justify(ui.End).Gap(t.Density.Row).Children(func() {
					if button(c, tr(c, "common.cancel")).Clicked() {
						s.nodes.comparePickerOpen = false
					}
					if primaryButton(c, tr(c, "nodes.compare.run")).Disabled(s.nodes.compareLeftRef == "" || s.nodes.compareRightRef == "" || s.nodes.compareLeftRef == s.nodes.compareRightRef).Clicked() {
						s.loadReportComparison()
					}
				})
			})
		})
	})
}

func (s *viewState) reportRefSelect(c *ui.Context, label string, selected *string) {
	reports := s.reportSummaries()
	ui.Field(c, label, func() {
		choice := studioSelect(c, selected)
		current := *selected
		currentLabel := s.comparisonReportNameFor(c, current)
		choice.Trigger.FillWidth().MinWidth(0).Label(label).Value(currentLabel).Children(func() { ui.Text(c, currentLabel).SingleLine() })
		choice.Popup(func(ui.Element) {
			components.ScrollView(c, components.ScrollViewOptions{}).Label(label).MaxHeight(300).Children(func() {
				for _, report := range reports {
					ref := report.ReportRef
					name := s.comparisonReportNameFor(c, ref)
					choice.Item(ref).Label(name).Children(func() { ui.Text(c, name).SingleLine() })
				}
			})
		})
		choice.Trigger.Changed()
	})
}

func (s *viewState) comparisonReportName(reference string) string {
	return s.comparisonReportNameFor(nil, reference)
}

func (s *viewState) comparisonReportNameFor(c *ui.Context, reference string) string {
	for _, report := range s.reportSummaries() {
		if report.ReportRef == reference {
			nodeName := report.NodeName
			if !report.IsRemote && nodeName == core.LocalNodeID {
				nodeName = tr(c, "nodes.reports.local")
			}
			return nodeName + " · " + reportHeadingFor(c, report.ReportSummary)
		}
	}
	return reference
}
