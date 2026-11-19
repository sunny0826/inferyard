package pages

import (
	"fmt"
	"math"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"inferyard/studio/internal/core"
)

type queueActivity struct{ running, waiting int }

func (q queueActivity) active() bool { return q.running+q.waiting > 0 }

func (s *viewState) queueActivity() queueActivity {
	q := queueActivity{}
	speedListed := false
	for _, task := range s.tasks {
		if terminal(task.State) {
			continue
		}
		if task.Queued || task.State == "queued" {
			q.waiting++
		} else {
			q.running++
		}
		if s.speed != nil && s.speed.current != nil && task.TaskId == s.speed.current.id {
			speedListed = true
		}
	}
	if !speedListed && s.speed != nil && s.speed.current != nil && s.speed.current.active() {
		if s.speed.current.stage == speedQueued {
			q.waiting++
		} else {
			q.running++
		}
	}
	return q
}

func (s *viewState) cornerStatus(c *ui.Context) {
	t := tokens(c)
	q := s.queueActivity()
	ui.Row(c).FillWidth().Justify(ui.End).AlignItems(ui.Center).Gap(t.Density.Row).Padding(0, t.Density.Page).Children(func() {
		state := s.engineState.State
		if state == "" {
			state = "stopped"
		}
		color, animated := stateColor(c, stateNeutral), false
		switch state {
		case "running":
			color, animated = stateColor(c, stateSuccess), true
		case "starting", "stopping":
			color, animated = stateColor(c, stateActive), true
		case "failed":
			color = stateColor(c, stateDanger)
		case "orphaned":
			color = stateColor(c, stateWarning)
		}
		// The engine glyph differs from the 设备 nav entry's lucide.Cpu, and the
		// visible word names its subject so "已停止" never reads as the device.
		statusPanelParts(c, statusPanelOptions{key: "engine-status", label: "引擎 · " + stateLabel(state), visible: "引擎 · " + engineStatusText(state), icon: lucide.Server, color: color, animated: animated}, func(close func()) {
			s.engineStatusPanel(c, q.active(), close)
		})
		label := "任务 · 空闲"
		if q.active() {
			label = fmt.Sprintf("任务 · 执行 %d · 等待 %d", q.running, q.waiting)
		} else if s.busy {
			label = "任务 · 准备中"
		}
		queueColor := stateColor(c, stateNeutral)
		if q.active() || s.busy {
			queueColor = stateColor(c, stateActive)
		}
		statusPanelParts(c, statusPanelOptions{key: "queue-status", label: label, visible: label, icon: lucide.ListTodo, color: queueColor, animated: q.active() || s.busy}, func(close func()) {
			heading(c, "任务队列", 3)
			ui.Text(c, label)
			for _, task := range s.tasks {
				if !terminal(task.State) {
					name := "上下文速度"
					if task.Bundle != nil {
						name = *task.Bundle
					}
					ui.Text(c, name+" · "+taskLabel(task))
				}
			}
			button(c, "查看当前任务").AlignSelf(ui.Start).OnClick(func() {
				s.selected, s.detailID = "dashboard", ""
				close()
			})
		})
		if n := s.activeDownloadCount(); n > 0 {
			label := fmt.Sprintf("下载中 %d 项", n)
			statusPanelParts(c, statusPanelOptions{key: "download-status", label: label, visible: label, icon: lucide.Download, color: stateColor(c, stateActive), animated: true}, func(close func()) {
				ui.Text(c, label)
				button(c, "前往下载页").AlignSelf(ui.Start).OnClick(func() {
					s.selected = "downloads"
					close()
				})
			})
		}
	})
}

type statusPanelState struct {
	open bool
}

// statusPanel is a header status control: an icon plus a word or count, so the
// meaning is readable without hovering. label stays the accessible name used by
// the popover and focus order; visible is the short text beside the icon.
// statusPanelOptions carries every display field so the control stays under the
// size policy; key keeps the identity, label the accessible name, visible the
// short text, icon the glyph, color the accent and animated the activity pulse.
type statusPanelOptions struct {
	key, label, visible string
	icon                func() *ui.SVG
	color               ui.Color
	animated            bool
}

