package pages

import (
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/icons/reicon"
)

func (r *reportViewState) categoryView(c *ui.Context, run core.ReportRun) {
	t := tokens(c)
	reportSectionCard(c, reicon.Layers, tr(c, "reports.tables.categories.title"), nil, func() {
		categories := run.Dashboard.Categories
		if len(categories) == 0 {
			ui.Text(c, tr(c, "reports.tables.categories.empty")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			return
		}
		columns := []ui.TableColumn{
			{Title: tr(c, "reports.tables.col_category"), MinWidth: c.Theme().Rem(12)},
			{Title: tr(c, "reports.tables.col_pass_denominator"), MinWidth: c.Theme().Rem(12)},
		}
		for _, category := range categories {
			if category.Rate.Excluded == nil || *category.Rate.Excluded > 0 {
				columns = append(columns, ui.TableColumn{Title: tr(c, "reports.tables.col_excluded"), MinWidth: c.Theme().Rem(5)})
				break
			}
		}
		selectableTable(&r.categories, func(i int) any { return categories[i].ID }, func(i int) string { return categories[i].Label })
		applyMaterial(c, ui.Box(c).FillWidth().MinWidth(0).Clip(), materialInset).Children(func() {
			pageTable(c.Key("report-categories"), &r.categories, tr(c, "reports.tables.categories.aria_label"), columns, len(categories), func(row, col int) {
				category := categories[row]
				label := category.Label
				if label == "" {
					label = category.ID
				}
				switch col {
				case 0:
					ui.Text(c, reportText(label)).Bold().SingleLine()
				case 1:
					rateText := reportRate(category.Rate)
					ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
						ui.Text(c, rateText).FontFeatures("tnum").SingleLine()
						if category.Rate.Numerator != nil && category.Rate.Denominator != nil && *category.Rate.Denominator > 0 {
							ratio := float64(*category.Rate.Numerator) / float64(*category.Rate.Denominator)
							reportRatioBar(c, ratio, 56, 4, t.Color.Accent)
						}
					})
				case 2:
					exclText := reportNumber(category.Rate.Excluded)
					exclColor := t.Color.FgTertiary
					if category.Rate.Excluded != nil && *category.Rate.Excluded > 0 {
						exclColor = stateScale(c, stateWarning).Fg
					}
					ui.Text(c, exclText).FontFeatures("tnum").TextColor(exclColor).SingleLine()
				}
			}).Height(c.Theme().Space(8) + c.Theme().Space(9)*float32(min(len(categories), 8)))
		})
	})
}

const reportRequestPageSize = 50

func reportQualityBadgeKind(value *string) (stateKind, bool) {
	if value == nil {
		return stateNeutral, false
	}
	switch *value {
	case "pass":
		return stateSuccess, true
	case "fail":
		return stateDanger, true
	case "unscorable":
		return stateWarning, true
	default:
		return stateNeutral, false
	}
}

