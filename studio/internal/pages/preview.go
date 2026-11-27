//go:build ui_debug && !release

package pages

import (
	"context"
	"os"
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
	accentMode          string
	themeMode           int
	appliedThemeMode    int
	languageMode        int
	appliedLanguageMode int
	lastLocale          string
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
	dsInput             string
	dsSwitch            bool
	dsCheck             bool
}

func NewPreview(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	p := newPreviewSession(ctx)
	shell := sidebar.NewShell(p.shellOptions())
	p.win = shell.Window()
	p.state.win = p.win
	offHide := p.win.OnHide(func() {
		if p.state != nil && p.state.openedReport != nil {
			p.state.openedReport.releaseSVGStage()
		}
	})
	offShow := p.win.OnShow(func() {
		if p.state != nil && p.state.selected == "reports" && p.state.openedReport != nil {
			p.win.Invalidate()
		}
	})
	offClose := p.win.OnClose(func(e *mygo.CloseEvent) {
		if p.state != nil {
			p.state.handleWindowClose(e)
		}
	})
	p.win.OnClosed(func() {
		offHide()
		offShow()
		offClose()
		setActiveAccent(core.AccentCobalt)
		if p.state != nil {
			p.state.closeOpenedReport()
		}
		cancel()
	})
	go p.runClock()
}

