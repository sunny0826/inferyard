package pages

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
)

type browseSpec struct {
	key        string
	title      string
	folder     bool
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
