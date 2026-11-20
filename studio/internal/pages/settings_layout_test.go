package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"inferyard/studio/internal/core"
)

// settingsState mounts the Settings page with saved paths and download names
// distinct from the form defaults, so "dirty" is meaningful.
func settingsState() *viewState {
	s := testState()
	s.selected = "settings"
	s.settings = core.SettingsResponse{
		Settings: core.UserSettings{
			WorkspaceRoot: ptr("/saved/workspace"), ResultsRoot: ptr("/saved/results"),
			ModelsRoot: ptr("/saved/models"), ModelDownloadName: "saved-model", EngineDownloadName: "saved-engine",
		},
		Effective: core.EffectiveSettings{WorkspaceRoot: ptr("/saved/workspace")},
	}
	s.loadSettings(s.settings)
	s.settingsLoaded = true
	return &s
}

func settingsTester(s *viewState) *ui.Tester {
	return ui.NewTester(s.settingsView, 1120, 1800)
}

// The path card only offers a save after an edit; an unchanged or still-loading
// form must not enable it.
func TestSettingsPathSaveRequiresEdit(t *testing.T) {
	s := settingsState()
	if s.pathsEdited() || s.canSavePaths() {
		t.Fatal("unchanged settings offered a path save")
	}
	s.workspace = "/new/workspace"
	if !s.pathsEdited() || !s.canSavePaths() {
		t.Fatal("edited path did not enable the save")
	}
	s.busy = true
	if s.canSavePaths() {
		t.Fatal("busy state kept the path save enabled")
	}
	s.busy, s.settingsLoaded = false, false
	if s.canSavePaths() {
		t.Fatal("unloaded settings kept the path save enabled")
	}
}

// The download-name save follows its own edit and validity, not the path form.
func TestSettingsDownloadSaveRequiresEditAndValidNames(t *testing.T) {
	s := settingsState()
	if s.downloadNamesEdited() || s.canSaveDownloadNames() {
		t.Fatal("unchanged names offered a save")
	}
	s.modelName = "other-model"
	if !s.downloadNamesEdited() || !s.canSaveDownloadNames() {
		t.Fatal("edited name did not enable the save")
	}
	s.engineName = "..."
	if s.canSaveDownloadNames() {
		t.Fatal("invalid name kept the save enabled")
	}
	s.engineName = "saved-engine"
	if !s.canSaveDownloadNames() {
		t.Fatal("restored valid name did not re-enable the save")
	}
	s.busy = true
	if s.canSaveDownloadNames() {
		t.Fatal("busy state kept the download save enabled")
	}
	s.busy, s.settingsLoaded = false, false
	if s.canSaveDownloadNames() {
		t.Fatal("unloaded settings kept the download save enabled")
	}
}

// Reverting to the defaults only fills the form; the card must say that the
// defaults are not persisted yet, and the save becomes available.
func TestSettingsRevertDefaultsShowsUnsavedNotice(t *testing.T) {
	s := settingsState()
	tt := settingsTester(s)
	tt.Frame()
	if tt.HasText("已填入默认值，请点击保存生效。") {
		t.Fatalf("notice shown before the revert: %v", tt.Texts())
	}
	click(t, tt, "恢复默认值")
	tt.Frame()
	if !tt.HasText("已填入默认值，请点击保存生效。") {
		t.Fatalf("revert did not explain that saving is still required: %v", tt.Texts())
	}
	if !s.canSaveDownloadNames() {
		t.Fatal("revert left the download-name save disabled")
	}
	if s.settings.Settings.ModelDownloadName != "saved-model" {
		t.Fatal("revert silently persisted the default")
	}
}

// Editing only the download names must not claim that the paths changed, and
// the reverse holds for path edits; each card reports its own draft.
func TestSettingsNoticesFollowTheirOwnCard(t *testing.T) {
	s := settingsState()
	s.modelName = "renamed-model"
	tt := settingsTester(s)
	tt.Frame()
	if tt.HasText("路径有未保存的修改，点击保存后生效。") {
		t.Fatalf("path notice followed a download-name edit: %v", tt.Texts())
	}
	if !tt.HasText("下载目录名已修改，请点击保存生效。") {
		t.Fatalf("download-name edit had no unsaved notice: %v", tt.Texts())
	}
	s = settingsState()
	s.results = "/other/results"
	tt = settingsTester(s)
	tt.Frame()
	if !tt.HasText("路径有未保存的修改，点击保存后生效。") {
		t.Fatalf("path edit had no unsaved notice: %v", tt.Texts())
	}
	if tt.HasText("下载目录名已修改，请点击保存生效。") || tt.HasText("已填入默认值，请点击保存生效。") {
		t.Fatalf("download-name notice followed a path edit: %v", tt.Texts())
	}
	if !tt.HasText("保存") || !tt.HasText("保存下载目录名") {
		t.Fatalf("settings lost a save action: %v", tt.Texts())
	}
}

