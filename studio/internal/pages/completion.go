package pages

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

// completionView is the report step after a completed scope. It summarises the
// runs this task produced from data Studio already holds (the run history), so
// it never rescans evidence. Missing measurements keep an explicit reason
// instead of rendering as zero.
func (s *viewState) completionView(c *ui.Context) {
	runs := s.completionRuns()
	path := s.completedReportPath()
	if len(runs) == 0 {
		ui.Text(c, "本轮还没有可读取的运行结果；报告页可以查看历史记录。")
		s.completionActions(c, path)
		return
	}
	ui.Text(c, fmt.Sprintf("本轮共 %d 次运行。", len(runs)))
	for _, run := range runs {
		card(c, completionRunTitle(run), func() {
			deviceRow(c, "通过率", runPassRate(run))
			deviceRow(c, "耗时", runDuration(run))
			deviceRow(c, "状态", stateLabel(run.Status))
		})
	}
	s.completionActions(c, path)
}

func (s *viewState) completionActions(c *ui.Context, path string) {
	ui.Row(c).Wrap().Gap(tokens(c).Density.CardGap).Children(func() {
		if path != "" {
			if primaryButton(c, "查看报告").AlignSelf(ui.Start).Clicked() {
				s.openReport(path)
			}
		} else if primaryButton(c, "查看报告列表").AlignSelf(ui.Start).Clicked() {
			s.selected = "reports"
		}
		if button(c, "再次测评").AlignSelf(ui.Start).Clicked() {
			s.newBenchmark()
		}
	})
}

// completedReportPath is the report this task wrote, or the workspace report
// that already covers the default scope.
func (s *viewState) completedReportPath() string {
	if s.task.report != "" {
		return s.task.report
	}
	if s.progress != nil {
		return emptyText(s.progress.Report.Path)
	}
	return ""
}

// completionRuns matches the run history against the runs this task produced.
// The task keeps run paths; only when this task recorded none does it fall back
// to the workspace report's run ids. Mixing both would attribute earlier runs
// to this round.
func (s *viewState) completionRuns() []core.RunSummary {
	keys := map[string]bool{}
	for _, path := range s.task.runs {
		keys[path] = true
	}
	if len(keys) == 0 && s.progress != nil {
		for _, id := range s.progress.Report.Runs {
			keys[id] = true
		}
	}
	if len(keys) == 0 {
		return nil
	}
	var runs []core.RunSummary
	for _, run := range s.historyRuns() {
		if keys[run.Path] || keys[run.RunId] {
			runs = append(runs, run)
		}
	}
	return runs
}

func completionRunTitle(run core.RunSummary) string {
	name := text(run.Bundle)
	if model := emptyText(run.ModelName); model != "" {
		name = model + " · " + name
	}
	return name
}

// runPassRate reports the recorded pass count and rate. A nil score means the
// run did not record one; a zero denominator means nothing executed. A failed
// or unscorable or unscored case stays in the denominator, so three passes plus
// one such case read 3 / 4 with the cause named, never 100%.
func runPassRate(run core.RunSummary) string {
	if run.Score == nil {
		return "缺测 — 未记录分数"
	}
	if run.Score.Valid == 0 {
		return "缺测 — 没有有效题"
	}
	if gap := run.Score.RateGap(); gap != "" {
		return fmt.Sprintf("%d / %d · 缺测 — %s，分母不缩", run.Score.Passed, run.Score.Valid, gapDescription(*run.Score, gap))
	}
	return fmt.Sprintf("%d / %d · %.1f%%", run.Score.Passed, run.Score.Valid, *run.Score.Rate*100)
}

// gapDescription names how many counted cases fall into the gap and why.
func gapDescription(score core.RunScore, gap string) string {
	switch gap {
	case "执行失败":
		return fmt.Sprintf("%d 题执行失败", score.Failed)
	case "不可评分":
		return fmt.Sprintf("%d 题不可评分", score.Unscorable)
	default:
		return fmt.Sprintf("%d 题缺评分", score.Missing)
	}
}

// runDuration derives the run wall time from its recorded start and finish.
func runDuration(run core.RunSummary) string {
	start, finish := emptyText(run.StartedAt), emptyText(run.FinishedAt)
	if start == "" || finish == "" {
		return "缺测 — 未记录开始或结束时间"
	}
	from, err := time.Parse(time.RFC3339Nano, start)
	if err != nil {
		return "缺测 — 开始时间无法解析"
	}
	to, err := time.Parse(time.RFC3339Nano, finish)
	if err != nil {
		return "缺测 — 结束时间无法解析"
	}
	if to.Before(from) {
		return "缺测 — 结束早于开始"
	}
	return formatDuration(to.Sub(from))
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%d 秒", int(d.Seconds()))
	}
	hours := int(d / time.Hour)
	minutes := int(d/time.Minute) % 60
	seconds := int(d/time.Second) % 60
	if hours > 0 {
		return fmt.Sprintf("%d 小时 %d 分", hours, minutes)
	}
	return fmt.Sprintf("%d 分 %d 秒", minutes, seconds)
}