func (r *reportViewState) requestView(c *ui.Context, run core.ReportRun) {
	t := tokens(c)
	requests := run.Requests
	totalCount := len(requests)
	pages := max(1, (totalCount+reportRequestPageSize-1)/reportRequestPageSize)
	r.requestsPage = min(max(0, r.requestsPage), pages-1)

	var trailing func()
	if totalCount > 0 {
		trailing = func() {
			studioTooltipGroup(c, ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row), func() {
				inlineBadge(c, trf(c, "reports.tables.requests.total", "count", totalCount), stateNeutral, badgeOptions{Size: sizeSM, Style: badgeSoft})
				if pages > 1 {
					ui.Text(c, trf(c, "chart.table.page_status", "page", r.requestsPage+1, "totalPages", pages, "count", totalCount)).
						FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary)
					if outlineIconButton(c, reicon.ArrowLeft, tr(c, "reports.tables.requests.prev_page")).Disabled(r.requestsPage == 0).Clicked() {
						r.requestsPage--
					}
					if outlineIconButton(c, reicon.ArrowRight, tr(c, "reports.tables.requests.next_page")).Disabled(r.requestsPage == pages-1).Clicked() {
						r.requestsPage++
					}
				}
			})
		}
	}

	reportSectionCard(c, reicon.FileText, tr(c, "reports.tables.requests.title"), trailing, func() {
		if totalCount == 0 {
			ui.Text(c, tr(c, "reports.tables.requests.empty")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			return
		}
		start := r.requestsPage * reportRequestPageSize
		visible := requests[start:min(start+reportRequestPageSize, totalCount)]
		selectableTable(&r.requests, func(i int) any { return start + i }, func(i int) string { return visible[i].CaseID })
		columns := []ui.TableColumn{
			{Title: tr(c, "reports.tables.col_case"), MinWidth: c.Theme().Rem(13)},
			{Title: tr(c, "reports.tables.col_category"), MinWidth: c.Theme().Rem(9)},
			{Title: tr(c, "reports.tables.col_execution"), MinWidth: c.Theme().Rem(7)},
			{Title: tr(c, "reports.tables.col_quality"), MinWidth: c.Theme().Rem(7)},
			{Title: tr(c, "reports.tables.col_latency"), MinWidth: c.Theme().Rem(7)},
		}
		applyMaterial(c, ui.Box(c).FillWidth().MinWidth(0).Clip(), materialInset).Children(func() {
			pageTable(c.Key("report-requests"), &r.requests, tr(c, "reports.tables.requests.aria_label"), columns, len(visible), func(row, col int) {
				request := visible[row]
				switch col {
				case 0:
					ui.Text(c, reportText(request.CaseID)).FontFeatures("tnum").SingleLine().Selectable()
				case 1:
					catColor := t.Color.FgSecondary
					if request.CategoryLabel == "" {
						catColor = t.Color.FgTertiary
					}
					ui.Text(c, reportText(request.CategoryLabel)).TextColor(catColor).SingleLine()
				case 2:
					inlineBadge(c, reportText(stateLabel(request.State)), caseStateKind(request.State), badgeOptions{Size: sizeSM, Style: badgeDot})
				case 3:
					qText := reportQualityFor(c, request.Quality)
					if kind, ok := reportQualityBadgeKind(request.Quality); ok {
						inlineBadge(c, qText, kind, badgeOptions{Size: sizeSM, Style: badgeSoft})
					} else {
						ui.Text(c, qText).FontSize(t.Type.Caption).TextColor(t.Color.FgTertiary).SingleLine()
					}
				case 4:
					latColor := t.Color.FgPrimary
					if request.LatencyMS == nil {
						latColor = t.Color.FgTertiary
					}
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
						ui.Text(c, reportScaled(request.LatencyMS, 1000, "s")).FontFeatures("tnum").TextColor(latColor).Grow(1).MinWidth(0).SingleLine()
						studioIcon(c, reicon.ChevronRight, t.Color.FgTertiary)
					})
				}
			}).Height(c.Theme().Space(8) + c.Theme().Space(10)*float32(min(len(visible), 8)))
		})
		if r.requests.clicked || r.requests.submitted {
			if idx := selectedRow(&r.requests, len(visible)); idx >= 0 {
				r.detailCaseIndex = start + idx
				r.detailCaseOpen = true
			}
		}
	})
}

func reportQuality(value *string) string {
	return reportQualityFor(nil, value)
}

func reportQualityFor(c *ui.Context, value *string) string {
	if value == nil {
		return "—"
	}
	switch *value {
	case "pass":
		return tr(c, "reports.quality.pass")
	case "fail":
		return tr(c, "reports.quality.fail")
	case "unscorable":
		return tr(c, "reports.quality.unscorable")
	case "not_scored":
		return tr(c, "reports.quality.not_scored")
	case "not_applicable":
		return tr(c, "reports.quality.not_applicable")
	default:
		return reportText(*value)
	}
}
