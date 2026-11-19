package pages

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
)

// browseSpec describes which native picker a path field opens. Manual typing
// stays available: some targets (a workspace that does not exist yet) cannot be
// expressed by picking an existing entry.
type browseSpec struct {
	key    string
	title  string
	folder bool
	// extensions restricts file pickers only; an empty list accepts any file.
	extensions []string
}

const browseFailureMessage = "无法打开选择窗口；可手动输入路径。"

var (
	browseCLI          = browseSpec{key: "cli", title: "选择 CLI 可执行文件"}
	browseInstallation = browseSpec{key: "installation", title: "选择 installation.json", extensions: []string{"json"}}
	browseWorkspace    = browseSpec{key: "workspace", title: "选择工作区目录", folder: true}
	browseResults      = browseSpec{key: "results", title: "选择结果目录", folder: true}
	browseModels       = browseSpec{key: "models", title: "选择模型目录", folder: true}
)

// pathField renders a path input with a native Browse button. The button opens a
// picker attached to the window; picking writes the field, cancelling leaves the
// draft untouched, and failures appear next to this field only.
func (s *viewState) pathField(c *ui.Context, label string, value *string, spec browseSpec) ui.Element {
	return ui.Field(c, label, func() {
		ui.Row(c).AlignItems(ui.Center).Gap(tokens(c).Density.Row).Children(func() {
			components.TextBox(c, value, components.TextBoxOptions{}).Grow(1)
			button(c, "浏览…").Label("浏览… · " + label).Disabled(s.busy || !s.canBrowse()).OnClick(func() {
				s.browsePath(value, spec)
			})
		})
	})
}

// canBrowse reports whether a picker can be opened: production has a window, and
// the preview answers through its in-memory intent instead.
func (s *viewState) canBrowse() bool {
	if s.preview != nil {
		return true
	}
	return s.win != nil || s.openDialog != nil
}

func (s *viewState) browsePath(value *string, spec browseSpec) {
	if s.simulate(previewBrowsePath{key: spec.key}) {
		return
	}
	open := s.openDialog
	if open == nil {
		if s.win == nil {
			return
		}
		open = mygo.Dialog.Open
	}
	defaultPath := *value
	win := s.win
	apply := func(paths []string, err error) {
		switch {
		case err != nil:
			s.browseFailure(spec.key, browseFailureMessage)
		case len(paths) > 0:
			s.clearBrowseFailure(spec.key)
			*value = paths[0]
		}
	}
	options := mygo.OpenDialogOptions{
		Parent: win, Title: spec.title, DefaultPath: defaultPath,
		Directory: spec.folder, CreateDirectories: spec.folder,
		Filters: spec.filters(),
	}
	// A real sheet needs the window and reports back on the UI thread. Without
	// one (tests and headless callers) the picker result applies in place.
	if win == nil {
		paths, err := open(options)
		apply(paths, err)
		return
	}
	go func() {
		paths, err := open(options)
		win.Update(func() { apply(paths, err) })
	}()
}

func (spec browseSpec) filters() []mygo.FileFilter {
	if len(spec.extensions) == 0 {
		return nil
	}
	return []mygo.FileFilter{{Name: "JSON", Extensions: spec.extensions}}
}

func (s *viewState) browseFailure(key, message string) {
	if s.browseFailures == nil {
		s.browseFailures = map[string]string{}
	}
	s.browseFailures[key] = message
}

func (s *viewState) clearBrowseFailure(key string) {
	delete(s.browseFailures, key)
}

func (s *viewState) browseError(key, manual string) string {
	if manual != "" {
		return manual
	}
	return s.browseFailures[key]
}
