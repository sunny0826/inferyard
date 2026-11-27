package pages

import (
	"fmt"
	"strings"
	"time"

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

var (
	caseModalFade = ui.ElementTransition{
		Duration: 160 * time.Millisecond,
		Colors:   true,
		Enter:    &ui.Motion{Opacity: 0},
		Exit:     &ui.Motion{Opacity: 0},
	}
	caseModalPop = ui.ElementTransition{
		Duration: 160 * time.Millisecond,
		Enter:    &ui.Motion{Opacity: 0, Y: 10},
		Exit:     &ui.Motion{Opacity: 0, Y: 10},
	}
)

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

func caseModalIconButton(c *ui.Context, variant components.ButtonVariant, icon func() *ui.SVG, iconSize float32, label string, disabled bool) ui.Element {
	t := tokens(c)
	b := components.Button(c, "", variant).
		Size(28, 28).Padding(0).Radius(t.Radius.Control).
		Label(label).Disabled(disabled)
	iconColor := t.Color.FgSecondary
	if variant == components.ButtonOutline {
		bg := t.Color.Surface
		iconColor = t.Color.FgPrimary
		if disabled {
			bg = t.Color.Sunken
			iconColor = t.Color.FgTertiary
		} else if b.Hovered() {
			bg = t.Color.SurfaceHover
		}
		b.Background(bg).Border(1, t.Color.BorderDefault)
	} else if disabled {
		iconColor = t.Color.FgTertiary
	} else if b.Hovered() {
		iconColor = t.Color.FgPrimary
	}
	b.Children(func() {
		components.Icon(c, icon).Size(iconSize, iconSize).TextColor(iconColor).Role(ui.RoleNone)
	})
	if !disabled {
		components.Tooltip(c, b, label)
	}
	return b
}

func (s *viewState) caseDetailDrawer(c *ui.Context, modalKey string, open *bool, item caseDetailItem) {
	c.Key(modalKey)
	if !*open {
		ui.Overlay(c, func() {})
		return
	}

	t := tokens(c)
	winW, winH := c.Size()
	if winW <= 0 {
		winW = 1120
	}
	if winH <= 0 {
		winH = 800
	}

	hasPrompt := strings.TrimSpace(item.Prompt) != ""
	hasResponse := strings.TrimSpace(item.Response) != ""
	split := hasPrompt && hasResponse && winW >= 860

	modalW := min(float32(640), max(float32(360), winW-80))
	if split {
		modalW = min(float32(940), max(float32(720), winW-80))
	}
	maxModalH := min(float32(680), max(float32(340), winH-96))
	bodyMaxH := max(float32(200), maxModalH-92)

	ariaTitle := tr(c, "detail.case_drawer.title")
	headerTitle := item.CaseID
	if headerTitle == "" {
		headerTitle = ariaTitle
	}
	reduceMotion := c.Preferences().ReduceMotion
	scrim := ui.RGBA(0, 0, 0, .24)
	if c.Theme().Dark {
		scrim = ui.RGBA(0, 0, 0, .48)
	}

	ui.Overlay(c, func() {
		backdrop := ui.Box(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).Background(scrim)
		if !reduceMotion {
			backdrop.Transition(caseModalFade)
		}
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			AlignItems(ui.Center).Justify(ui.Center).Padding(t.Density.Page).Modal()
		if back.Clicked() || back.OverlayShortcut(0, ui.KeyEscape) {
			*open = false
		}
		if item.OnPrev != nil && back.OverlayShortcut(0, ui.KeyLeft) {
			item.OnPrev()
		}
		if item.OnNext != nil && back.OverlayShortcut(0, ui.KeyRight) {
			item.OnNext()
		}

		back.Children(func() {
			panel := applyShadow(
				ui.Column(c).Role(ui.RoleDialog).Label(ariaTitle).
					Width(modalW).MaxWidthPercent(94).MaxHeight(maxModalH).
					Padding(0).Gap(0).
					Background(t.Color.Elevated).
					Border(1, t.Color.BorderDefault).
					Radius(t.Radius.Modal).Clip().
					TextColor(t.Color.FgPrimary),
				t.Elevation.LG,
			)
			if !reduceMotion {
				panel.Transition(caseModalPop)
			}
			panel.Clicked()
			panel.Children(func() {
				// 1. Header: Case ID + Status/Quality + Metadata strip on left; Prev/Next + single Close on right
				s.caseModalHeader(c, headerTitle, item, func() { *open = false })
				ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)

				// 2. Body: Edge-to-edge split Q&A or focused single-column prompt reaching the bottom radius
				s.caseModalBody(c, item, hasPrompt, hasResponse, split, bodyMaxH)
			})
		})
	})
}

