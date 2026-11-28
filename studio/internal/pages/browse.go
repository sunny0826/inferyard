package pages

import (
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/l10n"
)

type browseSpec struct {
	key        string
	titleKey   string
	folder     bool
	extensions []string
}

func browseFailureText() string {
	return l10n.TR("browse.failure")
}

func (spec browseSpec) dialogTitle() string {
	return l10n.TR(spec.titleKey)
}

var (
	browseInstallation  = browseSpec{key: "installation", titleKey: "browse.title.installation", extensions: []string{"json"}}
	browseExecutable    = browseSpec{key: "engine-exec", titleKey: "browse.title.executable"}
	browseModelsRootDir = browseSpec{key: "engine-models-root", titleKey: "browse.title.models_root_dir", folder: true}
	browseWorkspace     = browseSpec{key: "workspace", titleKey: "browse.title.workspace", folder: true}
)

func (s *viewState) pathField(c *ui.Context, label string, value *string, spec browseSpec) ui.Element {
	return ui.Field(c, label, func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(tokens(c).Density.Row).Children(func() {
			studioTextBox(c, value).Grow(1).MinWidth(0)
			button(c, tr(c, "browse.button")).Shrink(0).Label(trf(c, "browse.button_label", "label", label)).Disabled(s.busy || !s.canBrowse()).OnClick(func() {
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
	if s.preview != nil && (spec.key == browseExecutable.key || spec.key == browseModelsRootDir.key) {
		root := emptyText(s.settings.Effective.WorkspaceRoot)
		if root == "" {
			root = emptyText(s.settings.Settings.WorkspaceRoot)
		}
		if root == "" {
			root = "/preview"
		}
		if spec.key == browseModelsRootDir.key {
			*value = filepath.Join(root, "lmstudio-models")
		} else {
			*value = filepath.Join(root, "engines", "llama-server")
		}
		return
	}
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
			s.browseFailure(spec.key, browseFailureText())
		case len(paths) > 0:
			s.clearBrowseFailure(spec.key)
			*value = paths[0]
		}
	}
	options := mygo.OpenDialogOptions{
		Parent: win, Title: spec.dialogTitle(), DefaultPath: defaultPath,
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
