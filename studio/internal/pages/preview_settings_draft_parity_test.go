//go:build ui_debug && !release

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

// Saving one Settings card must not wipe the other card's draft. Production
// reaches the same boundary through persistSettings, the function the Save
// button's work calls; preview goes through the real Save buttons. Each side is
// checked against its own explicit expectation, not against the other's output.
func TestSettingsSaveKeepsOtherCardDraftParity(t *testing.T) {
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

	production := settingsState()
	production.service = service
	production.settings = service.Settings()
	production.loadPathSettings(production.settings)
	production.loadDownloadNames(production.settings)
	production.settingsLoaded = true

	p := newPreviewSession(context.Background())
	p.loadScene(previewSceneIndex(t, "设置 · 路径与目录名"))
	preview, tt := p.state, previewTester(p, 1120, 1600)
	tt.Frame()

	firstPath := filepath.Join(root, "first-workspace")
	secondPath := filepath.Join(root, "second-workspace")

	// The path card saves while the download-name card carries a draft.
	production.workspace, production.modelName = firstPath, "draft-model"
	preview.workspace, preview.modelName = firstPath, "draft-model"
	patch := map[string]json.RawMessage{}
	patch["workspace_root"], _ = json.Marshal(firstPath)
	if _, e := production.persistSettings(patch, production.settingsDraft(), "设置已保存。"); e != nil {
		t.Fatal(e)
	}
	click(t, tt, "保存")
	tt.Frame()
	for name, s := range map[string]*viewState{"production": production, "preview": preview} {
		if s.workspace != firstPath {
			t.Errorf("%s: saved path not shown: %q", name, s.workspace)
		}
		if s.modelName != "draft-model" {
			t.Errorf("%s: path save wiped the download-name draft: %q", name, s.modelName)
		}
	}

	// The name card saves while the path card carries a new draft.
	production.workspace, production.modelName = secondPath, "saved-again"
	preview.workspace, preview.modelName = secondPath, "saved-again"
	tt.Frame()
	names := map[string]json.RawMessage{}
	names["model_download_name"], _ = json.Marshal("saved-again")
	if _, e := production.persistSettings(names, production.settingsDraft(), "下载目录名已保存。"); e != nil {
		t.Fatal(e)
	}
	click(t, tt, "保存下载目录名")
	tt.Frame()
	for name, s := range map[string]*viewState{"production": production, "preview": preview} {
		if s.modelName != "saved-again" {
			t.Errorf("%s: saved name not shown: %q", name, s.modelName)
		}
		if s.workspace != secondPath {
			t.Errorf("%s: name save wiped the path draft: %q", name, s.workspace)
		}
	}
}
