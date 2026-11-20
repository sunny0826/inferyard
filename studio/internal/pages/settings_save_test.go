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

// settingsTestService mounts a real service over a throwaway settings file.
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

// A refresh batch read before a save landed is dropped whole: the saved
// workspace, names, readiness, progress and reports all survive.
func TestStaleRefreshBatchDoesNotRevertSave(t *testing.T) {
	s := settingsState()
	// The batch reads the old values, then a save lands while it is in flight.
	stale := refreshBatch{epoch: s.settingsSequence()}
	stale.settings = core.SettingsResponse{
		Settings:  core.UserSettings{WorkspaceRoot: ptr("/saved/workspace"), ResultsRoot: ptr("/saved/results"), ModelsRoot: ptr("/saved/models"), ModelDownloadName: "saved-model", EngineDownloadName: "saved-engine"},
		Effective: core.EffectiveSettings{WorkspaceRoot: ptr("/saved/workspace")},
	}
	stale.ready.Workspace = core.DirectoryReadiness{Path: ptr("/saved/workspace"), Exists: true, Initialized: true}
	// Readiness for the saved workspace, which the stale batch must not replace.
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

	// A batch taken after the save still applies, as the follow-up refresh does.
	fresh := refreshBatch{epoch: s.settingsSequence()}
	fresh.settings = core.SettingsResponse{
		Settings:  core.UserSettings{WorkspaceRoot: ptr("/later/workspace")},
		Effective: core.EffectiveSettings{WorkspaceRoot: ptr("/later/workspace")},
	}
	fresh.ready.Workspace = core.DirectoryReadiness{Path: ptr("/later/workspace"), Exists: true}
	// The fresh batch paints through the service: mount one so the paint is
	// observable end to end.
	s.service = settingsTestService(t, t.TempDir())
	s.ctx = context.Background()
	// Service discovery is not what this test observes; keep it from starting.
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

// Saving a new workspace path updates the settings before the next refresh
// reads readiness. Until that refresh arrives the path card must say the check
// is still pending instead of showing the replaced directory's result.
func TestSavedPathShowsPendingUntilReadinessArrives(t *testing.T) {
	s := settingsState()
	s.ready.Workspace = core.DirectoryReadiness{Path: ptr("/saved/workspace"), Exists: true, Initialized: true}
	s.workspace = "/next/workspace"
	patch := map[string]json.RawMessage{}
	patch["workspace_root"], _ = json.Marshal(s.workspace)
	s.syncSavedGroups(patch, s.settingsDraft(), core.SettingsResponse{
		Settings:  core.UserSettings{WorkspaceRoot: ptr("/next/workspace")},
		Effective: core.EffectiveSettings{WorkspaceRoot: ptr("/next/workspace")},
	})

	if got := directoryState(s.workspace, s.ready.Workspace, emptyText(s.settings.Effective.WorkspaceRoot)); got != "等待检查" {
		t.Fatalf("saved path reused the old readiness: %q", got)
	}
	tt := settingsTester(s)
	tt.Frame()
	if tt.HasText("目录已存在，工作区已初始化") {
		t.Fatalf("old workspace readiness shown under the new path: %v", tt.Texts())
	}
	if !tt.HasText("等待检查") {
		t.Fatalf("pending check not shown: %v", tt.Texts())
	}

	// The follow-up refresh reports the new path, and the card settles.
	s.ready.Workspace = core.DirectoryReadiness{Path: ptr("/next/workspace"), Exists: true, Initialized: true}
	if got := directoryState(s.workspace, s.ready.Workspace, emptyText(s.settings.Effective.WorkspaceRoot)); got != "目录已存在，工作区已初始化" {
		t.Fatalf("new readiness not shown: %q", got)
	}
}

// First-time configuration of Results or Models has no prior probe at all, so
// the card must say the check is pending instead of claiming the new directory
// does not exist before the post-save refresh runs.
func TestFirstTimeDirectorySaveShowsPending(t *testing.T) {
	for _, name := range []string{"results", "models"} {
		s := settingsState()
		value := "/first/" + name
		patch := map[string]json.RawMessage{}
		if name == "results" {
			s.settings.Settings.ResultsRoot, s.results = nil, value
			s.ready.Results = core.DirectoryReadiness{} // never probed: no recorded path
			patch["results_root"], _ = json.Marshal(value)
		} else {
			s.settings.Settings.ModelsRoot, s.models = nil, value
			s.ready.Models = core.DirectoryReadiness{}
			patch["models_root"], _ = json.Marshal(value)
		}
		if !s.pathsEdited() {
			t.Fatalf("%s: first-time entry did not register as an edit", name)
		}
		s.syncSavedGroups(patch, s.settingsDraft(), core.SettingsResponse{
			Settings:  core.UserSettings{ResultsRoot: ptr(value), ModelsRoot: ptr(value)},
			Effective: core.EffectiveSettings{ResultsRoot: ptr(value), ModelsRoot: ptr(value)},
		})
		saved := emptyText(s.settings.Settings.ResultsRoot)
		readiness := s.ready.Results
		if name == "models" {
			saved = emptyText(s.settings.Settings.ModelsRoot)
			readiness = s.ready.Models
		}
		if got := directoryState(value, readiness, saved); got != "等待检查" {
			t.Errorf("%s: first-time save claimed a directory result: %q", name, got)
		}

		// The post-save refresh supplies readiness for the new path.
		fresh := core.DirectoryReadiness{Path: ptr(value), Exists: true}
		if got := directoryState(value, fresh, saved); got != "目录已存在" {
			t.Errorf("%s: refreshed readiness not shown: %q", name, got)
		}
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
