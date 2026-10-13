package pages

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"math"
)

func (s *viewState) detailView(c *ui.Context) {
	s.header(c, "运行详情")
	if ui.Button(c, "返回运行").AlignSelf(ui.Start).Clicked() {
		s.detailID = ""
		s.detailMu.Lock()
		s.requestedDetail = ""
		s.detailMu.Unlock()
	}
	errorView(c, s.detailError)
	if s.detail == nil {
		ui.Text(c, "正在读取任务详情…")
		return
	}
	d := s.detail
	t := d.Task
	card(c, text(t.Bundle)+" · "+t.TaskId, func() {
		ui.Badge(c, taskLabel(t))
		ui.Text(c, "原始状态："+t.State).TextColor(c.Theme().TextMuted).Selectable()
		ui.Text(c, "运行目录："+text(t.RunPath)).Selectable()
		ui.Text(c, "任务类型："+t.Kind+" · 运行 ID："+text(t.RunId))
		errorView(c, t.Error)
		if ui.Button(c, "取消任务").AlignSelf(ui.Start).Disabled(s.busy || terminal(t.State) || t.State == "cancelling").Clicked() {
			id := t.TaskId
			s.work(func() (string, *core.ApiError) {
				_, e := s.service.CancelRun(id)
				return "已请求取消，等待安全收尾。", e
			})
		}
		if t.Exit != nil {
			kvview(c, t.Exit)
		}
	})
	card(c, "实时进度", func() {
		p := d.Progress
		if p == nil {
			ui.Text(c, "等待冻结题目与进度数据…")
			return
		}
		ui.Text(c, fmt.Sprintf("%s · %d / %s", stateLabel(emptyText(p.Phase)), p.Done, number(p.Total)))
		value := math.NaN()
		if p.Total != nil && *p.Total > 0 {
			value = float64(p.Done) / float64(*p.Total)
		}
		ui.Progress(c, value)
		denominator := "分母为冻结选中题集合"
		if t.Kind == "resume" {
			denominator = "分母为冻结续测子集"
		}
		ui.Text(c, fmt.Sprintf("%s；未读取时不猜测。当前题：%s · last_seq：%d", denominator, text(p.CurrentCaseId), p.LastSeq))
		columns := []ui.TableColumn{{Title: "题目", Width: 180}, {Title: "状态", Width: 160}, {Title: "类别", Width: 100}, {Title: "质量", Width: 100}, {Title: "耗时 ms", Width: 100}, {Title: "错误类别", Width: 160}}
		ui.Table(c, &s.casesTable, columns, len(p.Cases), func(row, col int) {
			item := p.Cases[row]
			values := []string{item.CaseId, stateLabel(item.State) + " · " + item.State, text(item.Category), text(item.QualityState), number(item.DurationMs), text(item.ErrorCategory)}
			ui.Text(c, values[col]).Selectable()
		}).Height(240)
	})
	s.resourceView(c, d.Resources)
}
