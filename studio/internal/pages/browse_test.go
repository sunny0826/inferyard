package pages

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

// browseTestState drives the settings form with an injected picker, so the
// production path is visible without opening a real dialog.
func browseTestState(open func(mygo.OpenDialogOptions) ([]string, error)) viewState {
	s := testState()
	s.selected, s.openDialog = "settings", open
	root := filepath.Join("/tmp", "inferyard-browse")
	s.settings = core.SettingsResponse{
		Settings:  core.UserSettings{WorkspaceRoot: &root},
		Effective: core.EffectiveSettings{WorkspaceRoot: &root},
	}
	return s
}

// Picking a directory fills only that field and asks for the right picker mode.
func TestBrowseWritesSelectedDirectory(t *testing.T) {
	var got mygo.OpenDialogOptions
	s := browseTestState(func(options mygo.OpenDialogOptions) ([]string, error) {
		got = options
		return []string{"/tmp/chosen-results"}, nil
	})
	tt := ui.NewTester(s.view, 1120, 2000)
	click(t, tt, "浏览… · 结果目录")
	tt.Frame()
	if s.results != "/tmp/chosen-results" {
		t.Fatalf("results = %q", s.results)
	}
	if !got.Directory || !got.CreateDirectories {
		t.Fatalf("directory picker options: %+v", got)
	}
	if got.Title != browseResults.title {
		t.Fatalf("title = %q", got.Title)
	}
	if s.workspace != "" || s.models != "" || s.cliPath != "" {
		t.Fatal("browse wrote more than the chosen field")
	}
}

// The installation declaration asks for a JSON file, not a directory.
func TestBrowseInstallationFiltersJSON(t *testing.T) {
	var got mygo.OpenDialogOptions
	s := browseTestState(func(options mygo.OpenDialogOptions) ([]string, error) {
		got = options
		return []string{"/tmp/installation.json"}, nil
	})
	tt := ui.NewTester(s.view, 1120, 2400)
	click(t, tt, "浏览… · 安装声明 JSON 路径")
	tt.Frame()
	if got.Directory {
		t.Fatal("installation must open a file picker")
	}
	if len(got.Filters) != 1 || len(got.Filters[0].Extensions) != 1 || got.Filters[0].Extensions[0] != "json" {
		t.Fatalf("filters = %+v", got.Filters)
	}
	if s.installationPath != "/tmp/installation.json" {
		t.Fatalf("path = %q", s.installationPath)
	}
}

// Cancelling and failing both leave the draft untouched; only a failure is
// reported, next to that field.
func TestBrowseCancelAndFailureKeepTheDraft(t *testing.T) {
	for _, tc := range []struct {
		name  string
		open  func(mygo.OpenDialogOptions) ([]string, error)
		error string
	}{
		{"cancel", func(mygo.OpenDialogOptions) ([]string, error) { return nil, nil }, ""},
		{"failure", func(mygo.OpenDialogOptions) ([]string, error) { return nil, errors.New("denied") }, "无法打开选择窗口；可手动输入路径。"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := browseTestState(tc.open)
			s.models = "/tmp/keep-models"
			tt := ui.NewTester(s.view, 1120, 2000)
			click(t, tt, "浏览… · 模型目录")
			tt.Frame()
			if s.models != "/tmp/keep-models" {
				t.Fatalf("draft changed to %q", s.models)
			}
			if tc.error == "" {
				if tt.HasText("无法打开选择窗口；可手动输入路径。") {
					t.Fatal("cancel reported an error")
				}
				return
			}
			if !tt.HasText(tc.error) {
				t.Fatalf("missing %q: %v", tc.error, tt.Texts())
			}
		})
	}
}

// An invalid manual value keeps its own error instead of the picker error.
func TestBrowseErrorYieldsToManualValidation(t *testing.T) {
	s := browseTestState(func(mygo.OpenDialogOptions) ([]string, error) { return nil, errors.New("denied") })
	s.models = "relative/path"
	s.browseFailure(browseModels.key, "无法打开选择窗口；可手动输入路径。")
	tt := ui.NewTester(s.view, 1120, 2000)
	if !tt.HasText("请输入有效的绝对路径。") {
		t.Fatal(tt.Texts())
	}
}