func (s *viewState) caseModalHeader(c *ui.Context, headerTitle string, item caseDetailItem, onClose func()) {
	t := tokens(c)
	hasCat := item.Category != "" && item.Category != "—"
	hasDur := item.DurationText != "" && item.DurationText != "—"
	hasErr := item.ErrorCategory != ""
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Stack).
		Padding(t.Density.Section, t.Density.Page, t.Density.Section-2, t.Density.Page).
		Background(t.Color.Elevated).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Tight + 2).Children(func() {
			ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				ui.Text(c, headerTitle).Bold().FontSize(t.Type.CardTitle + 2).FontFeatures("tnum").
					TextColor(t.Color.FgPrimary).SingleLine().Selectable()
				if item.StateLabel != "" {
					inlineBadge(c, item.StateLabel, item.StateKind, badgeOptions{Size: sizeSM, Style: badgeDot})
				}
				if item.HasQualityBadge && item.QualityText != "" {
					inlineBadge(c, item.QualityText, item.QualityKind, badgeOptions{Size: sizeSM, Style: badgeSoft})
				} else if item.QualityText != "" && item.QualityText != "—" {
					inlineBadge(c, item.QualityText, stateNeutral, badgeOptions{Size: sizeSM, Style: badgeSoft})
				}
			})
			if hasCat || hasDur || hasErr {
				ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Stack).Children(func() {
					if hasCat {
						ui.Row(c).AlignItems(ui.Center).Gap(5).Children(func() {
							components.Icon(c, lucide.Layers).Size(12, 12).TextColor(t.Color.FgTertiary).Role(ui.RoleNone)
							ui.Text(c, item.Category).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).SingleLine()
						})
					}
					if hasDur {
						ui.Row(c).AlignItems(ui.Center).Gap(5).Children(func() {
							components.Icon(c, lucide.Clock).Size(12, 12).TextColor(t.Color.FgTertiary).Role(ui.RoleNone)
							ui.Text(c, item.DurationText).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary).SingleLine()
						})
					}
					if hasErr {
						inlineBadge(c, item.ErrorCategory, stateDanger, badgeOptions{Size: sizeSM, Style: badgeSoft})
					}
				})
			}
		})

		ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			if item.Total > 1 {
				ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Tight + 2).Children(func() {
					if caseModalIconButton(c, components.ButtonOutline, lucide.ArrowLeft, 13, tr(c, "detail.case_drawer.prev_case"), item.OnPrev == nil).Clicked() && item.OnPrev != nil {
						item.OnPrev()
					}
					ui.Text(c, fmt.Sprintf("%d / %d", item.Index+1, item.Total)).
						FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary).SingleLine()
					if caseModalIconButton(c, components.ButtonOutline, lucide.ArrowRight, 13, tr(c, "detail.case_drawer.next_case"), item.OnNext == nil).Clicked() && item.OnNext != nil {
						item.OnNext()
					}
				})
				ui.Box(c).Width(1).Height(18).Shrink(0).Background(t.Color.BorderSubtle)
			}
			if caseModalIconButton(c, components.ButtonGhost, lucide.X, 15, tr(c, "common.close"), false).Clicked() {
				onClose()
			}
		})
	})
}

func casePromptMissingKey(isReport bool) string {
	if isReport {
		return "detail.case_drawer.prompt_unavailable"
	}
	return "detail.case_drawer.prompt_missing"
}

