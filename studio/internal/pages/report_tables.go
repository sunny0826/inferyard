package pages

import (
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func (r *reportViewState) categoryView(c *ui.Context, run core.ReportRun) {
	card(c, tr(c, "reports.tables.categories.title"), func() {
		categories := run.Dashboard.Categories
		if len(categories) == 0 {
			ui.Text(c, tr(c, "reports.tables.categories.empty"))
			return
		}
		columns := []ui.TableColumn{
			{Title: tr(c, "reports.tables.col_category"), MinWidth: c.Theme().Rem(12)},
			{Title: tr(c, "reports.tables.col_pass_denominator"), MinWidth: c.Theme().Rem(10)},
		}
		for _, category := range categories {
			if category.Rate.Excluded == nil || *category.Rate.Excluded > 0 {
				columns = append(columns, ui.TableColumn{Title: tr(c, "reports.tables.col_excluded"), MinWidth: c.Theme().Rem(5)})
				break
			}
		}
		selectableTable(&r.categories, func(i int) any { return categories[i].ID }, func(i int) string { return categories[i].Label })
		pageTable(c.Key("report-categories"), &r.categories, tr(c, "reports.tables.categories.aria_label"), columns, len(categories), func(row, col int) {
			category := categories[row]
			label := category.Label
			if label == "" {
				label = category.ID
			}
			ui.Text(c, []string{reportText(label), reportRate(category.Rate), reportNumber(category.Rate.Excluded)}[col]).SingleLine()
		}).Height(c.Theme().Space(8)*float32(min(len(categories), 8)+1) + 2)
	})
}

func (r *reportViewState) requestView(c *ui.Context, run core.ReportRun) {
	card(c, tr(c, "reports.tables.requests.title"), func() {
		requests := run.Requests
		if len(requests) == 0 {
			ui.Text(c, tr(c, "reports.tables.requests.empty"))
			return
		}
		selectableTable(&r.requests, func(i int) any { return i }, func(i int) string { return requests[i].CaseID })
		columns := []ui.TableColumn{
			{Title: tr(c, "reports.tables.col_case"), MinWidth: c.Theme().Rem(13)},
			{Title: tr(c, "reports.tables.col_category"), MinWidth: c.Theme().Rem(9)},
			{Title: tr(c, "reports.tables.col_execution"), MinWidth: c.Theme().Rem(6)},
			{Title: tr(c, "reports.tables.col_quality"), MinWidth: c.Theme().Rem(7)},
			{Title: tr(c, "reports.tables.col_latency"), MinWidth: c.Theme().Rem(7)},
		}
		pageTable(c.Key("report-requests"), &r.requests, tr(c, "reports.tables.requests.aria_label"), columns, len(requests), func(row, col int) {
			request := requests[row]
			ui.Text(c, []string{reportText(request.CaseID), reportText(request.CategoryLabel), reportText(stateLabel(request.State)), reportQualityFor(c, request.Quality), reportScaled(request.LatencyMS, 1000, "s")}[col]).SingleLine()
		}).Height(c.Theme().Space(8)*float32(min(len(requests), 8)+1) + 2)
		ui.Text(c, trf(c, "reports.tables.requests.total", "count", len(requests))).TextColor(c.Theme().TextMuted)
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
