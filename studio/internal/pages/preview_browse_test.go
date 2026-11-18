//go:build ui_debug && !release

package pages

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
)

// Browse in the preview writes a value from the simulated machine and never
// reaches the native picker, the filesystem or settings persistence.
func TestPreviewBrowseUsesSimulatedValue(t *testing.T) {
	p, tt := preparePreview(t)
	// Any call here would prove the preview reached the real picker.
	p.state.openDialog = func(mygo.OpenDialogOptions) ([]string, error) {
		t.Fatal("preview opened the native picker")
		return nil, nil
	}
	click(t, tt, "设置")
	root := emptyText(p.state.settings.Effective.WorkspaceRoot)
	before := p.state.settings.Settings
	click(t, tt, "浏览… · 结果目录")
	tt.Frame()
	want := browseChoice(root, browseResults.key, emptyText(before.ResultsRoot))
	if p.state.results != want {
		t.Fatalf("results = %q, want %q", p.state.results, want)
	}
	if !filepath.IsAbs(p.state.results) {
		t.Fatalf("simulated path is not absolute: %q", p.state.results)
	}
	if emptyText(p.state.settings.Settings.ResultsRoot) != emptyText(before.ResultsRoot) {
		t.Fatal("browsing persisted a setting")
	}
	if p.state.service != nil {
		t.Fatal("browsing reached the real service")
	}
}

// Every path field answers, and the installation field picks a JSON file.
func TestPreviewBrowseCoversAllPathFields(t *testing.T) {
	p, tt := preparePreview(t)
	click(t, tt, "设置")
	for _, tc := range []struct{ label, key string }{
		{"浏览… · CLI", browseCLI.key},
		{"浏览… · 工作区目录", browseWorkspace.key},
		{"浏览… · 结果目录", browseResults.key},
		{"浏览… · 模型目录", browseModels.key},
		{"浏览… · 安装声明 JSON 路径", browseInstallation.key},
	} {
		click(t, tt, tc.label)
		tt.Frame()
		var value string
		switch tc.key {
		case browseCLI.key:
			value = p.state.cliPath
		case browseWorkspace.key:
			value = p.state.workspace
		case browseResults.key:
			value = p.state.results
		case browseModels.key:
			value = p.state.models
		case browseInstallation.key:
			value = p.state.installationPath
		}
		if value == "" || !filepath.IsAbs(value) {
			t.Fatalf("%s produced %q", tc.key, value)
		}
	}
	if filepath.Ext(p.state.installationPath) != ".json" {
		t.Fatalf("installation pick %q", p.state.installationPath)
	}
}

// An unknown key is reported rather than silently accepted.
func TestPreviewBrowseRejectsUnknownField(t *testing.T) {
	p, _ := preparePreview(t)
	p.browse("unknown")
	if p.state.failure == nil || p.state.failure.Code != "preview_unsupported" {
		t.Fatalf("failure = %+v", p.state.failure)
	}
}
