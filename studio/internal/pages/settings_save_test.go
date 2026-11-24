package pages

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func settingsTestService(t *testing.T, root string) *core.Service {
	t.Helper()
	settings := filepath.Join(root, "settings.json")
	raw, _ := json.Marshal(core.UserSettings{WorkspaceRoot: ptr(filepath.Join(root, "workspace"))})
	if err := os.WriteFile(settings, raw, 0600); err != nil {
		t.Fatal(err)
	}
	service, e := core.New(context.Background(), core.Options{SettingsPath: settings, Execute: func(context.Context, string, ...string) (inferyard.Execution, error) {
		return inferyard.Execution{Stdout: []byte(`{"command":"probe","status":"ok"}`)}, nil
	}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = service.Shutdown(context.Background()) })
	return service
}

func TestSettingsViewExposesOnlyWorkspaceDirectory(t *testing.T) {
	s := settingsState()
	tt := settingsTester(s)
	if _, ok := tt.Find("工作区目录"); !ok {
		t.Fatal("settings page must show 工作区目录")
	}
	for _, removed := range []string{"路径设置", "结果目录", "模型目录", "下载目录名", "模型下载目录名", "引擎下载目录名"} {
		if _, ok := tt.Find(removed); ok {
			t.Fatalf("settings page must not show removed UI text %q", removed)
		}
	}
}

func TestStaleRefreshBatchDoesNotRevertSave(t *testing.T) {
	s := settingsState()
	stale := refreshBatch{epoch: s.settingsSequence()}
	stale.settings = core.SettingsResponse{
		Settings:  core.UserSettings{WorkspaceRoot: ptr("/saved/workspace")},
		Effective: core.EffectiveSettings{WorkspaceRoot: ptr("/saved/workspace")},
	}
	stale.ready.Workspace = core.DirectoryReadiness{Path: ptr("/saved/workspace"), Exists: true, Initialized: true}
	s.ready.Workspace = core.DirectoryReadiness{Path: ptr("/next/workspace"), Exists: true}
	s.workspace, s.downloadProxy = "/next/workspace", "http://127.0.0.1:7890"
	patch := map[string]json.RawMessage{}
	patch["workspace_root"], _ = json.Marshal(s.workspace)
	patch["download_proxy"], _ = json.Marshal(s.downloadProxy)
	s.syncSavedGroups(patch, s.settingsDraft(), core.SettingsResponse{
		Settings:  core.UserSettings{WorkspaceRoot: ptr("/next/workspace"), DownloadProxy: ptr("http://127.0.0.1:7890")},
		Effective: core.EffectiveSettings{WorkspaceRoot: ptr("/next/workspace"), DownloadProxy: ptr("http://127.0.0.1:7890")},
	})

	if s.applyRefreshBatch(stale) {
		t.Fatal("stale batch was painted")
	}
	if s.workspace != "/next/workspace" {
		t.Fatalf("stale batch reverted the saved workspace: %q", s.workspace)
	}
	if s.downloadProxy != "http://127.0.0.1:7890" {
		t.Fatalf("stale batch reverted the saved proxy: %q", s.downloadProxy)
	}
	if root := emptyText(s.settings.Effective.WorkspaceRoot); root != "/next/workspace" {
		t.Fatalf("stale batch reverted the baseline: %q", root)
	}
	if got := emptyText(s.ready.Workspace.Path); got != "/next/workspace" {
		t.Fatalf("stale batch painted the old directory readiness: %q", got)
	}

	fresh := refreshBatch{epoch: s.settingsSequence()}
	fresh.settings = core.SettingsResponse{
		Settings:  core.UserSettings{WorkspaceRoot: ptr("/later/workspace")},
		Effective: core.EffectiveSettings{WorkspaceRoot: ptr("/later/workspace")},
	}
	fresh.ready.Workspace = core.DirectoryReadiness{Path: ptr("/later/workspace"), Exists: true}
	s.service = settingsTestService(t, t.TempDir())
	s.ctx = context.Background()
	s.discoveryLoaded = true
	if !s.applyRefreshBatch(fresh) {
		t.Fatal("fresh batch was dropped")
	}
	if s.workspace != "/later/workspace" {
		t.Fatalf("fresh batch did not apply: %q", s.workspace)
	}
	if emptyText(s.ready.Workspace.Path) != "/later/workspace" {
		t.Fatalf("fresh readiness did not apply: %+v", s.ready.Workspace)
	}
}

func TestSettingsSaveKeepsOtherGroupDraft(t *testing.T) {
	root := t.TempDir()
	settings := filepath.Join(root, "settings.json")
	raw, _ := json.Marshal(core.UserSettings{
		WorkspaceRoot: ptr(filepath.Join(root, "workspace")),
	})
	if err := os.WriteFile(settings, raw, 0600); err != nil {
		t.Fatal(err)
	}
	service, e := core.New(context.Background(), core.Options{SettingsPath: settings, Execute: func(context.Context, string, ...string) (inferyard.Execution, error) {
		return inferyard.Execution{Stdout: []byte(`{"command":"probe","status":"ok"}`)}, nil
	}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = service.Shutdown(context.Background()) })

	s := settingsState()
	s.service = service
	s.settings = service.Settings()
	s.loadWorkspaceSettings(s.settings)
	s.loadProxySettings(s.settings)
	s.settingsLoaded = true
	workspace := filepath.Join(root, "draft-workspace")
	s.workspace, s.downloadProxy = workspace, "http://127.0.0.1:7890"

	patch := map[string]json.RawMessage{}
	patch["workspace_root"], _ = json.Marshal(workspace)
	if _, e := s.persistSettings(patch, s.settingsDraft(), "工作区目录已保存"); e != nil {
		t.Fatal(e)
	}
	if s.workspace != workspace {
		t.Fatalf("saved workspace not shown: %q", s.workspace)
	}
	if s.downloadProxy != "http://127.0.0.1:7890" {
		t.Fatalf("workspace save wiped the proxy draft: %q", s.downloadProxy)
	}

	next := filepath.Join(root, "later-workspace")
	s.workspace = next
	proxyPatch := map[string]json.RawMessage{}
	proxyPatch["download_proxy"], _ = json.Marshal("http://127.0.0.1:7890")
	if _, e := s.persistSettings(proxyPatch, s.settingsDraft(), "代理设置已保存"); e != nil {
		t.Fatal(e)
	}
	if s.downloadProxy != "http://127.0.0.1:7890" {
		t.Fatalf("saved proxy not shown: %q", s.downloadProxy)
	}
	if s.workspace != next {
		t.Fatalf("proxy save wiped the workspace draft: %q", s.workspace)
	}
}

