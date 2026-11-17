package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"inferyard/studio/internal/inferyard"
)

func preparationService(t *testing.T, workspace string) *Service {
	t.Helper()
	root := t.TempDir()
	settings := filepath.Join(root, "settings.json")
	raw, _ := json.Marshal(UserSettings{WorkspaceRoot: &workspace})
	if err := os.WriteFile(settings, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	service, err := New(context.Background(), Options{
		SettingsPath: settings,
		Execute: func(context.Context, string, ...string) (inferyard.Execution, error) {
			return inferyard.Execution{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { service.Shutdown(context.Background()) })
	return service
}

// The projection answers "where would preparation write" without creating
// anything or touching the network.
func TestPreparationLocationsIsReadOnly(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "not-created")
	service := preparationService(t, workspace)
	locations := service.PreparationLocations()
	if locations.ToolsDir == "" || locations.ManagedPython == "" {
		t.Fatalf("%+v", locations)
	}
	if locations.Workspace == nil || *locations.Workspace != workspace {
		t.Fatalf("workspace %+v", locations.Workspace)
	}
	if locations.Bound {
		t.Fatal("an unbound service reported bound paths")
	}
	if _, err := os.Stat(workspace); !os.IsNotExist(err) {
		t.Fatal("the projection created the workspace")
	}
	if _, err := os.Stat(locations.ToolsDir); !os.IsNotExist(err) {
		t.Fatal("the projection created the tools directory")
	}
}

// A configured CLI is reported, and no CLI is invented when none exists.
func TestPreparationLocationsReportsOnlyExistingCLI(t *testing.T) {
	workspace := t.TempDir()
	service := preparationService(t, workspace)
	service.settings.InferyardPath = nil
	locations := service.PreparationLocations()
	if locations.CLI != nil {
		if _, err := os.Stat(*locations.CLI); err != nil {
			t.Fatalf("reported a CLI that does not exist: %v", err)
		}
	}
}
