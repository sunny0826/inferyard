package pages

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

// stateBanner draws a semantic banner: a tinted surface with a 2 DIP state bar
// on the leading edge and a state icon before the content. High contrast keeps
// the plain surface and draws a full outline instead of a tint. The callback
// receives the foreground colour that stays readable on the tint.
func stateBanner(c *ui.Context, kind stateKind, icon func() *ui.SVG, fn func(foreground ui.Color)) {
	t := tokens(c)
	theme := c.Theme()
	color := stateColor(c, kind)
	banner := ui.Column(c).Gap(t.Density.Tight).Padding(t.Density.Inset).Radius(t.Radius.Inset)
	if c.Preferences().HighContrast {
		banner.Background(theme.Surface).Border(1, color)
	} else {
		banner.Background(theme.Surface.Mix(color, .07)).BorderWidth(0, 0, 0, 2).BorderColor(color)
	}
	foreground := color.Mix(theme.Text, .2)
	banner.Children(func() {
		ui.Row(c).AlignItems(ui.Start).Gap(t.Density.Row).Children(func() {
			components.Icon(c, icon).Size(t.Type.Body, t.Type.Body).TextColor(foreground)
			ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Tight).Children(func() { fn(foreground) })
		})
	})
}

// errorView renders a failure as a danger-tinted banner with an alert icon, so
// it never reads as plain body text and colour is not the only signal.
func errorView(c *ui.Context, e *core.ApiError) {
	if e == nil {
		return
	}
	message := e.Message
	if message == "" {
		message = human(e.Code)
	}
	stateBanner(c, stateDanger, lucide.CircleAlert, func(foreground ui.Color) {
		ui.Text(c, message+" · "+e.Code).TextColor(foreground)
		for _, v := range e.Limitations {
			ui.Text(c, human(v)+" · "+v).TextColor(foreground)
		}
		if len(e.Details) > 0 {
			kvview(c, "kv-error-details", e.Details)
		}
	})
}

// warningBanner renders a limitation that needs handling but is not a failure.
// It shares the banner geometry with errors and differs by hue and icon, so a
// warning still carries text and a shape instead of colour alone.
func warningBanner(c *ui.Context, message string) {
	if message == "" {
		return
	}
	stateBanner(c, stateWarning, lucide.TriangleAlert, func(foreground ui.Color) {
		ui.Text(c, message).Role(ui.RoleStatus).TextColor(foreground)
	})
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
