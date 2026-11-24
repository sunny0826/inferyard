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
	"inferyard/studio/internal/l10n"
)

type previewSession struct {
	ctx                 context.Context
	state               *viewState
	win                 *mygo.Window
	scene               int
	themeMode           int
	appliedThemeMode    int
	languageMode        int
	appliedLanguageMode int
	generation          int
	noticeKey           string
	failNext            bool
	serial              int
	clock               time.Duration
	lastTick            time.Time
	jobs                []previewEvent
	details             map[string]*core.RunTaskDetail
	indexes             map[string]*core.ReportIndex
	reportReadFailed    bool
	speedFailureStage   string
	speedFailure        *core.ApiError
	speedMissingTiming  bool
	local               *previewLocalTask
	polystrataReqs      map[string]core.PolystrataSetupRequest
}

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

func previewThemeModeString(mode int) string {
	switch mode {
	case 1:
		return core.ThemeLight
	case 2:
		return core.ThemeDark
	default:
		return core.ThemeSystem
	}
}

func previewThemeModeIndex(mode string) int {
	switch normalizeThemeMode(mode) {
	case core.ThemeLight:
		return 1
	case core.ThemeDark:
		return 2
	default:
		return 0
	}
}

func (p *previewSession) syncStateTheme() {
	if p.state == nil {
		return
	}
	mode := previewThemeModeString(p.themeMode)
	p.state.themeMode = mode
	p.state.appliedThemeMode = mode
	p.state.settings.Settings.Theme = mode
	p.state.settings.Effective.Theme = mode
}

func (p *previewSession) syncNativeTheme() {
	if p.win == nil || p.appliedThemeMode == p.themeMode {
		return
	}
	p.appliedThemeMode = p.themeMode
	syncNativeTheme(previewThemeModeString(p.themeMode))
}

func previewLanguageModeString(mode int) string {
	switch mode {
	case 1:
		return core.LanguageEn
	case 2:
		return core.LanguageZhHans
	default:
		return core.LanguageSystem
	}
}

func previewLanguageModeIndex(mode string) int {
	switch normalizeLanguageMode(mode) {
	case core.LanguageEn:
		return 1
	case core.LanguageZhHans:
		return 2
	default:
		return 0
	}
}

func (p *previewSession) syncStateLanguage() {
	if p.state == nil {
		return
	}
	mode := previewLanguageModeString(p.languageMode)
	p.state.languageMode = mode
	p.state.appliedLanguageMode = mode
	p.state.settings.Settings.Language = mode
	p.state.settings.Effective.Language = mode
}

func (p *previewSession) syncNativeLanguage() {
	if p.win == nil || p.appliedLanguageMode == p.languageMode {
		return
	}
	p.appliedLanguageMode = p.languageMode
	syncNativeLocale(previewLanguageModeString(p.languageMode))
}

func (p *previewSession) themeFor(c *ui.Context) *ui.Theme {
	p.syncStateTheme()
	p.syncNativeTheme()
	p.syncStateLanguage()
	p.syncNativeLanguage()
	return resolveStudioTheme(c, previewThemeModeString(p.themeMode))
}

func (p *previewSession) shellOptions() sidebar.ShellOptions {
	options := pageShellOptions(l10n.TR("app.title.preview"), "", p.left, p.content)
	options.Theme = p.themeFor
	options.ContentTitle = func(c *ui.Context) {
		ui.Text(c, tr(c, "preview.content_title")).SingleLine().TextColor(stateColor(c, stateWarning))
	}
	return options
}

func (p *previewSession) loadScene(index int) {
	p.scene, p.noticeKey = index, ""
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
	p.syncStateTheme()
	p.syncStateLanguage()
	p.activateScene()
}

func (p *previewSession) left(c *ui.Context) {
	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).FillWidth().Children(func() {
			ui.Column(c.Key(p.generation)).Children(func() { p.state.leftNav(c) })
			ui.Column(c).Padding(tokens(c).Density.Stack).Gap(tokens(c).Density.Row).Children(func() {
				heading(c, tr(c, "preview.sidebar.heading"), 2)
				sceneFieldLabel := tr(c, "preview.scene.label")
				ui.Field(c, sceneFieldLabel, func() {
					choice := components.Select(c, &p.scene)
					currentSceneLabel := tr(c, previewScenes[p.scene].labelKey)
					choice.Trigger.Label(sceneFieldLabel).Value(currentSceneLabel).Children(func() { ui.Text(c, currentSceneLabel).SingleLine() })
					choice.Popup(func(ui.Element) {
						components.ScrollView(c, components.ScrollViewOptions{}).Label(tr(c, "preview.scene.list_aria")).MaxHeight(240).Children(func() {
							for i, scene := range previewScenes {
								label := tr(c, scene.labelKey)
								choice.Item(i).Label(label).Children(func() { ui.Text(c, label).SingleLine() })
							}
						})
					})
					if choice.Trigger.Changed() {
						p.loadScene(p.scene)
					}
				})
				button(c, tr(c, "preview.scene.reset")).OnClick(func() { p.loadScene(p.scene) })
				prevTheme := p.themeMode
				segmentedField(c, tr(c, "preview.theme.label"), "", &p.themeMode, []components.NativeSelectOption[int]{
					{Label: tr(c, "preview.theme.system"), Value: 0},
					{Label: tr(c, "preview.theme.light"), Value: 1},
					{Label: tr(c, "preview.theme.dark"), Value: 2},
				})
				if p.themeMode != prevTheme {
					p.syncStateTheme()
					p.syncNativeTheme()
				}
				prevLang := p.languageMode
				segmentedField(c, tr(c, "preview.language.label"), "", &p.languageMode, []components.NativeSelectOption[int]{
					{Label: tr(c, "preview.language.system"), Value: 0},
					{Label: tr(c, "settings.language.en"), Value: 1},
					{Label: tr(c, "settings.language.zh_hans"), Value: 2},
				})
				if p.languageMode != prevLang {
					p.syncStateLanguage()
					p.syncNativeLanguage()
				}
				components.Checkbox(c, &p.failNext, tr(c, "preview.toggle.fail_next"))
				if p.state.speed != nil && (p.state.speed.selected || p.state.speed.current != nil) {
					components.Checkbox(c, &p.speedMissingTiming, tr(c, "preview.toggle.speed_missing_timing"))
				}
				ui.Text(c, tr(c, "preview.mode_caption")).TextColor(c.Theme().TextMuted)
				if p.noticeKey != "" {
					ui.Text(c, tr(c, p.noticeKey)).Role(ui.RoleStatus).TextColor(stateColor(c, stateWarning))
				}
			})
		})
	})
}

func (p *previewSession) content(c *ui.Context) {
	ui.Column(c.Key(p.generation)).Fill().Children(func() { p.state.contentView(c) })
}
func (p *previewSession) showNotice() {
	p.noticeKey = "preview.notice.unsupported"
	p.state.failure = &core.ApiError{Code: "preview_unsupported", Message: l10n.TR("preview.notice.unsupported")}
}
