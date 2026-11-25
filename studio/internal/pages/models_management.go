package pages

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

type pendingAssetDelete struct {
	open              bool
	seq               int
	inspecting        bool
	kind              string
	name              string
	path              string
	fingerprint       string
	scope             string
	reclaimableBytes  *int64
	preservedShared   bool
	preservedExternal bool
	keepsModelData    bool
}

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

func (s *viewState) deleteManagedAsset(kind, name string) {
	s.deleteManagedAssetVerified(kind, name, "", "")
}

func (s *viewState) deleteManagedAssetVerified(kind, name, expectedPath, expectedFP string) {
	if s.preview != nil {
		if kind == "model" {
			s.registry.Models = slices.DeleteFunc(s.registry.Models, func(m core.ModelAsset) bool {
				return m.Name == name && (expectedPath == "" || m.Path == expectedPath)
			})
			var total int64
			for _, m := range s.registry.Models {
				if m.SizeBytes != nil && *m.SizeBytes > 0 {
					total += *m.SizeBytes
				}
			}
			s.registry.TotalModelBytes = total
			if s.model == name {
				s.model = ""
			}
		} else {
			s.registry.Engines = slices.DeleteFunc(s.registry.Engines, func(e core.EngineAsset) bool {
				return e.Name == name && (expectedPath == "" || e.Path == expectedPath)
			})
			if s.checks.Engines != nil {
				delete(s.checks.Engines, name)
			}
			if s.engine == name {
				s.engine = ""
			}
		}
		s.enqueueStatusToast(l10n.TRf("models.asset.toast.deleted_preview", "name", name))
		return
	}
	if s.service == nil {
		return
	}
	execDelete := func() *core.ApiError {
		if expectedPath != "" {
			return s.service.DeleteManagedAssetVerified(kind, name, expectedPath, expectedFP)
		}
		return s.service.DeleteManagedAsset(kind, name)
	}
	if s.win != nil {
		s.workAs("delete-asset-"+kind+"-"+name, func() (string, *core.ApiError) {
			if err := execDelete(); err != nil {
				return "", err
			}
			return l10n.TRf("models.asset.toast.deleted", "name", name), nil
		})
		return
	}
	if err := execDelete(); err != nil {
		s.submissionFailed(err)
		return
	}
	s.settings = s.service.Settings()
	s.registry = s.service.Registry()
	s.checks = s.service.EngineChecks()
	if kind == "model" && s.model == name {
		s.model = ""
	} else if kind == "engine" && s.engine == name {
		s.engine = ""
	}
	s.enqueueStatusToast(l10n.TRf("models.asset.toast.deleted", "name", name))
}

func (s *viewState) modelsPageActions(c *ui.Context, wizardOpen *bool) {
	t := tokens(c)
	ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.CardGap).Children(func() {
		button(c, tr(c, "models.action.download_more")).OnClick(func() { s.selected = "downloads" })
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
	ui.Column(c).FillWidth().Gap(t.Density.Tight).Children(func() {
		ui.Text(c, modelsStorageSummaryText(s.registry)).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		primaryRoot := emptyText(s.settings.Effective.ModelsRoot)
		if len(s.registry.ModelRoots) > 0 {
			applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Hairline).Padding(t.Density.Tight), materialSunken).Children(func() {
				for _, rawDir := range s.registry.ModelRoots {
					dir := rawDir
					c.Key("model-root-" + dir)
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
						ui.Text(c, dir).FontSize(t.Type.Caption).TextColor(t.Color.FgPrimary).Grow(1).MinWidth(0).SingleLine()
						if s.isExtraModelRootDir(dir, primaryRoot) {
							sizedButton(c, components.ButtonGhost, sizeSM, tr(c, "models.roots.remove")).
								Label(trf(c, "models.roots.remove_label", "dir", dir)).
								Disabled(s.busy).
								OnClick(func() { s.removeExtraModelsRoot(dir) })
						} else {
							ui.Text(c, tr(c, "models.roots.primary")).FontSize(t.Type.Caption).TextColor(t.Color.FgTertiary).SingleLine()
						}
					})
				}
			})
		}
		if suggestions := s.suggestedModelDirs(); len(suggestions) > 0 {
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
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

func (s *viewState) modelRowTrailing(c *ui.Context, m core.ModelAsset, pending *pendingAssetDelete) {
	t := tokens(c)
	ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
		if !m.Available {
			badge(c, tr(c, "models.badge.unavailable"), stateWarning, badgeOptions{Style: badgeDot, Size: sizeSM})
		}
		s.modelStandaloneActions(c, m)
		modelTaskAction(c, s, m)
		s.assetRowActions(c, "model", m.Name, m.Path, m.Format, "", m.SizeBytes, m.Deletable, pending)
	})
}

