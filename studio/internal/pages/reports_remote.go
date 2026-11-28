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
	t := tokens(c)
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
	ui.Column(c.Key("unified-report-" + report.ReportRef)).FillWidth().Gap(t.Density.Tight).Children(func() {
		reportCard(c, report.ReportSummary, modelHint, exclusivity, func() { s.openReport(report.ReportRef) })
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			if report.IsRemote {
				inlineBadge(c, trf(c, "nodes.reports.remote", "node", report.NodeName), stateInfo, badgeOptions{Size: sizeSM})
				verifiedKind := stateNeutral
				verifiedLabel := tr(c, "nodes.reports.unverified")
				if report.VerifiedFromRaw {
					verifiedKind, verifiedLabel = stateSuccess, tr(c, "nodes.reports.verified")
				}
				inlineBadge(c, verifiedLabel, verifiedKind, badgeOptions{Size: sizeSM})
				if !report.SourceOnline {
					ui.Text(c, tr(c, "nodes.reports.offline")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				}
				ui.Box(c).Grow(1)
				if outlineIconButton(c, reicon.Play, tr(c, "nodes.reports.resume")).Disabled(s.busy || !report.CanResume).Clicked() {
					s.runRemoteReport(report, "resume")
				}
				if outlineIconButton(c, reicon.RotateCw, tr(c, "nodes.reports.rerun")).Disabled(s.busy || !report.CanRerun).Clicked() {
					s.runRemoteReport(report, "rerun")
				}
			} else {
				inlineBadge(c, tr(c, "nodes.reports.local"), stateNeutral, badgeOptions{Size: sizeSM})
			}
		})
	})
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
		panel.Width(c.Theme().Space(140)).Children(func() {
			ui.Column(c).FillWidth().Gap(t.Density.CardGap).Children(func() {
				heading(c, tr(c, "nodes.compare.title"), 2)
				s.reportRefSelect(c, tr(c, "nodes.compare.left"), &s.nodes.compareLeftRef)
				s.reportRefSelect(c, tr(c, "nodes.compare.right"), &s.nodes.compareRightRef)
				ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
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
	choice := studioSelect(c, selected)
	current := *selected
	choice.Trigger.Label(label).Value(current).Children(func() { ui.Text(c, s.comparisonReportName(current)).SingleLine() })
	choice.Popup(func(ui.Element) {
		components.ScrollView(c, components.ScrollViewOptions{}).Label(label).MaxHeight(300).Children(func() {
			for _, report := range reports {
				ref := report.ReportRef
				name := s.comparisonReportName(ref)
				choice.Item(ref).Label(name).Children(func() { ui.Text(c, name).SingleLine() })
			}
		})
	})
	choice.Trigger.Changed()
}

func (s *viewState) comparisonReportName(reference string) string {
	for _, report := range s.reportSummaries() {
		if report.ReportRef == reference {
			return report.NodeName + " · " + reportHeadingFor(nil, report.ReportSummary)
		}
	}
	return reference
}
