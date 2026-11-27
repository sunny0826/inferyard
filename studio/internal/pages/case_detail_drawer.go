package pages

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

type caseDetailItem struct {
	Key             string
	CaseID          string
	Category        string
	StateLabel      string
	StateKind       stateKind
	QualityText     string
	QualityKind     stateKind
	HasQualityBadge bool
	DurationText    string
	ErrorCategory   string
	Prompt          string
	Response        string
	IsReport        bool
}

func (s *viewState) copyCaseText(c *ui.Context, content string) {
	if s.preview != nil {
		s.enqueueStatusToast(tr(c, "detail.case_drawer.copied"))
		return
	}
	c.WriteClipboardThen(content, func(err error) {
		if err != nil {
			s.enqueueStatusToast(l10n.TR("detail.case_drawer.copy_failed"))
			return
		}
		s.enqueueStatusToast(l10n.TR("detail.case_drawer.copied"))
	})
}

func (s *viewState) caseDetailDrawer(c *ui.Context, drawerKey string, open *bool, item caseDetailItem) {
	c.Key(drawerKey)
	title := tr(c, "detail.case_drawer.title")
	drawer(c, open, drawerOptions{
		Label: title,
		Title: title,
		Width: c.Theme().Space(148),
	}, func(panel ui.Element) {
		t := tokens(c)
		ui.Scroll(c.Key("case-drawer-scroll-" + item.Key)).FillWidth().Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Gap(t.Density.CardGap).Children(func() {
				applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Row).Padding(t.Card.Padding), materialSunken).Children(func() {
					ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
						ui.Text(c, item.CaseID).Bold().FontSize(t.Type.CardTitle).FontFeatures("tnum").TextColor(t.Color.FgPrimary).Selectable()
						if item.StateLabel != "" {
							inlineBadge(c, item.StateLabel, item.StateKind, badgeOptions{Size: sizeSM, Style: badgeDot})
						}
						if item.HasQualityBadge && item.QualityText != "" {
							inlineBadge(c, item.QualityText, item.QualityKind, badgeOptions{Size: sizeSM, Style: badgeSoft})
						}
					})
					metaParts := []string{}
					if item.Category != "" && item.Category != "—" {
						metaParts = append(metaParts, item.Category)
					}
					if item.DurationText != "" && item.DurationText != "—" {
						metaParts = append(metaParts, item.DurationText)
					}
					if !item.HasQualityBadge && item.QualityText != "" && item.QualityText != "—" {
						metaParts = append(metaParts, item.QualityText)
					}
					if len(metaParts) > 0 || item.ErrorCategory != "" {
						ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
							if len(metaParts) > 0 {
								ui.Text(c, strings.Join(metaParts, " · ")).
									FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary)
							}
							if item.ErrorCategory != "" {
								inlineBadge(c, item.ErrorCategory, stateDanger, badgeOptions{Size: sizeSM, Style: badgeSoft})
							}
						})
					}
				})

				hasPrompt := strings.TrimSpace(item.Prompt) != ""
				hasResponse := strings.TrimSpace(item.Response) != ""

				if !hasPrompt && !hasResponse {
					msgKey := "detail.case_drawer.prompt_missing"
					if item.IsReport {
						msgKey = "detail.case_drawer.prompt_unavailable"
					}
					infoBanner(c, tr(c, msgKey))
					return
				}

				if hasPrompt {
					s.caseTextSection(c, lucide.FileText, tr(c, "detail.case_drawer.prompt_title"), tr(c, "detail.case_drawer.copy_prompt"), item.Prompt)
				} else {
					msgKey := "detail.case_drawer.prompt_missing"
					if item.IsReport {
						msgKey = "detail.case_drawer.prompt_unavailable"
					}
					infoBanner(c, tr(c, msgKey))
				}

				if hasResponse {
					s.caseTextSection(c, lucide.CircleCheck, tr(c, "detail.case_drawer.response_title"), tr(c, "detail.case_drawer.copy_response"), item.Response)
				}
			})
		})
	})
}

