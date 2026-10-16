package pages

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func (r *reportViewState) categoryView(c *ui.Context, run core.ReportRun) {
	card(c, "各类任务表现", func() {
		categories := run.Dashboard.Categories
		if len(categories) == 0 {
			ui.Text(c, "暂无分类质量数据。")
			return
		}
		columns := []ui.TableColumn{{Title: "类别", MinWidth: c.Theme().Rem(12)}, {Title: "通过 / 分母", MinWidth: c.Theme().Rem(10)}}
		for _, category := range categories {
			if category.Rate.Excluded == nil || *category.Rate.Excluded > 0 {
				columns = append(columns, ui.TableColumn{Title: "排除", MinWidth: c.Theme().Rem(5)})
				break
			}
		}
		selectableTable(&r.categories, func(i int) any { return categories[i].ID }, func(i int) string { return categories[i].Label })
		pageTable(c.Key("report-categories"), &r.categories, "质量分类", columns, len(categories), func(row, col int) {
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
	card(c, "逐题答案", func() {
		requests := run.Requests
		if len(requests) == 0 {
			ui.Text(c, "暂无逐题数据。")
			return
		}
		selectableTable(&r.requests, func(i int) any { return i }, func(i int) string { return requests[i].CaseID })
		columns := []ui.TableColumn{
			{Title: "题目", MinWidth: c.Theme().Rem(13)}, {Title: "类别", MinWidth: c.Theme().Rem(9)},
			{Title: "执行", MinWidth: c.Theme().Rem(6)}, {Title: "质量", MinWidth: c.Theme().Rem(7)}, {Title: "耗时", MinWidth: c.Theme().Rem(7)},
		}
		pageTable(c.Key("report-requests"), &r.requests, "逐题答案表", columns, len(requests), func(row, col int) {
			request := requests[row]
			ui.Text(c, []string{reportText(request.CaseID), reportText(request.CategoryLabel), reportText(stateLabel(request.State)), reportQuality(request.Quality), reportScaled(request.LatencyMS, 1000, "s")}[col]).SingleLine()
		}).Height(c.Theme().Space(8)*float32(min(len(requests), 8)+1) + 2)
		ui.Text(c, fmt.Sprintf("共 %d 题 · — 表示缺测，不当作 0。耗时由 ms 转为 s。", len(requests))).TextColor(c.Theme().TextMuted)
	})
}

func reportQuality(value *string) string {
	if value == nil {
		return "—"
	}
	switch *value {
	case "pass":
		return "通过"
	case "fail":
		return "未通过"
	case "unscorable":
		return "无法评分"
	case "not_scored":
		return "未评分"
	default:
		return reportText(*value)
	}
}
