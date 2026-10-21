package pages

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
)

func errorView(c *ui.Context, e *core.ApiError) {
	if e == nil {
		return
	}
	message := e.Message
	if message == "" {
		message = human(e.Code)
	}
	ui.Text(c, message+" · "+e.Code).TextColor(c.Theme().Danger)
	for _, v := range e.Limitations {
		ui.Text(c, human(v)+" · "+v)
	}
	if len(e.Details) > 0 {
		kvview(c, "kv-error-details", e.Details)
	}
}
func taskLabel(t core.RunTask) string {
	if !terminal(t.State) && t.PreparationStage != "" {
		if label, ok := map[string]string{"awaiting_service": "等待绑定 LM Studio", "switching_service": "正在切换题包服务", "starting_engine": "正在启动题包服务", "binding": "正在校验服务绑定"}[t.PreparationStage]; ok {
			return label
		}
	}
	if t.Exit != nil {
		switch t.Exit.ExitCode {
		case 0:
			return "成功"
		case 2:
			return "输入 / 预检阻断"
		case 3:
			return "运行不完整"
		case 4:
			return "工具 / 证据错误"
		case 130:
			return "已取消"
		}
	}
	if t.State == "starting" && t.Queued {
		return "排队中"
	}
	return stateLabel(t.State)
}
func stateLabel(v string) string {
	labels := map[string]string{"orphaned": "遗留引擎", "": "未知状态", "stopped": "已停止", "stopping": "停止中", "starting": "启动中", "running": "运行中", "cancelling": "取消中", "done": "已结束", "failed": "失败", "cancelled": "已取消", "downloading": "下载中", "complete": "完整", "partial": "不完整", "dirty": "未排空", "pending": "待执行（聚合态）", "completed": "已完成", "invalid": "无效", "probe": "探测", "warmup": "预热", "baseline": "基线", "formal": "正式运行", "scoring": "评分", "residual": "残留检测", "finalizing": "收尾", "candidate_missing": "待创建候选", "bound_missing": "待绑定服务", "run_missing": "待运行", "run_incomplete": "运行未完成", "run_complete": "已封存", "preflight_missing": "待检查设备"}
	if label, ok := labels[v]; ok {
		return label
	}
	return v
}
func (s *viewState) historyRuns() []core.RunSummary {
	runs := []core.RunSummary{}
	seen := map[string]bool{}
	add := func(r core.RunSummary) {
		if !seen[r.Path] {
			seen[r.Path] = true
			runs = append(runs, r)
		}
	}
	for _, b := range s.history.Batches {
		for _, r := range b.Runs {
			add(r)
		}
	}
	for _, r := range s.history.StandaloneRuns {
		add(r)
	}
	return runs
}
func number[T ~int64 | ~float64](v *T) string {
	if v == nil {
		return "缺测"
	}
	return fmt.Sprint(*v)
}

func formatRate(v float64) string {
	switch {
	case v >= 1048576:
		return fmt.Sprintf("%.2f MiB/s", v/1048576)
	case v >= 1024:
		return fmt.Sprintf("%.1f KiB/s", v/1024)
	default:
		return fmt.Sprintf("%d bytes/s", int64(v))
	}
}

func groupedInt(v int64) string {
	s := fmt.Sprintf("%d", v)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	for i := range len(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func componentFold(c *ui.Context, title string, fn func()) {
	container := ui.Column(c.Key(title))
	open := ui.Local(container, "open", func() bool { return false })
	container.Children(func() { components.Collapsible(c, title, open, components.CollapsibleOptions{}, fn) })
}

func statusBadge(c *ui.Context, label string, color ui.Color) ui.Element {
	badge := components.Badge(c, label).TextColor(color).Background(color.Alpha(0.10))
	if c.Preferences().HighContrast {
		badge.Background(c.Theme().Surface).Border(1, color)
	}
	return badge
}
