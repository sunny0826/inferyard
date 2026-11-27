package pages

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

type previewEngineUpdateMode interface {
	checkEngineUpdates()
	startEngineUpdate(eng core.EngineAsset, upd core.EngineUpdateInfo)
}

func (s *viewState) applyEngineUpdatesStatus(status core.EngineUpdatesStatus, manual bool) {
	s.engineUpdateStatus = status
	if status.Updates != nil {
		s.engineUpdates = status.Updates
	}
	if status.State != core.EngineUpdateCheckReady || status.UpdatableCount <= 0 {
		return
	}
	sig, labels := engineUpdatesNotificationSummary(status.Updates)
	if sig == "" || sig == s.engineUpdateNotifiedSig {
		return
	}
	s.engineUpdateNotifiedSig = sig
	if manual {
		return
	}
	s.pushNotification(studioNotification{
		Key:   "engine-updates:" + sig,
		Title: l10n.TR("notif.engine_updates.available.title"),
		Body: l10n.TRf("notif.engine_updates.available.body",
			"count", status.UpdatableCount,
			"engines", strings.Join(labels, ", "),
		),
		Kind: stateInfo,
		Primary: &notificationAction{
			Label: l10n.TR("notif.action.view_engines"),
			OnClick: func() {
				s.libraryTab = 1
				s.navigateTo("library", "")
			},
		},
	})
}

func engineUpdatesNotificationSummary(updates map[string]core.EngineUpdateInfo) (string, []string) {
	type groupItem struct {
		key    string
		label  string
		latest string
	}
	groups := map[string]groupItem{}
	covered := map[string]bool{}
	for _, info := range updates {
		gKey := info.Family
		if gKey == "" {
			gKey = info.Engine
		}
		if info.Distribution != "" {
			gKey += ":" + info.Distribution
		}
		if !info.HasUpdate {
			if strings.TrimSpace(info.InstalledVersion) != "" && strings.TrimSpace(info.LatestVersion) != "" {
				covered[gKey] = true
			}
			continue
		}
		label := engineFamilyTitle(info.Family, info.Engine)
		if info.LatestVersion != "" {
			label = fmt.Sprintf("%s %s", label, info.LatestVersion)
		}
		groups[gKey] = groupItem{key: gKey, label: label, latest: info.LatestVersion}
	}
	var sigParts []string
	var labels []string
	keys := make([]string, 0, len(groups))
	for k := range groups {
		if covered[k] {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		item := groups[k]
		sigParts = append(sigParts, item.key+"="+item.latest)
		labels = append(labels, item.label)
	}
	return strings.Join(sigParts, "|"), labels
}

func (s *viewState) engineUpdateChecking() bool {
	return s.engineUpdateStatus.State == core.EngineUpdateCheckChecking || s.actionBusy("check-engine-updates")
}

func (s *viewState) engineUpdateTaskActive(eng core.EngineAsset, upd core.EngineUpdateInfo) bool {
	for _, task := range s.downloads {
		if task.State != "downloading" {
			continue
		}
		switch task.Kind {
		case "engine_update":
			if task.OutName == eng.Name {
				return true
			}
		case "engine":
			if upd.DownloadURL != "" && task.SourceUrl == upd.DownloadURL {
				return true
			}
		case "polystrata":
			if upd.Family == "polystrata" {
				return true
			}
		}
	}
	return false
}

func (s *viewState) checkEngineUpdatesAction() {
	if s.preview != nil {
		if pm, ok := s.preview.(previewEngineUpdateMode); ok {
			pm.checkEngineUpdates()
		}
		return
	}
	checkFn := s.engineUpdateCheckFn
	if checkFn == nil {
		if s.service == nil {
			return
		}
		checkFn = s.service.CheckEngineUpdates
	}
	s.workAs("check-engine-updates", func() (string, *core.ApiError) {
		ctx := s.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		status, err := checkFn(ctx)
		if err != nil {
			return "", err
		}
		if s.updateWindow != nil {
			s.updateWindow(func() { s.applyEngineUpdatesStatus(status, true) })
		} else if s.win != nil {
			s.win.Update(func() { s.applyEngineUpdatesStatus(status, true) })
		}
		if status.UpdatableCount > 0 {
			return l10n.TRf("models.engine.toast.check_found", "count", status.UpdatableCount), nil
		}
		return l10n.TR("models.engine.toast.check_up_to_date"), nil
	})
}

func (s *viewState) startOneClickEngineUpdate(eng core.EngineAsset, upd core.EngineUpdateInfo) {
	if s.preview != nil {
		if pm, ok := s.preview.(previewEngineUpdateMode); ok {
			pm.startEngineUpdate(eng, upd)
		}
		return
	}
	runFn := s.engineUpdateRunFn
	if runFn == nil {
		if s.service == nil {
			return
		}
		runFn = s.service.UpdateEngine
	}
	s.workDownload(func() (string, *core.ApiError) {
		_, err := runFn(eng.Name)
		if err != nil {
			return "", err
		}
		return l10n.TRf("models.engine.toast.update_started", "name", eng.Name), nil
	})
}

func (s *viewState) renderEngineUpdateCheckButton(c *ui.Context) {
	checking := s.engineUpdateChecking()
	refreshIconButton(c, tr(c, "models.engine.action.check_updates"), checking).
		Disabled(s.busy || checking).
		OnClick(s.checkEngineUpdatesAction)
}
