package pages

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func errorView(c *ui.Context, e *core.ApiError) {
	if e == nil {
		return
	}
	message := e.Message
	if message == "" {
		message = human(e.Code)
	}
	// A conflict carries its machine reason in the details fold. Name the known
	// reasons in primary text so the message is not just "操作与当前状态冲突".
	if e.Code == "conflict" {
		if reason, ok := e.Details["reason"].(string); ok {
			if label := conflictReasonLabel(reason); label != "" {
				message = label
			}
		}
	}
	ui.Text(c, message+" · "+e.Code).TextColor(stateColor(c, stateDanger))
	for _, v := range e.Limitations {
		ui.Text(c, human(v)+" · "+v)
	}
	if len(e.Details) > 0 {
		kvview(c, "kv-error-details", e.Details)
	}
}

// conflictReasonLabel names the conflict reasons users hit on page actions. An
// unknown reason returns empty so the caller keeps the generic conflict text,
// and the raw code stays visible in the error details fold.
func conflictReasonLabel(reason string) string {
	switch reason {
	case "not_resumable":
		return "当前运行不可续测；请用「新任务」重新测评。"
	case "run_active", "engine_active":
		return "已有任务或服务在运行，请先取消并等待收尾后重试。"
	case "engine_stopping":
		return "引擎正在停止，请等待回收后再运行。"
	case "engine_orphaned":
		return "存在遗留服务记录，请先在任务页处理后再运行。"
	case "settings_incomplete":
		return "目录配置缺项，请先在设置中补全路径。"
	case "output_exists":
		return "输出目录已存在，请选择新目录。"
	case "invalid_state":
		return "当前状态不支持该操作，请刷新进度后重试。"
	default:
		return ""
	}
}
func taskLabel(t core.RunTask) string {
	if !terminal(t.State) && t.PreparationStage != "" {
		if label, ok := map[string]string{"awaiting_service": "等待连接 LM Studio", "switching_service": "正在切换测评题集服务", "starting_engine": "正在启动测评题集服务", "binding": "正在核验服务连接"}[t.PreparationStage]; ok {
			return label
		}
	}
	if t.Exit != nil {
		switch t.Exit.ExitCode {
		case inferyard.ExitComplete:
			return "成功"
		case inferyard.ExitBlocked:
			return "输入 / 预检阻断"
		case inferyard.ExitIncomplete:
			return "运行不完整"
		case inferyard.ExitToolError:
			return "工具 / 证据错误"
		case inferyard.ExitCancelled:
			return "已取消"
		}
	}
	if t.State == "starting" && t.Queued {
		return "排队中"
	}
	return stateLabel(t.State)
}
func stateLabel(v string) string {
	labels := map[string]string{"orphaned": "遗留引擎", "": "缺测", "stopped": "已停止", "stopping": "停止中", "starting": "启动中", "running": "运行中", "cancelling": "取消中", "done": "已结束", "failed": "失败", "cancelled": "已取消", "downloading": "下载中", "complete": "完整", "partial": "不完整", "dirty": "未排空", "pending": "待执行", "completed": "已完成", "invalid": "无效", "probe": "探测", "warmup": "预热", "baseline": "基线", "formal": "正式运行", "scoring": "评分", "residual": "残留检测", "finalizing": "收尾", "candidate_missing": "待创建测评配置", "bound_missing": "待连接服务", "run_missing": "待运行", "run_incomplete": "运行未完成", "run_complete": "证据已收尾", "preflight_missing": "待检查设备"}
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
	componentFoldKeyed(c, title, title, fn)
}

// componentFoldKeyed is a fold whose key differs from its title, so the same
// title can appear under one parent more than once.
func componentFoldKeyed(c *ui.Context, key, title string, fn func()) {
	container := ui.Column(c.Key(key))
	open := ui.Local(container, "open", func() bool { return false })
	container.Children(func() { components.Collapsible(c, title, open, components.CollapsibleOptions{}, fn) })
}

// componentFoldOpen is a fold that starts expanded, for content the user
// needs to read without an extra click.
func componentFoldOpen(c *ui.Context, key, title string, fn func()) {
	container := ui.Column(c.Key(key))
	open := ui.Local(container, "open", func() bool { return true })
	container.Children(func() { components.Collapsible(c, title, open, components.CollapsibleOptions{}, fn) })
}
