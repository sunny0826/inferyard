package pages

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func compactAssetBytes(bytes int64) string {
	switch {
	case bytes >= 1<<40:
		return fmt.Sprintf("%.2f TiB", float64(bytes)/(1<<40))
	case bytes >= 1<<30:
		return fmt.Sprintf("%.2f GiB", float64(bytes)/(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.2f MiB", float64(bytes)/(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(bytes)/(1<<10))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func modelsStorageSummaryText(reg core.Registry) string {
	used := compactAssetBytes(reg.TotalModelBytes)
	free := l10n.TR("models.storage.missing")
	if reg.DiskFreeBytes != nil {
		free = compactAssetBytes(*reg.DiskFreeBytes)
	}
	return l10n.TRf("models.storage.summary", "used", used, "free", free)
}

func samePreviewPath(a, b string) bool {
	return filepath.Clean(strings.TrimSpace(a)) == filepath.Clean(strings.TrimSpace(b))
}

func sameModelRoot(a, b string) bool {
	ca, cb := filepath.Clean(strings.TrimSpace(a)), filepath.Clean(strings.TrimSpace(b))
	if ca == cb {
		return true
	}
	ea, errA := filepath.EvalSymlinks(ca)
	eb, errB := filepath.EvalSymlinks(cb)
	return errA == nil && errB == nil && ea == eb
}

func (s *viewState) isExtraModelRootDir(dir, primaryRoot string) bool {
	if strings.TrimSpace(primaryRoot) == "" {
		return true
	}
	if s.preview != nil {
		return !samePreviewPath(dir, primaryRoot)
	}
	return !sameModelRoot(dir, primaryRoot)
}

func (s *viewState) suggestedModelDirs() []core.SuggestedModelDir {
	if s.preview != nil {
		cand := core.SuggestedModelDir{Label: "LM Studio", Path: "/preview/.lmstudio/models"}
		for _, root := range s.registry.ModelRoots {
			if samePreviewPath(root, cand.Path) {
				return nil
			}
		}
		return []core.SuggestedModelDir{cand}
	}
	if s.service == nil {
		return nil
	}
	return core.SuggestLocalModelDirs(s.registry.ModelRoots)
}

func (s *viewState) addExtraModelsRoot(dir string) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return
	}
	if s.preview != nil {
		if !slices.ContainsFunc(s.registry.ModelRoots, func(r string) bool { return samePreviewPath(r, dir) }) {
			s.registry.ModelRoots = append(s.registry.ModelRoots, dir)
		}
		primaryRoot := emptyText(s.settings.Effective.ModelsRoot)
		if (primaryRoot == "" || !samePreviewPath(dir, primaryRoot)) &&
			!slices.ContainsFunc(s.settings.Settings.ExtraModelsRoots, func(r string) bool { return samePreviewPath(r, dir) }) {
			s.settings.Settings.ExtraModelsRoots = append(s.settings.Settings.ExtraModelsRoots, dir)
			s.settings.Effective.ExtraModelsRoots = slices.Clone(s.settings.Settings.ExtraModelsRoots)
		}
		s.enqueueStatusToast(l10n.TRf("models.roots.toast.added_preview", "dir", dir))
		return
	}
	if s.service == nil {
		return
	}
	if s.win != nil {
		s.workAs("add-models-root", func() (string, *core.ApiError) {
			v, err := s.service.AddExtraModelsRoot(dir)
			if err != nil {
				return "", err
			}
			s.win.Update(func() {
				s.settings = v
				if !slices.ContainsFunc(s.registry.ModelRoots, func(r string) bool { return sameModelRoot(r, dir) }) {
					s.registry.ModelRoots = append(s.registry.ModelRoots, dir)
				}
			})
			return l10n.TRf("models.roots.toast.added", "dir", dir), nil
		})
		return
	}
	v, err := s.service.AddExtraModelsRoot(dir)
	if err != nil {
		s.submissionFailed(err)
		return
	}
	s.settings = v
	if !slices.ContainsFunc(s.registry.ModelRoots, func(r string) bool { return sameModelRoot(r, dir) }) {
		s.registry.ModelRoots = append(s.registry.ModelRoots, dir)
	}
	s.enqueueStatusToast(l10n.TRf("models.roots.toast.added", "dir", dir))
}

func (s *viewState) removeExtraModelsRoot(dir string) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return
	}
	if s.preview != nil {
		s.registry.ModelRoots = slices.DeleteFunc(s.registry.ModelRoots, func(r string) bool {
			return samePreviewPath(r, dir)
		})
		s.settings.Settings.ExtraModelsRoots = slices.DeleteFunc(s.settings.Settings.ExtraModelsRoots, func(r string) bool {
			return samePreviewPath(r, dir)
		})
		s.settings.Effective.ExtraModelsRoots = slices.Clone(s.settings.Settings.ExtraModelsRoots)
		s.enqueueStatusToast(l10n.TRf("models.roots.toast.removed_preview", "dir", dir))
		return
	}
	if s.service == nil {
		return
	}
	if s.win != nil {
		s.workAs("remove-models-root", func() (string, *core.ApiError) {
			v, err := s.service.RemoveExtraModelsRoot(dir)
			if err != nil {
				return "", err
			}
			s.win.Update(func() {
				s.settings = v
				s.registry.ModelRoots = slices.DeleteFunc(s.registry.ModelRoots, func(r string) bool {
					return sameModelRoot(r, dir)
				})
			})
			return l10n.TRf("models.roots.toast.removed", "dir", dir), nil
		})
		return
	}
	v, err := s.service.RemoveExtraModelsRoot(dir)
	if err != nil {
		s.submissionFailed(err)
		return
	}
	s.settings = v
	s.registry.ModelRoots = slices.DeleteFunc(s.registry.ModelRoots, func(r string) bool {
		return sameModelRoot(r, dir)
	})
	s.enqueueStatusToast(l10n.TRf("models.roots.toast.removed", "dir", dir))
}

