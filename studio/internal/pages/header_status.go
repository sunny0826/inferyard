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
	t := c.Theme()
	q := s.queueActivity()
	ui.Row(c).FillWidth().Justify(ui.End).AlignItems(ui.Center).Gap(t.Space(2)).Padding(0, t.Space(6)).Children(func() {
		state := s.engineState.State
		if state == "" {
			state = "stopped"
		}
		color, animated := t.TextMuted, false
		switch state {
		case "running":
			color, animated = t.Success, true
		case "starting", "stopping":
			color, animated = t.Accent, true
		case "failed":
			color = t.Danger
		case "orphaned":
			color = t.Warning
		}
		statusPanel(c, "engine-status", "引擎 · "+stateLabel(state), lucide.Cpu, color, animated, func() {
			s.engineStatusPanel(c, q.active())
		})
		label := "任务 · 空闲"
		if q.active() {
			label = fmt.Sprintf("任务 · 执行 %d · 等待 %d", q.running, q.waiting)
		} else if s.busy {
			label = "任务 · 准备中"
		}
		queueColor := t.TextMuted
		if q.active() || s.busy {
			queueColor = t.Accent
		}
		statusPanel(c, "queue-status", label, lucide.ListTodo, queueColor, q.active() || s.busy, func() {
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
		})
		if n := s.activeDownloadCount(); n > 0 {
			label := fmt.Sprintf("下载中 %d 项", n)
			statusPanel(c, "download-status", label, lucide.Download, t.Accent, true, func() {
				ui.Text(c, label)
			})
		}
	})
}

type statusPanelState struct {
	open, hovered bool
	lastInside    time.Time
}

func statusPanel(c *ui.Context, key, label string, icon func() *ui.SVG, color ui.Color, animated bool, content func()) {
	c.Key(key)
	ui.Column(c).Children(func() {
		t := c.Theme()
		anchor := components.Button(c, "", components.ButtonGhost).
			Size(t.Space(9), t.Space(9)).Padding(0).Radius(t.Space(4.5)).
			Label(label).Description("悬停或按 Enter 查看详情")
		local := ui.Local(anchor, "status-panel", func() statusPanelState { return statusPanelState{} })
		anchor.Children(func() {
			el := components.Icon(c, icon).Size(t.Space(5), t.Space(5)).TextColor(color)
			if animated && !c.Preferences().ReduceMotion {
				phase := el.Loop("status-activity", 1400*time.Millisecond, ui.Linear)
				el.Opacity(.65 + .35*float32((math.Sin(float64(phase)*2*math.Pi)+1)/2))
			}
		})
		hovered := anchor.Hovered()
		if hovered && !local.hovered || anchor.Clicked() {
			local.open = true
			local.lastInside = c.Now()
		}
		local.hovered = hovered
		components.Popover(c, anchor, &local.open, func(panel ui.Element) {
			panel.AttachTo(anchor, ui.AnchorBottomRight, ui.AnchorTopRight).
				Width(t.Space(78)).MaxWidthPercent(100).Label(label + "面板")
			if hovered || panel.Hovered() || panel.FocusWithin() || anchor.Focused() {
				local.lastInside = c.Now()
			} else if c.Now().Sub(local.lastInside) >= 220*time.Millisecond {
				local.open = false
			} else {
				c.After(220 * time.Millisecond)
			}
			panel.Children(content)
		})
	})
}

func (s *viewState) engineStatusPanel(c *ui.Context, runActive bool) {
	state := s.engineState
	label := state.State
	if label == "" {
		label = "stopped"
	}
	heading(c, "模型引擎", 3)
	ui.Text(c, stateLabel(label))
	if state.Candidate != "" {
		ui.Text(c, "候选 · "+displayName(state.Candidate))
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
		button(c, "停止引擎").Disabled(!s.stopEnabled(true, runActive)).OnClick(s.stopEngine)
		if runActive {
			ui.Text(c, "请先结束或取消测评任务，再停止引擎。").TextColor(c.Theme().TextMuted)
		}
	case "orphaned":
		ui.Text(c, "遗留服务状态尚未确认，请到任务页处理。")
		button(c, "前往任务").OnClick(func() { s.selected, s.detailID = "dashboard", "" })
	default:
		primaryButton(c, "开启引擎").Disabled(!s.canStartManagedEngine()).OnClick(s.startManagedEngine)
		if s.managedEngineCandidate() == "" {
			ui.Text(c, "先在任务页选择模型并准备候选配置。").TextColor(c.Theme().TextMuted)
			button(c, "前往任务").OnClick(func() { s.selected, s.detailID = "dashboard", "" })
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
