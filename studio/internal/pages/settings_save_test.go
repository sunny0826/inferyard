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

func TestStaleRefreshBatchDoesNotRevertSave(t *testing.T) {
	s := settingsState()
	stale := refreshBatch{epoch: s.settingsSequence()}
	stale.settings = core.SettingsResponse{
		Settings:  core.UserSettings{WorkspaceRoot: ptr("/saved/workspace"), ResultsRoot: ptr("/saved/results"), ModelsRoot: ptr("/saved/models"), ModelDownloadName: "saved-model", EngineDownloadName: "saved-engine"},
		Effective: core.EffectiveSettings{WorkspaceRoot: ptr("/saved/workspace")},
	}
	stale.ready.Workspace = core.DirectoryReadiness{Path: ptr("/saved/workspace"), Exists: true, Initialized: true}
	s.ready.Workspace = core.DirectoryReadiness{Path: ptr("/next/workspace"), Exists: true}
	s.workspace, s.modelName = "/next/workspace", "next-model"
	patch := map[string]json.RawMessage{}
	patch["workspace_root"], _ = json.Marshal(s.workspace)
	patch["model_download_name"], _ = json.Marshal(s.modelName)
	s.syncSavedGroups(patch, s.settingsDraft(), core.SettingsResponse{
		Settings:  core.UserSettings{WorkspaceRoot: ptr("/next/workspace"), ResultsRoot: ptr("/saved/results"), ModelsRoot: ptr("/saved/models"), ModelDownloadName: "next-model", EngineDownloadName: "saved-engine"},
		Effective: core.EffectiveSettings{WorkspaceRoot: ptr("/next/workspace")},
	})

	if s.applyRefreshBatch(stale) {
		t.Fatal("stale batch was painted")
	}
	if s.workspace != "/next/workspace" {
		t.Fatalf("stale batch reverted the saved workspace: %q", s.workspace)
	}
	if s.modelName != "next-model" {
		t.Fatalf("stale batch reverted the saved name: %q", s.modelName)
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
		WorkspaceRoot: ptr(filepath.Join(root, "workspace")), ResultsRoot: ptr(filepath.Join(root, "results")),
		ModelsRoot: ptr(filepath.Join(root, "models")), ModelDownloadName: "saved-model", EngineDownloadName: "saved-engine",
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
	s.loadPathSettings(s.settings)
	s.loadDownloadNames(s.settings)
	s.settingsLoaded = true
	workspace := filepath.Join(root, "draft-workspace")
	s.workspace, s.modelName = workspace, "draft-model"

	patch := map[string]json.RawMessage{}
	patch["workspace_root"], _ = json.Marshal(workspace)
	if _, e := s.persistSettings(patch, s.settingsDraft(), "设置已保存。"); e != nil {
		t.Fatal(e)
	}
	if s.workspace != workspace {
		t.Fatalf("saved path not shown: %q", s.workspace)
	}
	if s.modelName != "draft-model" {
		t.Fatalf("path save wiped the download-name draft: %q", s.modelName)
	}

	next := filepath.Join(root, "later-workspace")
	s.workspace = next
	names := map[string]json.RawMessage{}
	names["model_download_name"], _ = json.Marshal("saved-again")
	if _, e := s.persistSettings(names, s.settingsDraft(), "下载目录名已保存。"); e != nil {
		t.Fatal(e)
	}
	if s.modelName != "saved-again" {
		t.Fatalf("saved name not shown: %q", s.modelName)
	}
	if s.workspace != next {
		t.Fatalf("name save wiped the path draft: %q", s.workspace)
	}
}

func TestSettingsSavePreservesTypingDuringSave(t *testing.T) {
	s := settingsState()
	s.workspace, s.results = "/submitted/workspace", "/submitted/results"
	submitted := s.settingsDraft()
	s.workspace = "/typed/during-save"
	patch := map[string]json.RawMessage{}
	patch["workspace_root"], _ = json.Marshal(submitted.workspace)
	patch["results_root"], _ = json.Marshal(submitted.results)
	s.syncSavedGroups(patch, submitted, core.SettingsResponse{
		Settings:  core.UserSettings{WorkspaceRoot: ptr(submitted.workspace), ResultsRoot: ptr(submitted.results)},
		Effective: core.EffectiveSettings{WorkspaceRoot: ptr(submitted.workspace), ResultsRoot: ptr(submitted.results)},
	})
	if s.workspace != "/typed/during-save" {
		t.Fatalf("post-submit typing was overwritten: %q", s.workspace)
	}
	if s.results != "/submitted/results" {
		t.Fatalf("untouched field was not synced: %q", s.results)
	}
	if !s.pathsEdited() || !s.canSavePaths() {
		t.Fatal("newer draft did not keep the card dirty and Save enabled")
	}

	names := settingsState()
	names.modelName, names.engineName = "submitted-model", "submitted-engine"
	submitted = names.settingsDraft()
	names.modelName = "typed-model"
	namePatch := map[string]json.RawMessage{}
	namePatch["model_download_name"], _ = json.Marshal(submitted.modelName)
	namePatch["engine_download_name"], _ = json.Marshal(submitted.engineName)
	names.syncSavedGroups(namePatch, submitted, core.SettingsResponse{Settings: core.UserSettings{
		ModelDownloadName: submitted.modelName, EngineDownloadName: submitted.engineName,
	}})
	if names.modelName != "typed-model" {
		t.Fatalf("post-submit name typing was overwritten: %q", names.modelName)
	}
	if names.engineName != "submitted-engine" {
		t.Fatalf("untouched name was not synced: %q", names.engineName)
	}
	if !names.canSaveDownloadNames() {
		t.Fatal("newer name draft did not keep Save enabled")
	}
}