func (s *viewState) caseModalBody(c *ui.Context, item caseDetailItem, hasPrompt, hasResponse, split bool, bodyMaxH float32) {
	t := tokens(c)
	if !hasPrompt && !hasResponse {
		ui.Column(c).FillWidth().Padding(t.Density.Page).Children(func() {
			infoBanner(c, tr(c, casePromptMissingKey(item.IsReport)))
		})
		return
	}

	accentKind := stateActive
	if item.HasQualityBadge {
		accentKind = item.QualityKind
	}

	if split {
		paneH := estimateSplitPaneHeight(item.Prompt, item.Response, bodyMaxH)
		ui.Row(c).FillWidth().Height(paneH).AlignItems(ui.Stretch).Gap(0).Children(func() {
			ui.Column(c).Grow(42).MinWidth(0).FillHeight().Background(t.Color.Sunken).Children(func() {
				s.casePaneHeader(c, lucide.FileText, t.Color.FgTertiary,
					tr(c, "detail.case_drawer.prompt_title"), tr(c, "detail.case_drawer.copy_prompt"),
					item.Prompt, t.Density.Page, t.Density.Section)
				ui.Scroll(c.Key("case-prompt-scroll-" + item.Key)).FillWidth().Grow(1).MinHeight(0).Children(func() {
					ui.Column(c).FillWidth().Padding(0, t.Density.Section, t.Density.Section, t.Density.Page).Children(func() {
						ui.Text(c, item.Prompt).FontSize(t.Type.Body).TextColor(t.Color.FgPrimary).Selectable()
					})
				})
			})
			ui.Box(c).Width(1).FillHeight().Shrink(0).Background(t.Color.BorderSubtle)
			ui.Column(c).Grow(58).MinWidth(0).FillHeight().Background(t.Color.Surface).Children(func() {
				s.casePaneHeader(c, lucide.CircleCheck, semanticAccentColor(c, stateScale(c, accentKind)),
					tr(c, "detail.case_drawer.response_title"), tr(c, "detail.case_drawer.copy_response"),
					item.Response, t.Density.Section, t.Density.Page)
				ui.Scroll(c.Key("case-response-scroll-" + item.Key)).FillWidth().Grow(1).MinHeight(0).Children(func() {
					ui.Column(c).FillWidth().Padding(0, t.Density.Page, t.Density.Section, t.Density.Section).Children(func() {
						ui.Text(c, item.Response).FontSize(t.Type.Body).TextColor(t.Color.FgPrimary).Selectable()
					})
				})
			})
		})
		return
	}

	ui.Scroll(c.Key("case-single-scroll-" + item.Key)).FillWidth().MaxHeight(bodyMaxH).Children(func() {
		ui.Column(c).FillWidth().Gap(0).Children(func() {
			if hasPrompt {
				ui.Column(c).FillWidth().MinHeight(124).Background(t.Color.Sunken).Children(func() {
					s.casePaneHeader(c, lucide.FileText, t.Color.FgTertiary,
						tr(c, "detail.case_drawer.prompt_title"), tr(c, "detail.case_drawer.copy_prompt"),
						item.Prompt, t.Density.Page, t.Density.Page)
					ui.Column(c).FillWidth().Padding(0, t.Density.Page, t.Density.Page, t.Density.Page).Children(func() {
						ui.Text(c, item.Prompt).FontSize(t.Type.Body).TextColor(t.Color.FgPrimary).Selectable()
					})
				})
			} else {
				ui.Column(c).FillWidth().Padding(t.Density.Section, t.Density.Page).Children(func() {
					infoBanner(c, tr(c, casePromptMissingKey(item.IsReport)))
				})
			}
			if hasResponse {
				if hasPrompt {
					ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
				}
				ui.Column(c).FillWidth().MinHeight(124).Background(t.Color.Surface).Children(func() {
					s.casePaneHeader(c, lucide.CircleCheck, semanticAccentColor(c, stateScale(c, accentKind)),
						tr(c, "detail.case_drawer.response_title"), tr(c, "detail.case_drawer.copy_response"),
						item.Response, t.Density.Page, t.Density.Page)
					ui.Column(c).FillWidth().Padding(0, t.Density.Page, t.Density.Page, t.Density.Page).Children(func() {
						ui.Text(c, item.Response).FontSize(t.Type.Body).TextColor(t.Color.FgPrimary).Selectable()
					})
				})
			}
		})
	})
}

func estimateSplitPaneHeight(prompt, response string, maxH float32) float32 {
	lines := func(text string, charsPerLine int) int {
		n := 0
		for _, para := range strings.Split(text, "\n") {
			r := len([]rune(para))
			n += max(1, (r+charsPerLine-1)/charsPerLine)
		}
		return n
	}
	maxLines := max(lines(prompt, 36), lines(response, 52))
	// 56px pane header + 24px bottom pad + ~24px per line
	est := float32(84 + maxLines*24)
	return min(maxH, max(float32(168), est))
}

func (s *viewState) casePaneHeader(c *ui.Context, icon func() *ui.SVG, iconColor ui.Color, label, copyLabel, body string, padLeft, padRight float32) {
	t := tokens(c)
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Tight+2).
		Padding(t.Density.Stack+2, padRight, t.Density.Stack, padLeft).Children(func() {
		components.Icon(c, icon).Size(13, 13).TextColor(iconColor).Shrink(0).Role(ui.RoleNone)
		ui.Text(c, label).Bold().FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).Grow(1).SingleLine()
		if caseModalIconButton(c, components.ButtonGhost, lucide.Copy, 14, copyLabel, false).Clicked() {
			s.copyCaseText(c, body)
		}
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
