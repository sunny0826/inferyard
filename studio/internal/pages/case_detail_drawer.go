package pages

import (
	"fmt"
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
	Index           int
	Total           int
	OnPrev          func()
	OnNext          func()
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

// drawerGhostIconButton renders a compact 28x28 frameless icon button aligned with
// the drawer's close button so auxiliary actions stay quiet next to section labels.
func drawerGhostIconButton(c *ui.Context, icon func() *ui.SVG, label string, disabled bool) ui.Element {
	t := tokens(c)
	b := components.Button(c, "", components.ButtonGhost).
		Size(28, 28).Padding(0).Radius(t.Radius.Control).
		Label(label).Disabled(disabled)
	iconColor := t.Color.FgSecondary
	if disabled {
		iconColor = t.Color.FgTertiary
	} else if b.Hovered() {
		iconColor = t.Color.FgPrimary
	}
	b.Children(func() {
		components.Icon(c, icon).Size(14, 14).TextColor(iconColor).Role(ui.RoleNone)
	})
	if !disabled {
		components.Tooltip(c, b, label)
	}
	return b
}

func (s *viewState) caseDetailDrawer(c *ui.Context, drawerKey string, open *bool, item caseDetailItem) {
	c.Key(drawerKey)
	ariaTitle := tr(c, "detail.case_drawer.title")
	headerTitle := item.CaseID
	if headerTitle == "" {
		headerTitle = ariaTitle
	}
	drawer(c, open, drawerOptions{
		Label: ariaTitle,
		Title: headerTitle,
		Width: c.Theme().Space(148),
	}, func(panel ui.Element) {
		t := tokens(c)

		// Pinned metadata & prev/next case navigator directly under the drawer header.
		ui.Column(c).FillWidth().Gap(t.Density.Stack).Children(func() {
			ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				ui.Row(c).Grow(1).MinWidth(180).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
					if item.StateLabel != "" {
						inlineBadge(c, item.StateLabel, item.StateKind, badgeOptions{Size: sizeSM, Style: badgeDot})
					}
					if item.HasQualityBadge && item.QualityText != "" {
						inlineBadge(c, item.QualityText, item.QualityKind, badgeOptions{Size: sizeSM, Style: badgeSoft})
					} else if item.QualityText != "" && item.QualityText != "—" {
						inlineBadge(c, item.QualityText, stateNeutral, badgeOptions{Size: sizeSM, Style: badgeSoft})
					}
					if item.Category != "" && item.Category != "—" {
						inlineBadge(c, item.Category, stateNeutral, badgeOptions{Size: sizeSM, Style: badgeOutline})
					}
					if item.DurationText != "" && item.DurationText != "—" {
						ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
							components.Icon(c, lucide.Clock).Size(12, 12).TextColor(t.Color.FgTertiary).Role(ui.RoleNone)
							ui.Text(c, item.DurationText).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary).SingleLine()
						})
					}
					if item.ErrorCategory != "" {
						inlineBadge(c, item.ErrorCategory, stateDanger, badgeOptions{Size: sizeSM, Style: badgeSoft})
					}
				})
				if item.Total > 1 {
					ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
						ui.Text(c, fmt.Sprintf("%d / %d", item.Index+1, item.Total)).
							FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgTertiary).SingleLine()
						if drawerGhostIconButton(c, lucide.ArrowLeft, tr(c, "detail.case_drawer.prev_case"), item.OnPrev == nil).Clicked() && item.OnPrev != nil {
							item.OnPrev()
						}
						if drawerGhostIconButton(c, lucide.ArrowRight, tr(c, "detail.case_drawer.next_case"), item.OnNext == nil).Clicked() && item.OnNext != nil {
							item.OnNext()
						}
					})
				}
			})
			ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
		})

		ui.Scroll(c.Key("case-drawer-scroll-" + item.Key)).FillWidth().Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Gap(t.Density.Section).Padding(t.Density.Tight, 0, t.Density.Stack, 0).Children(func() {
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
					s.casePromptBlock(c, item.Prompt)
				} else {
					msgKey := "detail.case_drawer.prompt_missing"
					if item.IsReport {
						msgKey = "detail.case_drawer.prompt_unavailable"
					}
					infoBanner(c, tr(c, msgKey))
				}

				if hasResponse {
					accentKind := stateActive
					if item.HasQualityBadge {
						accentKind = item.QualityKind
					}
					s.caseResponseBlock(c, item.Response, accentKind)
				}
			})
		})
	})
}