func newPreviewSession(ctx context.Context) *previewSession {
	p := &previewSession{
		ctx:        ctx,
		accentMode: core.AccentCobalt,
		dsInput:    "http://127.0.0.1:48857",
		dsSwitch:   true,
		dsCheck:    true,
	}
	if os.Getenv("INFERYARD_DEBUG_SCENE") == "design_system" {
		p.openDesignSystemScene()
		return p
	}
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

func (p *previewSession) syncStateAccent() {
	accent := core.NormalizeAccent(p.accentMode)
	p.accentMode = accent
	setActiveAccent(accent)
	if p.state == nil {
		return
	}
	p.state.accentMode = accent
	p.state.settings.Settings.Accent = accent
	p.state.settings.Effective.Accent = accent
}

func (p *previewSession) selectAccent(accent string) {
	p.accentMode = core.NormalizeAccent(accent)
	p.syncStateAccent()
}

func (p *previewSession) themeFor(c *ui.Context) *ui.Theme {
	p.syncStateAccent()
	p.syncStateTheme()
	p.syncNativeTheme()
	p.syncStateLanguage()
	p.syncNativeLanguage()
	if loc := contextLocale(c); loc != "" && loc != p.lastLocale {
		p.lastLocale = loc
		p.reapplyPreviewLocale(loc)
	}
	return resolveStudioTheme(c, previewThemeModeString(p.themeMode))
}

func (p *previewSession) shellOptions() sidebar.ShellOptions {
	options := pageShellOptions(l10n.TR("app.title.preview"), "", p.left, p.content)
	options.Theme = p.themeFor
	options.ContentTitle = func(c *ui.Context) {
		p.state.shellContentTitle(c, func(c *ui.Context) {
			ui.Text(c, tr(c, "preview.content_title")).SingleLine().TextColor(stateColor(c, stateWarning))
		})
	}
	return options
}

func (p *previewSession) loadScene(index int) {
	if p.state != nil {
		p.state.closeOpenedReport()
	}
	p.scene, p.noticeKey, p.lastLocale = index, "", ""
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
	p.syncStateAccent()
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
					choice := studioSelect(c, &p.scene)
					currentSceneLabel := tr(c, previewScenes[p.scene].labelKey)
					choice.Trigger.Label(sceneFieldLabel).Value(currentSceneLabel).Children(func() { ui.Text(c, currentSceneLabel).SingleLine() })
					choice.Popup(func(ui.Element) {
						components.ScrollView(c, components.ScrollViewOptions{}).Label(tr(c, "preview.scene.list_aria")).MaxHeight(280).Children(func() {
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
				paletteFieldLabel := tr(c, "preview.palette.label")
				accentIdx := 0
				for i, spec := range studioAccentSpecs {
					if spec.id == core.NormalizeAccent(p.accentMode) {
						accentIdx = i
						break
					}
				}
				ui.Field(c, paletteFieldLabel, func() {
					choice := studioSelect(c, &accentIdx)
					currentPaletteLabel := tr(c, studioAccentSpecs[accentIdx].labelKey)
					choice.Trigger.Label(paletteFieldLabel).Value(currentPaletteLabel).Children(func() { ui.Text(c, currentPaletteLabel).SingleLine() })
					choice.Popup(func(ui.Element) {
						components.ScrollView(c, components.ScrollViewOptions{}).Label(tr(c, "preview.palette.list_aria")).MaxHeight(220).Children(func() {
							for i, spec := range studioAccentSpecs {
								label := tr(c, spec.labelKey)
								choice.Item(i).Label(label).Children(func() { ui.Text(c, label).SingleLine() })
							}
						})
					})
					if choice.Trigger.Changed() && accentIdx >= 0 && accentIdx < len(studioAccentSpecs) {
						p.selectAccent(studioAccentSpecs[accentIdx].id)
					}
				})
				if button(c, tr(c, "preview.ds.open_button")).Clicked() {
					p.openDesignSystemScene()
				}
				prevTheme := p.themeMode
				previewCompactSegmentedField(c, tr(c, "preview.theme.label"), &p.themeMode, []components.NativeSelectOption[int]{
					{Label: tr(c, "preview.theme.system"), Value: 0},
					{Label: tr(c, "preview.theme.light"), Value: 1},
					{Label: tr(c, "preview.theme.dark"), Value: 2},
				})
				if p.themeMode != prevTheme {
					p.syncStateTheme()
					p.syncNativeTheme()
				}
				prevLang := p.languageMode
				previewCompactSegmentedField(c, tr(c, "preview.language.label"), &p.languageMode, []components.NativeSelectOption[int]{
					{Label: tr(c, "preview.language.system"), Value: 0},
					{Label: tr(c, "settings.language.en"), Value: 1},
					{Label: tr(c, "settings.language.zh_hans"), Value: 2},
				})
				if p.languageMode != prevLang {
					p.syncStateLanguage()
					p.syncNativeLanguage()
					p.reapplyPreviewLocale(l10n.LocaleForSetting(previewLanguageModeString(p.languageMode)))
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
	if p.state != nil && p.state.selected == "design_system" {
		ui.Column(c.Key(p.generation)).Fill().Children(func() { p.designSystemView(c) })
		return
	}
	ui.Column(c.Key(p.generation)).Fill().Children(func() { p.state.contentView(c) })
}
func (p *previewSession) showNotice() {
	p.noticeKey = "preview.notice.unsupported"
	p.state.failure = &core.ApiError{Code: "preview_unsupported", Message: l10n.TR("preview.notice.unsupported")}
}

func previewCompactSegmentedField[T comparable](c *ui.Context, name string, selected *T, options []components.NativeSelectOption[T]) {
	if len(options) == 0 {
		return
	}
	t := tokens(c)
	idx := 0
	for i, option := range options {
		if option.Value == *selected {
			idx = i
			break
		}
	}
	ui.Field(c, name, func() {
		seg := ui.SegmentedBase(c, &idx, len(options))
		trackR := t.Radius.Floating
		trackPad := t.Density.Tight
		itemR := concentricRadius(trackR, trackPad)
		seg.Track.FillWidth().AlignItems(ui.Center).Gap(2).
			Padding(trackPad).Radius(trackR).
			Background(t.Color.Sunken).Border(1, t.Color.BorderSubtle).Label(name).
			Children(func() {
				for i, option := range options {
					active := i == idx
					item := seg.Segment(i).Grow(1).MinWidth(0).
						AlignItems(ui.Center).Justify(ui.Center).
						Padding(t.Density.Tight, t.Density.Tight).
						Radius(itemR).Label(option.Label).Disabled(option.Disabled)
					if active {
						applyShadow(item.Background(t.Color.Surface).Border(1, t.Color.BorderSubtle), t.Elevation.XS)
					} else if item.Hovered() && !option.Disabled {
						item.Background(t.Color.Surface.Alpha(.6))
					}
					item.Children(func() {
						label := ui.Text(c, option.Label).FontSize(t.Type.Caption).SingleLine()
						if active {
							label.Bold().TextColor(t.Color.FgPrimary)
						} else {
							label.TextColor(t.Color.FgSecondary)
						}
					})
				}
			})
		if seg.Track.Changed() && idx >= 0 && idx < len(options) && !options[idx].Disabled {
			*selected = options[idx].Value
		}
	})
}