func TestSettingsSavePreservesTypingDuringSave(t *testing.T) {
	s := settingsState()
	s.workspace = "/submitted/workspace"
	submitted := s.settingsDraft()
	s.workspace = "/typed/during-save"
	patch := map[string]json.RawMessage{}
	patch["workspace_root"], _ = json.Marshal(submitted.workspace)
	s.syncSavedGroups(patch, submitted, core.SettingsResponse{
		Settings:  core.UserSettings{WorkspaceRoot: ptr(submitted.workspace)},
		Effective: core.EffectiveSettings{WorkspaceRoot: ptr(submitted.workspace)},
	})
	if s.workspace != "/typed/during-save" {
		t.Fatalf("post-submit typing was overwritten: %q", s.workspace)
	}
	if !s.workspaceEdited() || !s.canSaveWorkspace() {
		t.Fatal("newer draft did not keep the card dirty and Save enabled")
	}
}

func TestFirstRunDefaultWorkspaceCanSaveAndWorkspaceIsolation(t *testing.T) {
	// Finding 2: First-run default workspace (Settings.WorkspaceRoot == nil) can be saved without false unsaved dirty state.
	s := testState()
	s.selected = "settings"
	defaultWS := filepath.Join(t.TempDir(), "default-ws")
	s.settings = core.SettingsResponse{
		Settings:  core.UserSettings{},
		Effective: core.EffectiveSettings{WorkspaceRoot: ptr(defaultWS)},
	}
	s.loadSettings(s.settings)
	s.settingsLoaded = true
	if s.workspaceEdited() || s.settingsUnsaved() {
		t.Fatal("first-run prefilled default workspace must not mark settings dirty/unsaved")
	}
	if !s.canSaveWorkspace() {
		t.Fatal("first-run prefilled default workspace must allow saving")
	}

	// Finding 7: When Effective.ModelsRoot is empty, index-0 extra model root must still be removable.
	if !s.isExtraModelRootDir("/extra/models", "") {
		t.Fatal("isExtraModelRootDir must treat extra root as removable when primaryRoot is empty")
	}

	// Finding 4: Active download in wsA must not mark wsB catalog/retry buttons as downloading.
	wsA := filepath.Join(t.TempDir(), "ws-a")
	wsB := filepath.Join(t.TempDir(), "ws-b")
	s.settings.Effective.WorkspaceRoot = ptr(wsB)
	s.settings.Effective.ModelsRoot = ptr(filepath.Join(wsB, "models"))
	src := "https://huggingface.co/o/r/blob/0123456789012345678901234567890123456789/model.gguf"
	s.downloads = []core.DownloadTask{
		{TaskId: "dl-a", Kind: "model", State: "downloading", SourceUrl: src, OutDir: filepath.Join(wsA, "models", "model")},
		{TaskId: "dl-b-fail", Kind: "model", State: "failed", SourceUrl: src, OutDir: filepath.Join(wsB, "models", "model")},
	}
	if s.isModelEntryDownloading(core.CatalogEntry{URL: src}) {
		t.Fatal("wsA active model download disabled wsB model entry")
	}
	if s.downloadRetryActive(s.downloads[1]) {
		t.Fatal("wsA active model download blocked wsB retry")
	}

	// Finding 1: Configured existing empty dir initializes in place without rewriting workspace_root.
	existingWS := t.TempDir()
	var gotInitCmd string
	initialized := false
	err := prepareEnvironmentSteps(
		context.Background(),
		func() core.Readiness {
			return core.Readiness{
				CLI:       core.InferyardProbe{Status: "ok"},
				Workspace: core.DirectoryReadiness{Path: ptr(existingWS), Configured: true, Exists: true, Initialized: initialized},
				Results:   core.DirectoryReadiness{Path: ptr(filepath.Join(existingWS, "results")), Configured: true, Exists: initialized},
				Models:    core.DirectoryReadiness{Path: ptr(filepath.Join(existingWS, "models")), Configured: true, Exists: initialized},
			}
		},
		nil,
		func() *core.ApiError { return nil },
		func(_ context.Context, req core.WizardRequest) (core.WizardResult, *core.ApiError) {
			gotInitCmd = req.Command
			initialized = true
			return core.WizardResult{ExitCode: 0}, nil
		},
	)
	if err != nil {
		t.Fatalf("expected existing empty workspace to initialize in place, got %+v", err)
	}
	if gotInitCmd != commandOf([]string{"init", "--out", existingWS}) {
		t.Fatalf("prepareEnvironmentSteps rewrote configured workspace: %q", gotInitCmd)
	}
}