func (s *viewState) caseSectionBar(c *ui.Context, icon func() *ui.SVG, iconColor ui.Color, label, copyLabel, body string) {
	t := tokens(c)
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Tight + 2).Children(func() {
		components.Icon(c, icon).Size(13, 13).TextColor(iconColor).Shrink(0).Role(ui.RoleNone)
		ui.Text(c, label).Bold().FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).Grow(1).SingleLine()
		if drawerGhostIconButton(c, lucide.Copy, copyLabel, false).Clicked() {
			s.copyCaseText(c, body)
		}
	})
}

func (s *viewState) casePromptBlock(c *ui.Context, body string) {
	t := tokens(c)
	ui.Column(c).FillWidth().Gap(t.Density.Tight + 2).Children(func() {
		s.caseSectionBar(c, lucide.FileText, t.Color.FgTertiary, tr(c, "detail.case_drawer.prompt_title"), tr(c, "detail.case_drawer.copy_prompt"), body)
		ui.Column(c).FillWidth().
			Background(t.Color.Sunken).
			Border(1, t.Color.BorderSubtle).
			Radius(t.Radius.MD).
			Padding(t.Density.Stack+2, t.Density.Section).Children(func() {
			ui.Text(c, body).FontSize(t.Type.Body).TextColor(t.Color.FgPrimary).Selectable()
		})
	})
}

func (s *viewState) caseResponseBlock(c *ui.Context, body string, accentKind stateKind) {
	t := tokens(c)
	scale := stateScale(c, accentKind)
	accent := semanticAccentColor(c, scale)
	ui.Column(c).FillWidth().Gap(t.Density.Tight + 2).Children(func() {
		s.caseSectionBar(c, lucide.CircleCheck, accent, tr(c, "detail.case_drawer.response_title"), tr(c, "detail.case_drawer.copy_response"), body)
		applyShadow(
			ui.Row(c).FillWidth().
				Background(t.Color.Surface).
				Border(1, t.Color.BorderDefault).
				Radius(t.Radius.MD).
				Clip(),
			t.Elevation.XS,
		).Children(func() {
			ui.Box(c).Width(3).FillHeight().Shrink(0).Background(scale.Solid)
			ui.Column(c).Grow(1).MinWidth(0).Padding(t.Density.Stack+2, t.Density.Section).Children(func() {
				ui.Text(c, body).FontSize(t.Type.Body).TextColor(t.Color.FgPrimary).Selectable()
			})
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
		Key:             fmt.Sprintf("report-%s-%d-%s", runID, idx, req.CaseID),
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
	var item caseDetailItem
	if open {
		cases := d.Progress.Cases
		idx := -1
		for i := range cases {
			if cases[i].CaseId == s.detailCaseID {
				idx = i
				break
			}
		}
		if idx < 0 {
			open = false
			s.detailCaseID = ""
		} else {
			item = detailCaseItem(c, cases[idx])
			item.Index = idx
			item.Total = len(cases)
			const pageSize = 50
			if idx > 0 {
				prevIdx := idx - 1
				item.OnPrev = func() {
					s.detailCaseID = cases[prevIdx].CaseId
					s.casesTablePage = prevIdx / pageSize
				}
			}
			if idx+1 < len(cases) {
				nextIdx := idx + 1
				item.OnNext = func() {
					s.detailCaseID = cases[nextIdx].CaseId
					s.casesTablePage = nextIdx / pageSize
				}
			}
		}
	}
	s.caseDetailDrawer(c, "detail-case-drawer", &open, item)
	if !open {
		s.detailCaseID = ""
	}
}

func (s *viewState) reportCaseDrawer(c *ui.Context, report *reportViewState, run core.ReportRun) {
	open := report != nil && report.detailCaseOpen && report.detailCaseIndex >= 0 && report.detailCaseIndex < len(run.Requests)
	var item caseDetailItem
	if open {
		idx := report.detailCaseIndex
		item = reportCaseItem(c, run.Source.RunID, idx, run.Requests[idx])
		item.Index = idx
		item.Total = len(run.Requests)
		if idx > 0 {
			prevIdx := idx - 1
			item.OnPrev = func() {
				report.detailCaseIndex = prevIdx
				report.requestsPage = prevIdx / reportRequestPageSize
			}
		}
		if idx+1 < len(run.Requests) {
			nextIdx := idx + 1
			item.OnNext = func() {
				report.detailCaseIndex = nextIdx
				report.requestsPage = nextIdx / reportRequestPageSize
			}
		}
	}
	s.caseDetailDrawer(c, "report-case-drawer", &open, item)
	if !open && report != nil {
		report.detailCaseOpen = false
		report.detailCaseIndex = -1
	}
}
