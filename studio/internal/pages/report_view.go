package pages

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
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
		return s.preview.report(reference)
	}
	if s.service == nil {
		return nil, core.Err("not_found")
	}
	return s.service.ReportIndex(reference)
}

func (s *viewState) openReport(reference string) {
	if s.speed != nil {
		s.speed.opened = nil
	}
	index, e := s.readReport(reference)
	s.openedReport = &reportViewState{reference: reference, index: index, failure: e}
	s.selected = "reports"
}

func (s *viewState) reportBackLink(c *ui.Context) {
	button(c, "返回报告列表").OnClick(func() {
		s.openedReport, s.reportsTab = nil, reportsTabList
	})
}

func (s *viewState) reportView(c *ui.Context) {
	report := s.openedReport
	id := displayName(report.reference)
	s.reportBackLink(c)
	if report.failure != nil {
		s.header(c, "报告 "+id)
		noticeBanner(c, noticeBannerOptions{
			Kind:    stateDanger,
			Message: "无法读取",
			Actions: func() {
				button(c, "重新读取报告").OnClick(func() { s.openReport(report.reference) })
			},
		})
		return
	}
	if len(report.index.Runs) == 0 {
		s.header(c, "报告 "+id)
		emptyGuideCard(c, emptyGuideOptions{
			Icon:        lucide.FileText,
			Title:       "报告中暂无运行数据。",
			Surface:     emptyCard,
		})
		return
	}
	run := report.index.Runs[report.run]
	s.header(c, reportModel(run))
	ui.Text(c, id+" · "+reportText(run.Facets.BundleLabel)).TextColor(c.Theme().TextMuted)
	if len(report.index.Runs) > 1 {
		report.runPicker(c)
		run = report.index.Runs[report.run]
	}
	if run.Diagnostic {
		statusBadge(c, "诊断运行", stateColor(c, stateWarning))
	}
	reportMetrics(c, run)
	reportSummaryGrid(c,
		func(c *ui.Context) { reportThroughput(c, run) },
		func(c *ui.Context) { reportOutputIntervals(c, run) },
	)
	report.categoryView(c, run)
	reportSummaryGrid(c,
		func(c *ui.Context) { reportProfileView(c, run) },
		func(c *ui.Context) { reportSourceView(c, run) },
	)
	report.requestView(c, run)
}

func reportSummaryGrid(c *ui.Context, cards ...func(*ui.Context)) {
	if compactLayout(c) {
		for _, build := range cards {
			build(c)
		}
		return
	}
	ui.Grid(c).Columns(2).Gap(tokens(c).Density.Stack).Children(func() {
		for _, build := range cards {
			build(c)
		}
	})
}

func reportSourceView(c *ui.Context, run core.ReportRun) {
	card(c, "数据来源", func() {
		deviceRow(c, "运行", reportText(run.Source.RunID))
		deviceRow(c, "索引", "studio.json")
		deviceRow(c, "停止原因", stopReasonLabel(run.Summary.StopReason))
	})
}

func stopReasonLabel(raw string) string {
	switch raw {
	case "":
		return "—"
	case "plan_finished", "completed":
		return "已完成"
	case "user_cancelled", "cancelled":
		return "用户取消"
	case "run_in_progress":
		return "运行进行中"
	case "interrupted_checkpoint":
		return "中断后恢复的检查点"
	case "diagnostic_run_not_formal":
		return "诊断运行，不进入正式测评"
	case "check_only_no_formal_cases":
		return "仅检查，无正式题目"
	case "unscorable_case":
		return "存在无法评分的题目"
	case "service_stop_unconfirmed":
		return "服务停止未确认"
	default:
		return "未识别停止原因：" + raw
	}
}

func (r *reportViewState) runPicker(c *ui.Context) {
	ui.Field(c, "选择运行", func() {
		selectRun := components.Select(c, &r.run)
		selectRun.Trigger.Label("选择运行").Value(reportRunLabel(r.index.Runs[r.run])).Children(func() {
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
}

func reportRunLabel(run core.ReportRun) string {
	return reportText(run.Facets.BundleLabel) + " · " + reportText(run.Source.RunID)
}

func reportModel(run core.ReportRun) string {
	if run.Profile.Title != nil && *run.Profile.Title != "" {
		return displayValue(*run.Profile.Title)
	}
	return reportText(run.Model)
}

func reportProfileView(c *ui.Context, run core.ReportRun) {
	card(c, "模型与机器", func() {
		deviceRow(c, "模型", reportModel(run))
		engine := displayName(emptyText(run.Profile.Engine.BinaryPath))
		if engine == "" {
			engine = run.Profile.Engine.Adapter
		}
		backend := strings.ToUpper(run.Profile.Engine.Backend)
		if backend == "METAL" {
			backend = "Metal"
		}
		deviceRow(c, "引擎", fmt.Sprintf("%s · %s · %s 线程", reportText(engine), reportText(backend), reportNumber(run.Threads)))
		machine := run.Profile.Machine
		deviceRow(c, "机器", fmt.Sprintf("%s · %s 核 · %s", reportText(emptyText(machine.CPU)), reportNumber(machine.LogicalCPUs), reportScaled(machine.MemoryTotalBytes, 1<<30, "GiB")))
		deviceRow(c, "测评题集", reportText(run.Facets.BundleLabel))
	})
}