func (s *viewState) pickExtraModelsRoot(defaultPath string) {
	if s.preview != nil {
		s.addExtraModelsRoot("/preview/extra-models")
		return
	}
	open := s.openDialog
	if open == nil {
		if s.win == nil {
			return
		}
		open = mygo.Dialog.Open
	}
	options := mygo.OpenDialogOptions{
		Parent:            s.win,
		Title:             l10n.TR("browse.title.models"),
		DefaultPath:       defaultPath,
		Directory:         true,
		CreateDirectories: true,
	}
	apply := func(paths []string, err error) {
		switch {
		case err != nil:
			s.enqueueStatusToast(browseFailureText())
		case len(paths) > 0 && strings.TrimSpace(paths[0]) != "":
			s.addExtraModelsRoot(paths[0])
		}
	}
	if s.win == nil {
		paths, err := open(options)
		apply(paths, err)
		return
	}
	win := s.win
	go func() {
		paths, err := open(options)
		win.Update(func() { apply(paths, err) })
	}()
}

var revealAssetFunc = func(s *core.Service, kind, name string) *core.ApiError {
	return s.RevealAsset(kind, name)
}

func (s *viewState) revealAsset(kind, name string) {
	if s.preview != nil {
		s.enqueueStatusToast(l10n.TRf("models.asset.toast.reveal_preview", "name", name))
		return
	}
	if s.service == nil {
		return
	}
	if err := revealAssetFunc(s.service, kind, name); err != nil {
		s.submissionFailed(err)
	}
}

func (s *viewState) modelsPageActions(c *ui.Context, wizardOpen *bool) {
	t := tokens(c)
	ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.CardGap).Children(func() {
		button(c, tr(c, "models.action.download_more")).OnClick(func() {
			s.libraryTab = 0
			s.navigateTo("library", "")
		})
		button(c, tr(c, "models.action.add_model_dir")).Disabled(s.busy || !s.canBrowse()).OnClick(func() {
			s.pickExtraModelsRoot(emptyText(s.settings.Effective.ModelsRoot))
		})
		engineLabel := tr(c, "engine.wizard.title")
		if *wizardOpen {
			engineLabel = tr(c, "models.action.collapse_add_engine")
		}
		button(c, engineLabel).OnClick(func() { *wizardOpen = !*wizardOpen })
		refreshButton(c, tr(c, "models.action.refresh"), s.registry.Refreshing || s.refreshing).
			Disabled(s.registry.Refreshing || s.busy).
			OnClick(s.refreshAssetsAction)
	})
}

