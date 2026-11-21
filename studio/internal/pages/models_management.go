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
	open bool
	kind string
	name string
	path string
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
	if s.preview != nil {
		if kind == "model" {
			s.registry.Models = slices.DeleteFunc(s.registry.Models, func(m core.ModelAsset) bool {
				return m.Name == name
			})
			var total int64
			for _, m := range s.registry.Models {
				if m.SizeBytes != nil && *m.SizeBytes > 0 {
					total += *m.SizeBytes
				}
			}
			s.registry.TotalModelBytes = total
		} else {
			s.registry.Engines = slices.DeleteFunc(s.registry.Engines, func(e core.EngineAsset) bool {
				return e.Name == name
			})
		}
		s.enqueueStatusToast("已模拟删除：" + name)
		return
	}
	if s.service == nil {
		return
	}
	if s.win != nil {
		s.workAs("delete-asset-"+kind+"-"+name, func() (string, *core.ApiError) {
			if err := s.service.DeleteManagedAsset(kind, name); err != nil {
				return "", err
			}
			return "已删除：" + name, nil
		})
		return
	}
	if err := s.service.DeleteManagedAsset(kind, name); err != nil {
		s.submissionFailed(err)
		return
	}
	s.registry = s.service.Registry()
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
		s.assetRowActions(c, "model", m.Name, m.Path, m.Deletable, pending)
	})
}

func (s *viewState) engineRowTrailing(c *ui.Context, eng core.EngineAsset, pending *pendingAssetDelete) {
	t := tokens(c)
	ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
		label, kind := s.engineBadgeState(eng)
		badge(c, label, kind, badgeOptions{Style: badgeDot, Size: sizeSM})
		s.assetRowActions(c, "engine", eng.Name, eng.Path, eng.Deletable, pending)
	})
}

func (s *viewState) assetRowActions(c *ui.Context, kind, name, path string, deletable bool, pending *pendingAssetDelete) {
	if path != "" || s.preview != nil {
		button(c, "打开目录").Label("打开目录 · " + name).Disabled(s.busy).OnClick(func() {
			s.revealAsset(kind, name)
		})
	}
	if deletable {
		if dangerButton(c, "删除").Label("删除 · " + name).Disabled(s.busy).Clicked() {
			*pending = pendingAssetDelete{open: true, kind: kind, name: name, path: path}
		}
	}
}

func (s *viewState) assetDeleteDialog(c *ui.Context, pending *pendingAssetDelete) {
	c.Key("asset-delete-dialog")
	msg := fmt.Sprintf("将从磁盘删除「%s」以释放空间，此操作不可撤销。", pending.name)
	if pending.path != "" {
		msg = fmt.Sprintf("将从磁盘删除「%s」（%s）以释放空间，此操作不可撤销。", pending.name, pending.path)
	}
	result := components.AlertDialog(c, &pending.open, components.AlertDialogOptions{
		DialogOptions: components.DialogOptions{Label: "删除以释放空间"},
		Title:         "删除以释放空间",
		Message:       msg,
		ConfirmLabel:  "确认删除",
		CancelLabel:   "取消",
	})
	switch result {
	case components.DialogConfirmed:
		kind, name := pending.kind, pending.name
		*pending = pendingAssetDelete{}
		s.deleteManagedAsset(kind, name)
	case components.DialogCancelled:
		*pending = pendingAssetDelete{}
	default:
		if !pending.open {
			*pending = pendingAssetDelete{}
		}
	}
}