func (s *viewState) engineRowTrailing(c *ui.Context, eng core.EngineAsset, pending *pendingAssetDelete) {
	t := tokens(c)
	ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
		label, kind := s.engineBadgeState(eng)
		badge(c, label, kind, badgeOptions{Style: badgeDot, Size: sizeSM})
		s.assetRowActions(c, "engine", eng.Name, eng.Path, "", eng.Family, nil, eng.Deletable, pending)
	})
}

func (s *viewState) assetRowActions(c *ui.Context, kind, name, path, format, family string, sizeBytes *int64, deletable bool, pending *pendingAssetDelete) {
	if path != "" || s.preview != nil {
		button(c, tr(c, "models.action.open_dir")).Label(trf(c, "models.action.open_dir_label", "name", name)).Disabled(s.busy).OnClick(func() {
			s.revealAsset(kind, name)
		})
	}
	if deletable {
		if dangerButton(c, tr(c, "models.action.delete")).Label(trf(c, "models.action.delete_label", "name", name)).Disabled(s.busy || pending.inspecting).Clicked() {
			s.openDeleteConfirm(pending, kind, name, path, format, family, sizeBytes)
		}
	}
}

func cloneBytesPtr(v *int64) *int64 {
	if v == nil {
		return nil
	}
	copyVal := *v
	return &copyVal
}

func (s *viewState) openDeleteConfirm(pending *pendingAssetDelete, kind, name, path, format, family string, sizeBytes *int64) {
	if pending.inspecting {
		return
	}
	seq := pending.seq + 1
	scope := "standard"
	var initialSize *int64
	keepsModelData := false
	if kind == "engine" && family == "polystrata" {
		scope = "polystrata_engine"
		if s.preview != nil {
			previewSize := int64(2 << 30)
			initialSize = &previewSize
			keepsModelData = true
		}
	} else if kind == "model" && format == "strata_pack" {
		scope = "polystrata_model"
		initialSize = cloneBytesPtr(sizeBytes)
	} else {
		initialSize = cloneBytesPtr(sizeBytes)
	}

	next := pendingAssetDelete{
		open:             true,
		seq:              seq,
		kind:             kind,
		name:             name,
		path:             path,
		scope:            scope,
		reclaimableBytes: initialSize,
		keepsModelData:   keepsModelData,
	}
	if s.preview != nil || s.service == nil {
		*pending = next
		return
	}
	updateFn := s.updateWindow
	if updateFn == nil && s.win != nil {
		win := s.win
		updateFn = win.Update
	}
	if updateFn == nil {
		info, err := s.service.InspectDeleteAsset(kind, name)
		if err != nil {
			s.submissionFailed(err)
			*pending = pendingAssetDelete{seq: seq}
			return
		}
		applyDeleteInfo(&next, info)
		*pending = next
		return
	}

	*pending = pendingAssetDelete{
		seq:        seq,
		inspecting: true,
		kind:       kind,
		name:       name,
	}
	svc := s.service
	go func() {
		info, err := svc.InspectDeleteAsset(kind, name)
		updateFn(func() {
			if pending.seq != seq || !pending.inspecting {
				return
			}
			pending.inspecting = false
			if err != nil {
				*pending = pendingAssetDelete{seq: seq}
				s.submissionFailed(err)
				return
			}
			applyDeleteInfo(&next, info)
			*pending = next
		})
	}()
}