func (s *viewState) modelStorageAndRootsView(c *ui.Context) {
	t := tokens(c)
	ui.Column(c).FillWidth().Gap(t.Density.Row).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
			components.Icon(c, lucide.HardDrive).Size(14, 14).TextColor(t.Color.FgSecondary)
			ui.Text(c, modelsStorageSummaryText(s.registry)).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary)
		})
		primaryRoot := emptyText(s.settings.Effective.ModelsRoot)
		if len(s.registry.ModelRoots) > 0 {
			applyMaterial(c, ui.Column(c).FillWidth().Padding(t.Density.Tight, t.Density.Stack), materialSunken).
				Border(1, t.Color.BorderSubtle).Children(func() {
				for i, rawDir := range s.registry.ModelRoots {
					if i > 0 {
						ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
					}
					dir := rawDir
					c.Key("model-root-" + dir)
					ui.Row(c).FillWidth().MinHeight(t.Control.SM).AlignItems(ui.Center).Gap(t.Density.Row).
						Padding(t.Density.Hairline, 0).Children(func() {
						components.Icon(c, lucide.Folder).Size(14, 14).TextColor(t.Color.FgSecondary)
						dirText := ui.Text(c, dir).FontSize(t.Type.Caption).TextColor(t.Color.FgPrimary).Grow(1).MinWidth(0).SingleLine().TruncateMiddle()
						components.Tooltip(c, dirText, dir)
						if s.isExtraModelRootDir(dir, primaryRoot) {
							sizedButton(c, components.ButtonGhost, sizeSM, tr(c, "models.roots.remove")).
								Label(trf(c, "models.roots.remove_label", "dir", dir)).
								Disabled(s.busy).
								OnClick(func() { s.removeExtraModelsRoot(dir) })
						} else {
							inlineBadge(c, tr(c, "models.roots.primary"), stateNeutral, badgeOptions{Size: sizeSM, Style: badgeSoft})
						}
					})
				}
			})
		}
		if suggestions := s.suggestedModelDirs(); len(suggestions) > 0 {
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				ui.Text(c, tr(c, "models.roots.suggested")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				for _, item := range suggestions {
					dir := item
					sizedButton(c, components.ButtonOutline, sizeSM, "+ "+dir.Label).
						Label(trf(c, "models.roots.add_suggested_label", "label", dir.Label)).
						Description(dir.Path).
						Disabled(s.busy).
						OnClick(func() { s.addExtraModelsRoot(dir.Path) })
				}
			})
		}
	})
}

func (s *viewState) installedModelRow(c *ui.Context, m core.ModelAsset, pending *pendingAssetDelete) {
	t := tokens(c)
	titleText := modelLabel(m)
	detailsText := modelDetails(m)
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Stack).
		Padding(t.Density.Stack, t.Density.Tight).Children(func() {
		modelIconBox(c, titleText+" "+s.modelIconHint(m.Name), 36, t.Radius.Control)
		ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Tight).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				titleEl := ui.Text(c, titleText).Bold().FontSize(t.Type.Body).TextColor(t.Color.FgPrimary).SingleLine().TruncateMiddle()
				components.Tooltip(c, titleEl, titleText)
				s.renderModelStandaloneBadge(c, m)
			})
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				if detailsText != "" {
					ui.Text(c, detailsText).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary)
				}
				s.renderModelEvidenceInline(c, m)
			})
			s.renderModelStandaloneSubrow(c, m)
		})
		ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			s.renderModelStandalonePrimaryAction(c, m)
			modelTaskAction(c, s, m)
			s.assetRowActions(c, "model", m.Name, m.Path, m.Format, "", m.SizeBytes, m.Deletable, pending)
		})
	})
}

func (s *viewState) installedEngineRow(c *ui.Context, eng core.EngineAsset, pending *pendingAssetDelete) {
	t := tokens(c)
	badgeLabel, badgeKind := s.engineBadgeState(eng)
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Stack).
		Padding(t.Density.Stack, t.Density.Tight).Children(func() {
		engineIconBox(c, engineFamilyTitle(eng.Family, eng.Name)+" "+eng.Family, 36, t.Radius.Control)
		ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Tight).Children(func() {
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				ui.Text(c, eng.Name).Bold().FontSize(t.Type.Body).TextColor(t.Color.FgPrimary).SingleLine()
				inlineBadge(c, badgeLabel, badgeKind, badgeOptions{Style: badgeDot, Size: sizeSM})
				s.renderEngineUpdateBadge(c, eng)
			})
			ui.Text(c, s.engineStateLine(eng)).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		})
		ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			s.renderEngineUpdateAction(c, eng)
			s.assetRowActions(c, "engine", eng.Name, eng.Path, "", eng.Family, nil, eng.Deletable, pending)
		})
	})
}

func (s *viewState) assetRowActions(c *ui.Context, kind, name, path, format, family string, sizeBytes *int64, deletable bool, pending *pendingAssetDelete) {
	if path != "" || s.preview != nil {
		outlineButton(c, tr(c, "models.action.open_dir")).Label(trf(c, "models.action.open_dir_label", "name", name)).Disabled(s.busy).OnClick(func() {
			s.revealAsset(kind, name)
		})
	}
	if deletable {
		if quietDangerButton(c, tr(c, "models.action.delete")).Label(trf(c, "models.action.delete_label", "name", name)).Disabled(s.busy || pending.inspecting).Clicked() {
			s.openDeleteConfirm(pending, kind, name, path, format, family, sizeBytes)
		}
	}
}
