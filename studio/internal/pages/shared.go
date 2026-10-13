package pages

import (
	"fmt"
	"github.com/egoist/mygo/ui"
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
		ui.Text(c, v)
	}
	if len(e.Details) > 0 {
		kvview(c, e.Details)
	}
}
func taskLabel(t core.RunTask) string {
	if t.Exit != nil {
		switch t.Exit.ExitCode {
		case 0:
			return "成功 · done"
		case 2:
			return "输入 / 预检阻断 · failed"
		case 3:
			return "运行不完整 · done · partial"
		case 4:
			return "工具 / 证据错误 · failed"
		case 130:
			return "已取消 · cancelled"
		}
	}
	if t.State == "starting" && t.Queued {
		return "排队中 · starting"
	}
	return stateLabel(t.State) + " · " + t.State
}
func stateLabel(v string) string {
	labels := map[string]string{"starting": "启动中", "running": "运行中", "cancelling": "取消中", "done": "已结束", "failed": "失败", "cancelled": "已取消", "downloading": "下载中", "complete": "完整", "partial": "不完整", "dirty": "未排空", "pending": "待执行（聚合态）", "completed": "已完成", "invalid": "无效", "probe": "探测", "warmup": "预热", "baseline": "基线", "formal": "正式运行", "scoring": "评分", "residual": "残留检测", "finalizing": "收尾", "candidate_missing": "待创建候选", "bound_missing": "待绑定服务", "run_missing": "待运行", "run_incomplete": "运行未完成", "run_complete": "已封存", "preflight_missing": "待检查设备"}
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