func applyDeleteInfo(p *pendingAssetDelete, info core.AssetDeleteInfo) {
	if info.TargetPath != "" {
		p.path = info.TargetPath
	}
	p.fingerprint = info.Fingerprint
	p.scope = info.Scope
	p.reclaimableBytes = cloneBytesPtr(info.ReclaimableBytes)
	p.preservedShared = info.PreservedShared
	p.preservedExternal = info.PreservedExternal
	p.keepsModelData = info.KeepsModelData
}

func assetDeleteConfirmMessage(p pendingAssetDelete) string {
	sizeClause := ""
	if p.reclaimableBytes != nil {
		sizeClause = l10n.TRf("models.delete.clause.size", "size", compactAssetBytes(*p.reclaimableBytes))
	}
	pathClause := ""
	if p.path != "" {
		pathClause = l10n.TRf("models.delete.clause.path", "path", p.path)
	}
	switch p.scope {
	case "polystrata_engine":
		parts := []string{l10n.TRf("models.delete.confirm.polystrata_engine", "name", p.name, "pathClause", pathClause, "sizeClause", sizeClause)}
		if p.keepsModelData {
			parts = append(parts, l10n.TR("models.delete.note.keeps_model_data"))
		}
		return strings.Join(parts, " ")
	case "polystrata_model":
		parts := []string{l10n.TRf("models.delete.confirm.polystrata_model", "name", p.name, "sizeClause", sizeClause)}
		if p.preservedShared {
			parts = append(parts, l10n.TR("models.delete.note.preserved_shared"))
		}
		if p.preservedExternal {
			parts = append(parts, l10n.TR("models.delete.note.preserved_external_managed"))
		}
		return strings.Join(parts, " ")
	case "polystrata_model_config_only":
		parts := []string{l10n.TRf("models.delete.confirm.polystrata_model_config_only", "name", p.name, "sizeClause", sizeClause)}
		if p.preservedShared {
			parts = append(parts, l10n.TR("models.delete.note.preserved_shared"))
		}
		if p.preservedExternal {
			parts = append(parts, l10n.TR("models.delete.note.preserved_external"))
		}
		return strings.Join(parts, " ")
	default:
		if p.reclaimableBytes != nil {
			return l10n.TRf("models.delete.confirm.standard_sized", "name", p.name, "pathClause", pathClause, "sizeClause", sizeClause)
		}
		return l10n.TRf("models.delete.confirm.standard", "name", p.name, "pathClause", pathClause)
	}
}

func (s *viewState) assetDeleteDialog(c *ui.Context, pending *pendingAssetDelete) {
	c.Key("asset-delete-dialog")
	title := tr(c, "models.delete.dialog_title")
	result := components.AlertDialog(c, &pending.open, components.AlertDialogOptions{
		DialogOptions: components.DialogOptions{Label: title},
		Title:         title,
		Message:       assetDeleteConfirmMessage(*pending),
		ConfirmLabel:  tr(c, "models.delete.confirm_button"),
		CancelLabel:   tr(c, "models.delete.cancel_button"),
	})
	switch result {
	case components.DialogConfirmed:
		kind, name, seq := pending.kind, pending.name, pending.seq
		expectedPath, expectedFP := pending.path, pending.fingerprint
		*pending = pendingAssetDelete{seq: seq}
		s.deleteManagedAssetVerified(kind, name, expectedPath, expectedFP)
	case components.DialogCancelled:
		*pending = pendingAssetDelete{seq: pending.seq}
	default:
		if !pending.open && !pending.inspecting {
			*pending = pendingAssetDelete{seq: pending.seq}
		}
	}
}