// Both engine actions sit on one row, and the card explains where the
// installation declaration comes from.
func TestLocalInstallationsActionsShareOneRow(t *testing.T) {
	s := settingsState()
	tt := settingsTester(s)
	tt.Frame()
	register, ok := tt.Find("注册引擎")
	if !ok {
		t.Fatalf("register action missing: %v", tt.Texts())
	}
	recheck, ok := tt.Find("重新核验引擎")
	if !ok {
		t.Fatalf("recheck action missing: %v", tt.Texts())
	}
	if register.Y != recheck.Y {
		t.Fatalf("engine actions stack instead of sharing a row: %+v / %+v", register, recheck)
	}
	if recheck.X <= register.X {
		t.Fatalf("recheck action does not follow register: %+v / %+v", register, recheck)
	}
	if !tt.HasText("installation.json") || !tt.HasText("不生成该文件") {
		t.Fatalf("missing installation.json hint: %v", tt.Texts())
	}
}

// Dismissing an inline stop confirmation leaves the engine running and clears
// the prompt, so a later click starts over instead of acting.
func TestStopEngineConfirmDismissKeepsRunning(t *testing.T) {
	s := engineStopState()
	stops := 0
	s.stopEngineAPI = func() (core.EngineState, *core.ApiError) {
		stops++
		return s.engineState, nil
	}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.serviceView(c) }) }, 1120, 1200)
	click(t, tt, "停止服务")
	tt.Frame()
	if !tt.HasText("保持服务运行") {
		t.Fatalf("stop prompt lacks dismiss copy: %v", tt.Texts())
	}
	if tt.HasText("返回") {
		t.Fatalf("stop prompt still shows 返回: %v", tt.Texts())
	}
	click(t, tt, "保持服务运行")
	tt.Frame()
	if stops != 0 {
		t.Fatalf("dismissal stopped the engine %d times", stops)
	}
	if s.pendingDestructive != (destructiveRequest{}) {
		t.Fatalf("dismissal kept the prompt armed: %+v", s.pendingDestructive)
	}
	if !tt.HasText("停止服务") || tt.HasText("确认停止服务") {
		t.Fatalf("dismissal did not close the prompt: %v", tt.Texts())
	}
}

// A destructive confirmation never says "返回": nothing navigated anywhere, so
// the dismiss copy names the action it leaves alone.
func TestDestructiveConfirmationDismissCopy(t *testing.T) {
	s := testState()
	confirm := func(option destructiveOption) *ui.Tester {
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Column(c).Children(func() { s.destructiveAction(c, option) })
		}, 900, 600)
		click(t, tt, option.label)
		tt.Frame()
		return tt
	}
	for _, scenario := range []struct {
		name   string
		option destructiveOption
		want   string
	}{
		{"run", destructiveOption{kind: destructiveCancelRun, id: "t1", label: "取消运行", confirmLabel: "确认取消运行", dismissLabel: "保持运行"}, "保持运行"},
		{"default", destructiveOption{kind: destructiveCancelDownload, id: "d1", label: "取消下载", confirmLabel: "确认取消下载"}, "暂不操作"},
	} {
		tt := confirm(scenario.option)
		if !tt.HasText(scenario.want) {
			t.Errorf("%s confirmation lacks dismiss copy %q: %v", scenario.name, scenario.want, tt.Texts())
		}
		if tt.HasText("返回") {
			t.Errorf("%s confirmation still shows 返回: %v", scenario.name, tt.Texts())
		}
	}
}

// The gate builders pass their own dismiss copy through to the prompt.
func TestDestructiveGatesUseContextDismissCopy(t *testing.T) {
	if got := destructiveDismissLabel(destructiveOption{}); got != "暂不操作" {
		t.Fatalf("default dismiss label = %q", got)
	}
	if got := destructiveDismissLabel(destructiveOption{dismissLabel: "保持运行"}); got != "保持运行" {
		t.Fatalf("explicit dismiss label = %q", got)
	}
}
