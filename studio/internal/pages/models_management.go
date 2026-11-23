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
	free := "缺测"
	if reg.DiskFreeBytes != nil {
		free = compactAssetBytes(*reg.DiskFreeBytes)
	}
	return "总占用 " + used + " · 可用磁盘空间 " + free
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

func isExtraModelRoot(idx int, dir, primaryRoot string) bool {
	if strings.TrimSpace(primaryRoot) != "" {
		return !sameModelRoot(dir, primaryRoot)
	}
	return idx > 0
}

func (s *viewState) suggestedModelDirs() []core.SuggestedModelDir {
	if s.preview != nil {
		cand := core.SuggestedModelDir{Label: "LM Studio", Path: "/preview/.lmstudio/models"}
		for _, root := range s.registry.ModelRoots {
			if sameModelRoot(root, cand.Path) {
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
		if !slices.ContainsFunc(s.registry.ModelRoots, func(r string) bool { return sameModelRoot(r, dir) }) {
			s.registry.ModelRoots = append(s.registry.ModelRoots, dir)
		}
		s.enqueueStatusToast("已模拟添加模型目录：" + dir)
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
			return "已记录目录：" + dir, nil
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
	s.enqueueStatusToast("已记录目录：" + dir)
}

func (s *viewState) removeExtraModelsRoot(dir string) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return
	}
	if s.preview != nil {
		s.registry.ModelRoots = slices.DeleteFunc(s.registry.ModelRoots, func(r string) bool {
			return sameModelRoot(r, dir)
		})
		s.enqueueStatusToast("已模拟移除模型目录：" + dir)
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
			return "已移除目录：" + dir, nil
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
	s.enqueueStatusToast("已移除目录：" + dir)
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
		Title:             browseModels.title,
		DefaultPath:       defaultPath,
		Directory:         true,
		CreateDirectories: true,
	}
	apply := func(paths []string, err error) {
		switch {
		case err != nil:
			s.enqueueStatusToast(browseFailureMessage)
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
		s.enqueueStatusToast("模拟打开目录：" + name)
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
		s.enqueueStatusToast("已模拟删除：" + name)
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
			return "已删除：" + name, nil
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
	s.enqueueStatusToast("已删除：" + name)
}

func (s *viewState) modelsPageActions(c *ui.Context, wizardOpen *bool) {
	t := tokens(c)
	ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.CardGap).Children(func() {
		button(c, "下载更多").OnClick(func() { s.selected = "downloads" })
		button(c, "添加模型目录").Disabled(s.busy || !s.canBrowse()).OnClick(func() {
			s.pickExtraModelsRoot(emptyText(s.settings.Settings.ModelsRoot))
		})
		engineLabel := "添加本地引擎"
		if *wizardOpen {
			engineLabel = "收起添加引擎"
		}
		button(c, engineLabel).OnClick(func() { *wizardOpen = !*wizardOpen })
		refreshButton(c, "刷新", s.registry.Refreshing || s.refreshing).
			Disabled(s.registry.Refreshing || s.busy).
			OnClick(s.refreshAssetsAction)
	})
}

func (s *viewState) modelStorageAndRootsView(c *ui.Context) {
	t := tokens(c)
	ui.Column(c).FillWidth().Gap(t.Density.Tight).Children(func() {
		ui.Text(c, modelsStorageSummaryText(s.registry)).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		primaryRoot := emptyText(s.settings.Settings.ModelsRoot)
		if len(s.registry.ModelRoots) > 0 {
			applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Hairline).Padding(t.Density.Tight), materialSunken).Children(func() {
				for idx, rawDir := range s.registry.ModelRoots {
					dir := rawDir
					c.Key("model-root-" + dir)
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
						ui.Text(c, dir).FontSize(t.Type.Caption).TextColor(t.Color.FgPrimary).Grow(1).MinWidth(0).SingleLine()
						if isExtraModelRoot(idx, dir, primaryRoot) {
							sizedButton(c, components.ButtonGhost, sizeSM, "移除").
								Label("移除目录 · " + dir).
								Disabled(s.busy).
								OnClick(func() { s.removeExtraModelsRoot(dir) })
						} else {
							ui.Text(c, "主目录").FontSize(t.Type.Caption).TextColor(t.Color.FgTertiary).SingleLine()
						}
					})
				}
			})
		}
		if suggestions := s.suggestedModelDirs(); len(suggestions) > 0 {
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
				ui.Text(c, "本机常见目录：").FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				for _, item := range suggestions {
					dir := item
					sizedButton(c, components.ButtonOutline, sizeSM, "+ "+dir.Label).
						Label("添加常见目录 · " + dir.Label).
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
			badge(c, "不可用", stateWarning, badgeOptions{Style: badgeDot, Size: sizeSM})
		}
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
		button(c, "打开目录").Label("打开目录 · " + name).Disabled(s.busy).OnClick(func() {
			s.revealAsset(kind, name)
		})
	}
	if deletable {
		if dangerButton(c, "删除").Label("删除 · " + name).Disabled(s.busy || pending.inspecting).Clicked() {
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
		sizeClause = fmt.Sprintf("，预计释放 %s", compactAssetBytes(*p.reclaimableBytes))
	}
	pathClause := ""
	if p.path != "" {
		pathClause = fmt.Sprintf("（%s）", p.path)
	}
	switch p.scope {
	case "polystrata_engine":
		msg := fmt.Sprintf("将卸载并移除 PolyStrata 托管运行时「%s」%s%s，并移除本地引擎注册，此操作不可撤销。", p.name, pathClause, sizeClause)
		if p.keepsModelData {
			msg += " 已下载的模型包（polystrata-data）将保留，可在模型列表中单独删除。"
		}
		return msg
	case "polystrata_model":
		msg := fmt.Sprintf("将删除 PolyStrata 模型「%s」的配置及未被引用的托管模型权重%s，此操作不可撤销。", p.name, sizeClause)
		if p.preservedShared {
			msg += " 其他模型配置仍引用的共享权重将被保留。"
		}
		if p.preservedExternal {
			msg += " 托管目录外的外部权重文件不会被删除。"
		}
		return msg
	case "polystrata_model_config_only":
		msg := fmt.Sprintf("将仅删除 PolyStrata 模型配置「%s」%s，此操作不可撤销。", p.name, sizeClause)
		if p.preservedShared {
			msg += " 其他模型配置仍引用的共享权重将被保留。"
		}
		if p.preservedExternal {
			msg += " 外部权重文件不会被删除。"
		}
		return msg
	default:
		if p.reclaimableBytes != nil {
			return fmt.Sprintf("将从磁盘删除「%s」%s%s，此操作不可撤销。", p.name, pathClause, sizeClause)
		}
		return fmt.Sprintf("将从磁盘删除「%s」%s以释放空间，此操作不可撤销。", p.name, pathClause)
	}
}

func (s *viewState) assetDeleteDialog(c *ui.Context, pending *pendingAssetDelete) {
	c.Key("asset-delete-dialog")
	result := components.AlertDialog(c, &pending.open, components.AlertDialogOptions{
		DialogOptions: components.DialogOptions{Label: "删除以释放空间"},
		Title:         "删除以释放空间",
		Message:       assetDeleteConfirmMessage(*pending),
		ConfirmLabel:  "确认删除",
		CancelLabel:   "取消",
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