func (s *viewState) caseTextSection(c *ui.Context, icon func() *ui.SVG, headingText, copyLabel, body string) {
	t := tokens(c)
	applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Row).Padding(t.Card.Padding), materialCard).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			components.Icon(c, icon).Size(14, 14).TextColor(t.Color.FgSecondary).Shrink(0)
			ui.Text(c, headingText).Bold().FontSize(t.Type.Section).TextColor(t.Color.FgPrimary).Grow(1)
			if outlineIconButton(c, lucide.Copy, copyLabel).Clicked() {
				s.copyCaseText(c, body)
			}
		})
		applyMaterial(c, ui.Column(c).FillWidth().Padding(t.Density.Stack), materialSunken).Children(func() {
			ui.Text(c, body).FontSize(t.Type.Body).TextColor(t.Color.FgPrimary).Selectable()
		})
	})
}

func detailCaseItem(c *ui.Context, item core.CaseProgress) caseDetailItem {
	qText := caseQuality(item)
	qKind := stateNeutral
	hasQBadge := false
	switch emptyText(item.QualityState) {
	case "pass", "passed":
		qKind, hasQBadge = stateSuccess, true
	case "fail", "failed":
		qKind, hasQBadge = stateDanger, true
	case "unscorable":
		qKind, hasQBadge = stateWarning, true
	}
	errCat := ""
	if item.ErrorCategory != nil && *item.ErrorCategory != "" {
		errCat = *item.ErrorCategory
	}
	cat := ""
	if item.Category != nil && *item.Category != "" {
		cat = *item.Category
	}
	dur := ""
	if item.DurationMs != nil {
		dur = formatDurationMs(item.DurationMs)
	}
	return caseDetailItem{
		Key:             "detail-" + item.CaseId,
		CaseID:          item.CaseId,
		Category:        cat,
		StateLabel:      stateLabel(item.State),
		StateKind:       caseStateKind(item.State),
		QualityText:     qText,
		QualityKind:     qKind,
		HasQualityBadge: hasQBadge,
		DurationText:    dur,
		ErrorCategory:   errCat,
		Prompt:          emptyText(item.Prompt),
		Response:        emptyText(item.Response),
	}
}

func reportCaseItem(c *ui.Context, runID string, idx int, req core.ReportRequest) caseDetailItem {
	qText := reportQualityFor(c, req.Quality)
	qKind, hasQBadge := reportQualityBadgeKind(req.Quality)
	dur := ""
	if req.LatencyMS != nil {
		dur = reportScaled(req.LatencyMS, 1000, "s")
	}
	return caseDetailItem{
		Key:             "report-" + runID + "-" + req.CaseID,
		CaseID:          reportText(req.CaseID),
		Category:        reportText(req.CategoryLabel),
		StateLabel:      reportText(stateLabel(req.State)),
		StateKind:       caseStateKind(req.State),
		QualityText:     qText,
		QualityKind:     qKind,
		HasQualityBadge: hasQBadge,
		DurationText:    dur,
		Prompt:          emptyText(req.Prompt),
		Response:        emptyText(req.Response),
		IsReport:        true,
	}
}

func (s *viewState) detailCaseDrawer(c *ui.Context, d *core.RunTaskDetail) {
	open := s.detailCaseID != "" && d != nil && d.Progress != nil
	var selected core.CaseProgress
	if open {
		found := false
		for _, item := range d.Progress.Cases {
			if item.CaseId == s.detailCaseID {
				selected = item
				found = true
				break
			}
		}
		if !found {
			open = false
			s.detailCaseID = ""
		}
	}
	s.caseDetailDrawer(c, "detail-case-drawer", &open, detailCaseItem(c, selected))
	if !open {
		s.detailCaseID = ""
	}
}

func (s *viewState) reportCaseDrawer(c *ui.Context, report *reportViewState, run core.ReportRun) {
	open := report != nil && report.detailCaseOpen && report.detailCaseIndex >= 0 && report.detailCaseIndex < len(run.Requests)
	var item caseDetailItem
	if open {
		item = reportCaseItem(c, run.Source.RunID, report.detailCaseIndex, run.Requests[report.detailCaseIndex])
	}
	s.caseDetailDrawer(c, "report-case-drawer", &open, item)
	if !open && report != nil {
		report.detailCaseOpen = false
		report.detailCaseIndex = -1
	}
}