// statusPanelParts opens its popover on click or Enter only. Pointer movement
// across the header no longer opens a panel that would cover the page's own
// top-right actions, and MyGo closes it on Escape or a press outside. content
// receives close so a navigation action can dismiss the panel it came from.
func statusPanelParts(c *ui.Context, o statusPanelOptions, content func(close func())) {
	c.Key(o.key)
	ui.Column(c).Children(func() {
		t := c.Theme()
		anchor := components.Button(c, "", components.ButtonGhost).
			Padding(t.Space(.75), t.Space(1.5)).Radius(t.Space(4.5)).
			Label(o.label).Description("点击或按 Enter 查看详情")
		local := ui.Local(anchor, "status-panel", func() statusPanelState { return statusPanelState{} })
		anchor.Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(t.Space(.75)).Children(func() {
				el := components.Icon(c, o.icon).Size(t.Space(4.5), t.Space(4.5)).TextColor(o.color)
				if o.animated && !c.Preferences().ReduceMotion {
					phase := el.Loop("status-activity", 1400*time.Millisecond, ui.Linear)
					el.Opacity(.65 + .35*float32((math.Sin(float64(phase)*2*math.Pi)+1)/2))
				}
				ui.Text(c, o.visible).FontSize(tokens(c).Type.Caption).SingleLine().TextColor(o.color)
			})
		})
		if anchor.Clicked() {
			local.open = !local.open
		}
		components.Popover(c, anchor, &local.open, func(panel ui.Element) {
			panel.AttachTo(anchor, ui.AnchorBottomRight, ui.AnchorTopRight).
				Width(t.Space(78)).MaxWidthPercent(100).Label(o.label + "面板")
			panel.Children(func() { content(func() { local.open = false }) })
		})
	})
}

// engineStatusText is the short visible word for the engine status control.
func engineStatusText(state string) string {
	switch state {
	case "running":
		return "运行中"
	case "starting":
		return "启动中"
	case "stopping":
		return "停止中"
	case "failed":
		return "失败"
	case "orphaned":
		return "遗留服务"
	default:
		return "已停止"
	}
}

func (s *viewState) engineStatusPanel(c *ui.Context, runActive bool, close func()) {
	state := s.engineState
	label := state.State
	if label == "" {
		label = "stopped"
	}
	heading(c, "模型引擎", 3)
	ui.Text(c, stateLabel(label))
	if state.Candidate != "" {
		ui.Text(c, "测评配置："+displayName(state.Candidate))
	}
	if state.Pid > 0 {
		ui.Text(c, fmt.Sprintf("PID %d", state.Pid))
	}
	if state.Endpoint != "" {
		ui.Text(c, loopbackEndpoint(state.Endpoint))
	}
	errorView(c, state.Error)
	switch state.State {
	case "starting", "running", "stopping":
		if s.destructiveAction(c, destructiveOption{
			kind: destructiveStopEngine, label: "停止引擎", confirmLabel: "确认停止引擎",
			prompt:   "将停止 Studio 启动的模型服务；正在加载的模型会中断，之后需要重新启动并绑定。",
			disabled: !s.stopEnabled(true, runActive),
		}) {
			s.stopEngine()
		}
		if runActive {
			ui.Text(c, "请先结束或取消测评任务，再停止引擎。").TextColor(c.Theme().TextMuted)
		}
	case "orphaned":
		ui.Text(c, "遗留服务状态尚未确认，请到任务页处理。")
		button(c, "前往任务").OnClick(func() { s.selected, s.detailID = "dashboard", ""; close() })
	default:
		primaryButton(c, "开启引擎").Disabled(!s.canStartManagedEngine()).OnClick(s.startManagedEngine)
		if s.managedEngineCandidate() == "" {
			ui.Text(c, "先在任务页选择模型并创建测评配置。").TextColor(c.Theme().TextMuted)
			button(c, "前往任务").OnClick(func() { s.selected, s.detailID = "dashboard", ""; close() })
		}
	}
}

func (s *viewState) managedEngineCandidate() string {
	if s.task.start.candidate != "" {
		return s.task.start.candidate
	}
	return s.candidate
}

func (s *viewState) canStartManagedEngine() bool {
	state := s.engineState.State
	return (state == "" || state == "stopped" || state == "failed") &&
		!s.busy && !s.newTaskPending && !s.queueActivity().active() && s.managedEngineCandidate() != ""
}

func (s *viewState) startManagedEngine() {
	if !s.canStartManagedEngine() {
		return
	}
	candidate := s.managedEngineCandidate()
	if s.simulate(previewManagedEngine{candidate: candidate}) {
		return
	}
	s.guideWork(func() (string, *core.ApiError) {
		_, e := s.service.StartEngine(candidate)
		return "已请求开启引擎。", e
	})
}
