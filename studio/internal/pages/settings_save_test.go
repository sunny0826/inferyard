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
	paths.syncSavedGroups(map[string]json.RawMessage{"workspace_root": json.RawMessage(`"/draft/workspace"`)}, respond)
	if paths.workspace != "/saved/workspace" {
		t.Fatalf("saved path not synced: %q", paths.workspace)
	}
	if paths.modelName != "draft-model" {
		t.Fatalf("path save wiped the download-name draft: %q", paths.modelName)
	}

	names := settingsState()
	names.workspace, names.modelName = "/draft/workspace", "draft-model"
	names.syncSavedGroups(map[string]json.RawMessage{"model_download_name": json.RawMessage(`"saved-model"`)}, respond)
	if names.modelName != "saved-model" {
		t.Fatalf("saved name not synced: %q", names.modelName)
	}
	if names.workspace != "/draft/workspace" {
		t.Fatalf("name save wiped the path draft: %q", names.workspace)
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
	if _, e := s.persistSettings(patch, "设置已保存。"); e != nil {
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
	if _, e := s.persistSettings(names, "下载目录名已保存。"); e != nil {
		t.Fatal(e)
	}
	if s.modelName != "saved-again" {
		t.Fatalf("saved name not shown: %q", s.modelName)
	}
	if s.workspace != next {
		t.Fatalf("name save wiped the path draft: %q", s.workspace)
	}
}
