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

// The two Settings cards save independently. A successful response must sync
// only the fields the patch wrote, so the other card keeps its draft.

// Invalid path drafts keep the save disabled instead of failing on click.
func TestSettingsPathSaveRejectsInvalidDraft(t *testing.T) {
	for name, apply := range map[string]func(*viewState){
		"relative_workspace": func(s *viewState) { s.workspace = "relative/path" },
		"cleared_workspace":  func(s *viewState) { s.workspace = "" },
		"cleared_results":    func(s *viewState) { s.results = "" },
		"cleared_models":     func(s *viewState) { s.models = "" },
	} {
		s := settingsState()
		apply(s)
		if !s.pathsEdited() {
			t.Errorf("%s: draft did not register as an edit", name)
			continue
		}
		if s.pathsValid() || s.canSavePaths() {
			t.Errorf("%s: invalid draft enabled the path save", name)
		}
	}
	s := settingsState()
	s.workspace = "/valid/new"
	if !s.pathsValid() || !s.canSavePaths() {
		t.Fatal("valid draft did not enable the save")
	}
}

// settingsPatchGroups tells which card a patch belongs to.
func TestSettingsPatchGroupsSplitTheTwoCards(t *testing.T) {
	paths, names := settingsPatchGroups(map[string]json.RawMessage{"workspace_root": json.RawMessage(`"/w"`)})
	if !paths || names {
		t.Fatalf("path-only patch: paths=%v names=%v", paths, names)
	}
	paths, names = settingsPatchGroups(map[string]json.RawMessage{"model_download_name": json.RawMessage(`"m"`)})
	if paths || !names {
		t.Fatalf("name-only patch: paths=%v names=%v", paths, names)
	}
}

// syncSavedGroups keeps the untouched card's draft in both directions.
func TestSyncSavedGroupsKeepsOtherDraft(t *testing.T) {
	respond := core.SettingsResponse{
		Settings: core.UserSettings{
			WorkspaceRoot: ptr("/saved/workspace"), ResultsRoot: ptr("/saved/results"), ModelsRoot: ptr("/saved/models"),
			ModelDownloadName: "saved-model", EngineDownloadName: "saved-engine",
		},
		Effective: core.EffectiveSettings{WorkspaceRoot: ptr("/saved/workspace")},
	}

	paths := settingsState()
	paths.workspace, paths.modelName = "/draft/workspace", "draft-model"
	paths.syncSavedGroups(map[string]json.RawMessage{"workspace_root": json.RawMessage(`"/draft/workspace"`)}, paths.settingsDraft(), respond)
	if paths.workspace != "/saved/workspace" {
		t.Fatalf("saved path not synced: %q", paths.workspace)
	}
	if paths.modelName != "draft-model" {
		t.Fatalf("path save wiped the download-name draft: %q", paths.modelName)
	}

	names := settingsState()
	names.workspace, names.modelName = "/draft/workspace", "draft-model"
	names.syncSavedGroups(map[string]json.RawMessage{"model_download_name": json.RawMessage(`"saved-model"`)}, names.settingsDraft(), respond)
	if names.modelName != "saved-model" {
		t.Fatalf("saved name not synced: %q", names.modelName)
	}
	if names.workspace != "/draft/workspace" {
		t.Fatalf("name save wiped the path draft: %q", names.workspace)
	}
}

// applySettingsSnapshot drops a refresh snapshot read before a save landed, so
// the older values cannot revert the newly saved workspace and names.
func TestStaleRefreshSnapshotDoesNotRevertSave(t *testing.T) {
	s := settingsState()
	// A refresh reads the old values, then a save applies while it is in flight.
	staleEpoch := s.settingsSequence()
	stale := core.SettingsResponse{
		Settings:  core.UserSettings{WorkspaceRoot: ptr("/saved/workspace"), ResultsRoot: ptr("/saved/results"), ModelsRoot: ptr("/saved/models"), ModelDownloadName: "saved-model", EngineDownloadName: "saved-engine"},
		Effective: core.EffectiveSettings{WorkspaceRoot: ptr("/saved/workspace")},
	}
	s.workspace, s.modelName = "/next/workspace", "next-model"
	patch := map[string]json.RawMessage{}
	patch["workspace_root"], _ = json.Marshal(s.workspace)
	patch["model_download_name"], _ = json.Marshal(s.modelName)
	s.syncSavedGroups(patch, s.settingsDraft(), core.SettingsResponse{
		Settings:  core.UserSettings{WorkspaceRoot: ptr("/next/workspace"), ResultsRoot: ptr("/saved/results"), ModelsRoot: ptr("/saved/models"), ModelDownloadName: "next-model", EngineDownloadName: "saved-engine"},
		Effective: core.EffectiveSettings{WorkspaceRoot: ptr("/next/workspace")},
	})

	s.applySettingsSnapshot(staleEpoch, stale)
	if s.workspace != "/next/workspace" {
		t.Fatalf("stale snapshot reverted the saved workspace: %q", s.workspace)
	}
	if s.modelName != "next-model" {
		t.Fatalf("stale snapshot reverted the saved name: %q", s.modelName)
	}
	if root := emptyText(s.settings.Effective.WorkspaceRoot); root != "/next/workspace" {
		t.Fatalf("stale snapshot reverted the baseline: %q", root)
	}

	// A snapshot taken after the save still applies, as the follow-up refresh does.
	fresh := s.settingsSequence()
	s.applySettingsSnapshot(fresh, core.SettingsResponse{
		Settings:  core.UserSettings{WorkspaceRoot: ptr("/later/workspace")},
		Effective: core.EffectiveSettings{WorkspaceRoot: ptr("/later/workspace")},
	})
	if s.workspace != "/later/workspace" {
		t.Fatalf("fresh snapshot was dropped: %q", s.workspace)
	}
}

// persistSettings is the production save boundary; both groups keep the other
// card's draft after the real UpdateSettings round trip.
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

// Saving updates the baseline as well as the form: immediately after success
// the card is no longer dirty, so the Save button disables without waiting for
// a refresh.
func TestSettingsSaveClearsDirtyImmediately(t *testing.T) {
	s := settingsState()
	s.workspace = "/next/workspace"
	if !s.canSavePaths() {
		t.Fatal("edited path did not enable Save")
	}
	patch := map[string]json.RawMessage{}
	patch["workspace_root"], _ = json.Marshal(s.workspace)
	submitted := s.settingsDraft()
	s.syncSavedGroups(patch, submitted, core.SettingsResponse{
		Settings:  core.UserSettings{WorkspaceRoot: ptr(s.workspace)},
		Effective: core.EffectiveSettings{WorkspaceRoot: ptr(s.workspace)},
	})
	if s.pathsEdited() || s.canSavePaths() {
		t.Fatalf("saved path still reads as a draft: %q", s.workspace)
	}
	if s.settings.Effective.WorkspaceRoot == nil || *s.settings.Effective.WorkspaceRoot != "/next/workspace" {
		t.Fatal("saved settings baseline was not updated")
	}
	tt := settingsTester(s)
	tt.Frame()
	click(t, tt, "保存")
	if s.busy || s.failure != nil {
		t.Fatalf("disabled Save still submitted: busy=%v failure=%+v", s.busy, s.failure)
	}
}

// Typing into a field while its save is in flight keeps the newer value; only
// the untouched fields of the saved group take the response.
func TestSettingsSavePreservesTypingDuringSave(t *testing.T) {
	s := settingsState()
	s.workspace, s.results = "/submitted/workspace", "/submitted/results"
	submitted := s.settingsDraft()
	// The user keeps typing after the request left.
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
