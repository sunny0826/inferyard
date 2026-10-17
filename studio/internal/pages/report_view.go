package pages

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
)

type reportViewState struct {
	reference            string
	index                *core.ReportIndex
	failure              *core.ApiError
	run                  int
	categories, requests pageTableState
}

func (s *viewState) readReport(reference string) (*core.ReportIndex, *core.ApiError) {
	if s.preview != nil {
		return previewReport(reference)
	}
	if s.service == nil {
		return nil, core.Err("not_found")
	}
	return s.service.ReportIndex(reference)
}

func (s *viewState) openReport(reference string) {
	index, e := s.readReport(reference)
	s.openedReport = &reportViewState{reference: reference, index: index, failure: e}
	s.selected = "reports"
}

func (s *viewState) reportView(c *ui.Context) {
	report := s.openedReport
	id := displayName(report.reference)
	if report.failure != nil {
		s.header(c, "报告 "+id)
		button(c, "返回报告列表").AlignSelf(ui.Start).OnClick(func() { s.openedReport = nil })
		ui.Text(c, "无法读取").TextColor(c.Theme().Danger)
		button(c, "重新读取报告").AlignSelf(ui.Start).OnClick(func() { s.openReport(report.reference) })
		return
	}
	if len(report.index.Runs) == 0 {
		s.header(c, "报告 "+id)
		button(c, "返回报告列表").AlignSelf(ui.Start).OnClick(func() { s.openedReport = nil })
		ui.Text(c, "报告中暂无运行数据。")
		return
	}
	run := report.index.Runs[report.run]
	s.header(c, reportModel(run))
	ui.Text(c, id+" · "+reportText(run.Facets.BundleLabel)).TextColor(c.Theme().TextMuted)
	button(c, "返回报告列表").AlignSelf(ui.Start).OnClick(func() { s.openedReport = nil })
	if len(report.index.Runs) > 1 {
		report.runPicker(c)
		run = report.index.Runs[report.run]
	}
	if run.Diagnostic {
		statusBadge(c, "诊断运行", c.Theme().Warning)
	}
	if run.Dashboard == nil {
		ui.Text(c, "无法读取 dashboard").TextColor(c.Theme().Danger)
	} else {
		reportMetrics(c, run)
		report.categoryView(c, run)
	}
	reportProfileView(c, run)
	report.requestView(c, run)
	card(c, "数据来源", func() {
		deviceRow(c, "运行", reportText(run.Source.RunID))
		deviceRow(c, "索引", "index.json")
		deviceRow(c, "停止原因", reportText(run.Summary.StopReason))
	})
}

func (r *reportViewState) runPicker(c *ui.Context) {
	ui.Field(c, "选择运行", func() {
		selectRun := components.Select(c, &r.run)
		selectRun.Trigger.Label("选择运行").Children(func() {
			ui.Text(c, reportRunLabel(r.index.Runs[r.run])).SingleLine()
		})
		selectRun.Popup(func(panel ui.Element) {
			for i, run := range r.index.Runs {
				selectRun.Item(i).Label(reportRunLabel(run)).Children(func() {
					ui.Text(c, reportRunLabel(run)).SingleLine()
				})
			}
		})
		if selectRun.Trigger.Changed() {
			r.categories, r.requests = pageTableState{}, pageTableState{}
		}
	})
	ui.Text(c, "各次运行分别展示，不合并成绩。").TextColor(c.Theme().TextMuted)
}

func reportRunLabel(run core.ReportRun) string {
	return reportText(run.Facets.BundleLabel) + " · " + reportText(run.Source.RunID)
}

func reportModel(run core.ReportRun) string {
	if run.Profile.Title != "" {
		return displayValue(run.Profile.Title)
	}
	return reportText(run.Model)
}

func reportProfileView(c *ui.Context, run core.ReportRun) {
	card(c, "模型与机器", func() {
		deviceRow(c, "模型", reportModel(run))
		engine := displayName(run.Profile.Engine.BinaryPath)
		if engine == "" {
			engine = run.Profile.Engine.Adapter
		}
		backend := strings.ToUpper(run.Profile.Engine.Backend)
		if backend == "METAL" {
			backend = "Metal"
		}
		deviceRow(c, "引擎", fmt.Sprintf("%s · %s · %s 线程", reportText(engine), reportText(backend), reportNumber(run.Threads)))
		machine := run.Profile.Machine
		deviceRow(c, "机器", fmt.Sprintf("%s · %s 核 · %s", reportText(machine.CPU), reportNumber(machine.LogicalCPUs), reportScaled(machine.MemoryTotalBytes, 1<<30, "GiB")))
		deviceRow(c, "题包", reportText(run.Facets.BundleLabel))
	})
}
