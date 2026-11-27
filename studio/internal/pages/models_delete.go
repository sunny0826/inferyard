package pages

import (
	"slices"
	"strings"

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
	} else if kind == "engine" && strings.HasSuffix(path, "installation.json") {
		scope = "managed_venv_engine"
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
	case "managed_venv_engine":
		return l10n.TRf("models.delete.confirm.managed_venv_engine", "name", p.name, "pathClause", pathClause, "sizeClause", sizeClause)
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
	result := studioDestructiveAlertDialog(c, &pending.open, components.AlertDialogOptions{
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
