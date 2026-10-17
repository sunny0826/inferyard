//go:build ui_debug && !release

package pages

import (
	"context"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/shells/sidebar"
)

const previewNotice = "模拟预览：未执行实际操作。"

type previewSession struct {
	ctx        context.Context
	state      *viewState
	win        *mygo.Window
	scene      int
	generation int
	notice     string
}

// NewPreview opens the real page builders with in-memory fixtures and no Service.
func NewPreview(ctx context.Context) {
	p := newPreviewSession(ctx)
	shell := sidebar.NewShell(p.shellOptions())
	p.win = shell.Window()
	p.state.win = p.win
}

func newPreviewSession(ctx context.Context) *previewSession {
	p := &previewSession{ctx: ctx}
	p.loadScene(1)
	return p
}

func (p *previewSession) shellOptions() sidebar.ShellOptions {
	options := pageShellOptions("InferYard Studio · UI 调试", "", p.left, p.content)
	options.ContentTitle = func(c *ui.Context) {
		ui.Text(c, "UI 调试 · 模拟数据").SingleLine().TextColor(c.Theme().Warning)
	}
	return options
}

func (p *previewSession) loadScene(index int) {
	p.scene, p.notice = index, ""
	p.generation++
	s := previewBase(p.ctx)
	s.preview, s.win = p, p.win
	p.state = s
	previewScenes[index].apply(s)
}

func (p *previewSession) left(c *ui.Context) {
	ui.Column(c).Fill().Children(func() {
		ui.Column(c.Key(p.generation)).Children(func() { p.state.leftNav(c) })
		ui.Column(c).Padding(c.Theme().Space(3)).Gap(c.Theme().Space(2)).Children(func() {
			heading(c, "UI 调试", 2)
			ui.Field(c, "模拟场景", func() {
				choice := components.Select(c, &p.scene)
				choice.Trigger.Label("模拟场景").Children(func() { ui.Text(c, previewScenes[p.scene].label).SingleLine() })
				choice.Popup(func(ui.Element) {
					for i, scene := range previewScenes {
						choice.Item(i).Label(scene.label).Children(func() { ui.Text(c, scene.label).SingleLine() })
					}
				})
				if choice.Trigger.Changed() {
					p.loadScene(p.scene)
				}
			})
			button(c, "重置场景").OnClick(func() { p.loadScene(p.scene) })
			ui.Text(c, "全部为模拟数据，不运行模型或保存设置。").TextColor(c.Theme().TextMuted)
			if p.notice != "" {
				ui.Text(c, p.notice).Role(ui.RoleStatus).TextColor(c.Theme().Warning)
			}
		})
	})
}

func (p *previewSession) content(c *ui.Context) {
	ui.Column(c.Key(p.generation)).Fill().Children(func() { p.state.contentView(c) })
}

func (p *previewSession) showNotice() { p.notice = previewNotice }

func (p *previewSession) resetTask(model, engine string) {
	p.loadScene(0)
	if model != "" && engine != "" {
		p.state.model, p.state.engine = model, engine
	}
}
