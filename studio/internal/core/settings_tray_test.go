package core

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"inferyard/studio/internal/inferyard"
)

func TestMinimizeToTraySettingPersistenceAndValidation(t *testing.T) {
	root := t.TempDir()
	settingsPath := filepath.Join(root, "settings.json")
	execFn := func(context.Context, string, ...string) (inferyard.Execution, error) {
		return inferyard.Execution{Stdout: []byte(`{"command":"probe","status":"ok"}`)}, nil
	}

	svc, err := New(context.Background(), Options{SettingsPath: settingsPath, Execute: execFn})
	if err != nil {
		t.Fatalf("core.New: %v", err)
	}
	defer func() { _ = svc.Shutdown(context.Background()) }()

	if svc.MinimizeToTray() {
		t.Fatal("expected MinimizeToTray default to be false")
	}
	if s := svc.Settings(); s.Settings.MinimizeToTray || s.Effective.MinimizeToTray {
		t.Fatalf("expected initial settings false, got %+v", s)
	}

	// Enable and verify response + getter.
	resp, apiErr := svc.UpdateSettings(map[string]json.RawMessage{
		"minimize_to_tray": json.RawMessage(`true`),
	})
	if apiErr != nil {
		t.Fatalf("enable minimize_to_tray failed: %+v", apiErr)
	}
	if !resp.Settings.MinimizeToTray || !resp.Effective.MinimizeToTray || !svc.MinimizeToTray() {
		t.Fatalf("expected minimize_to_tray enabled, got %+v", resp)
	}

	// Reload from disk to verify persistence.
	reloaded, err := New(context.Background(), Options{SettingsPath: settingsPath, Execute: execFn})
	if err != nil {
		t.Fatalf("reload core.New: %v", err)
	}
	defer func() { _ = reloaded.Shutdown(context.Background()) }()
	if !reloaded.MinimizeToTray() {
		t.Fatal("expected minimize_to_tray to persist across reload")
	}

	// Null resets to default false.
	resp, apiErr = reloaded.UpdateSettings(map[string]json.RawMessage{
		"minimize_to_tray": json.RawMessage(`null`),
	})
	if apiErr != nil {
		t.Fatalf("reset minimize_to_tray with null failed: %+v", apiErr)
	}
	if resp.Settings.MinimizeToTray || resp.Effective.MinimizeToTray || reloaded.MinimizeToTray() {
		t.Fatalf("expected minimize_to_tray reset to false, got %+v", resp)
	}

	// Non-boolean payload is rejected.
	_, apiErr = reloaded.UpdateSettings(map[string]json.RawMessage{
		"minimize_to_tray": json.RawMessage(`"yes"`),
	})
	if apiErr == nil || apiErr.Code != "invalid_request" {
		t.Fatalf("expected invalid_request for string value, got %+v", apiErr)
	}
}

func TestMinimizeToTrayAllowedWithBoundPaths(t *testing.T) {
	root, err := canonical(t.TempDir())
	if err != nil {
		t.Fatalf("canonical tempdir: %v", err)
	}
	paths := &BoundPaths{
		Source:      "test",
		WindowTitle: "Test Studio",
		Settings:    filepath.Join(root, "settings.json"),
		CLI:         filepath.Join(root, "inferyard"),
		Workspace:   filepath.Join(root, "workspace"),
		Results:     filepath.Join(root, "results"),
		Models:      filepath.Join(root, "models"),
		Engines:     filepath.Join(root, "engines"),
	}
	svc, err := New(context.Background(), Options{
		Paths: paths,
		Execute: func(context.Context, string, ...string) (inferyard.Execution, error) {
			return inferyard.Execution{Stdout: []byte(`{"command":"probe","status":"ok"}`)}, nil
		},
	})
	if err != nil {
		t.Fatalf("core.New with BoundPaths: %v", err)
	}
	defer func() { _ = svc.Shutdown(context.Background()) }()

	resp, apiErr := svc.UpdateSettings(map[string]json.RawMessage{
		"minimize_to_tray": json.RawMessage(`true`),
	})
	if apiErr != nil {
		t.Fatalf("minimize_to_tray should be allowed with BoundPaths, got %+v", apiErr)
	}
	if !resp.Settings.MinimizeToTray || !resp.Effective.MinimizeToTray {
		t.Fatalf("expected minimize_to_tray enabled under BoundPaths, got %+v", resp)
	}
}
