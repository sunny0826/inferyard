//go:build ui_debug && !release

package pages

import (
	"context"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/shells/sidebar"
	"inferyard/studio/internal/core"
)

const previewNotice = "此操作尚未接入模拟流程，未执行实际操作。"

type previewSession struct {
	ctx                context.Context
	state              *viewState
	win                *mygo.Window
	scene              int
	candidate          int
	generation         int
	notice             string
	failNext           bool
	serial             int
	clock              time.Duration
	lastTick           time.Time
	jobs               []previewEvent
	details            map[string]*core.RunTaskDetail
	indexes            map[string]*core.ReportIndex
	reportReadFailed   bool
	speedFailureStage  string
	speedFailure       *core.ApiError
	speedMissingTiming bool
	local              *previewLocalTask
}

// NewPreview opens the real page builders with in-memory fixtures and no Service.
func NewPreview(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	p := newPreviewSession(ctx)
	shell := sidebar.NewShell(p.shellOptions())
	p.win = shell.Window()
	p.state.win = p.win
	p.win.OnClosed(cancel)
	go p.runClock()
}

func newPreviewSession(ctx context.Context) *previewSession {
	p := &previewSession{ctx: ctx}
	p.loadScene(0)
	return p
}

func (p *previewSession) shellOptions() sidebar.ShellOptions {
	options := pageShellOptions("InferYard Studio · UI 调试", "", p.left, p.content)
	options.Theme = p.theme
	options.ContentTitle = func(c *ui.Context) {
		ui.Text(c, "UI 调试 · 模拟数据").SingleLine().TextColor(stateColor(c, stateWarning))
	}
	return options
}

// theme installs the selected visual candidate for the preview window only.
func (p *previewSession) theme(c *ui.Context) *ui.Theme { return candidateTheme(p.candidate, c) }

func (p *previewSession) loadScene(index int) {
	p.scene, p.notice = index, ""
	p.generation++
	p.jobs, p.clock, p.lastTick = nil, 0, time.Now()
	p.failNext, p.reportReadFailed, p.serial = false, false, 0
	p.speedMissingTiming = false
	p.speedFailureStage, p.speedFailure = "", nil
	p.details, p.indexes = map[string]*core.RunTaskDetail{}, map[string]*core.ReportIndex{}
	p.local = nil
	s := previewBase(p.ctx)
	s.preview, s.win = p, p.win
	p.state = s
	previewScenes[index].apply(s)
	p.activateScene()
}

func (p *previewSession) left(c *ui.Context) {
	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).FillWidth().Children(func() {
			ui.Column(c.Key(p.generation)).Children(func() { p.state.leftNav(c) })
			ui.Column(c).Padding(tokens(c).Density.Stack).Gap(tokens(c).Density.Row).Children(func() {
				heading(c, "UI 调试", 2)
				ui.Field(c, "模拟场景", func() {
					choice := components.Select(c, &p.scene)
					choice.Trigger.Label("模拟场景").Children(func() { ui.Text(c, previewScenes[p.scene].label).SingleLine() })
					choice.Popup(func(ui.Element) {
						components.ScrollView(c, components.ScrollViewOptions{}).Label("模拟场景列表").MaxHeight(240).Children(func() {
							for i, scene := range previewScenes {
								choice.Item(i).Label(scene.label).Children(func() { ui.Text(c, scene.label).SingleLine() })
							}
						})
					})
					if choice.Trigger.Changed() {
						p.loadScene(p.scene)
					}
				})
				button(c, "重置场景").OnClick(func() { p.loadScene(p.scene) })
				p.candidatePicker(c)
				components.Checkbox(c, &p.failNext, "下次操作模拟失败")
				if p.state.speed != nil && (p.state.speed.selected || p.state.speed.current != nil) {
					components.Checkbox(c, &p.speedMissingTiming, "下次专项模拟缺测")
				}
				ui.Text(c, "全部为模拟数据；操作仅改变本窗口，不运行模型或写入文件。").TextColor(c.Theme().TextMuted)
				if p.notice != "" {
					ui.Text(c, p.notice).Role(ui.RoleStatus).TextColor(stateColor(c, stateWarning))
				}
			})
		})
	})
}

// candidatePicker switches the visual scheme and shows the A/B side colors
// through the same paired-bar component the comparison view uses.
func (p *previewSession) candidatePicker(c *ui.Context) {
	ui.Field(c, "视觉候选", func() {
		choice := components.Select(c, &p.candidate)
		choice.Trigger.Label("视觉候选").Children(func() { ui.Text(c, previewCandidates[p.candidate].label).SingleLine() })
		choice.Popup(func(ui.Element) {
			components.ScrollView(c, components.ScrollViewOptions{}).Label("视觉候选列表").MaxHeight(240).Children(func() {
				for i, candidate := range previewCandidates {
					choice.Item(i).Label(candidate.label).Children(func() { ui.Text(c, candidate.label).SingleLine() })
				}
			})
		})
		if choice.Trigger.Changed() {
			p.selectCandidate(p.candidate)
		}
	})
	ui.Text(c, previewCandidates[p.candidate].summary).TextColor(c.Theme().TextMuted)
	ui.Column(c).Gap(tokens(c).Density.Tight).Children(func() {
		ui.Text(c, "A/B 对照侧色").FontSize(tokens(c).Type.Caption).TextColor(c.Theme().TextMuted)
		pairedBar(c, "通过率", pairedBarValue{Label: "A", Value: .72, Text: "72%"}, pairedBarValue{Label: "B", Value: .54, Text: "54%"})
	})
}

func (p *previewSession) content(c *ui.Context) {
	ui.Column(c.Key(p.generation)).Fill().Children(func() { p.state.contentView(c) })
}
func (p *previewSession) showNotice() {
	p.notice = previewNotice
	p.state.failure = &core.ApiError{Code: "preview_unsupported", Message: previewNotice}
}

// selectCandidate switches the preview-only visual scheme without touching
// page data, settings or any service.
func (p *previewSession) selectCandidate(index int) { p.candidate = candidateIndex(index) }
