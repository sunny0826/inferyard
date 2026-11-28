package pages

import (
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func (s *viewState) nodeCleanupSettingsView(c *ui.Context) {
	if !s.remoteView() && len(s.nodes.snapshots) <= 1 && !s.hasCachedRemoteReports() {
		return
	}
	t := tokens(c)
	node := s.activeNode()
	cleanCurrentDisabled := s.busy || !node.Online || (node.Record.NodeID == core.LocalNodeID && !s.canCleanWorkspaceArtifacts())
	card(c, tr(c, "nodes.settings.cleanup_title"), func() {
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Section).Children(func() {
			ui.Text(c, trf(c, "nodes.settings.clean_current_desc", "node", nodeDisplayName(node))).
				FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).Grow(1).MinWidth(200)
			if quietDangerButton(c, tr(c, "nodes.settings.clean_current")).Disabled(cleanCurrentDisabled).Clicked() {
				if node.Record.NodeID == core.LocalNodeID {
					s.cleanArtifactsConfirm = true
				} else {
					s.nodes.cleanNodeConfirm = true
				}
			}
		})
		ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Section).Children(func() {
			ui.Text(c, tr(c, "nodes.settings.clean_cache_desc")).
				FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).Grow(1).MinWidth(200)
			if quietDangerButton(c, tr(c, "nodes.settings.clean_cache")).Disabled(s.busy).Clicked() {
				s.nodes.cleanCacheConfirm = true
			}
		})
	})
}

func (s *viewState) nodeCleanupDialogs(c *ui.Context) {
	nodeName := nodeDisplayName(s.activeNode())
	if studioDestructiveAlertDialog(c, &s.nodes.cleanNodeConfirm, components.AlertDialogOptions{
		DialogOptions: components.DialogOptions{Label: tr(c, "nodes.settings.cleanup_title")},
		Title:         trf(c, "nodes.settings.clean_node_title", "node", nodeName),
		Message:       tr(c, "nodes.settings.clean_node_message"),
		ConfirmLabel:  tr(c, "nodes.settings.clean_current"),
		CancelLabel:   tr(c, "common.cancel"),
	}) == components.DialogConfirmed {
		s.cleanCurrentRemoteNode()
	}
	if studioDestructiveAlertDialog(c, &s.nodes.cleanCacheConfirm, components.AlertDialogOptions{
		DialogOptions: components.DialogOptions{Label: tr(c, "nodes.settings.clean_cache_title")},
		Title:         tr(c, "nodes.settings.clean_cache_title"),
		Message:       tr(c, "nodes.settings.clean_cache_message"),
		ConfirmLabel:  tr(c, "nodes.settings.clean_cache"),
		CancelLabel:   tr(c, "common.cancel"),
	}) == components.DialogConfirmed {
		s.cleanRemoteReportCache()
	}
}

func (s *viewState) cleanCurrentRemoteNode() {
	nodeID := s.activeNode().Record.NodeID
	if nodeID == core.LocalNodeID {
		return
	}
	s.workAs("clean-remote-node", func() (string, *core.ApiError) {
		clean := s.nodes.cleanNodeFn
		if clean == nil && s.service != nil {
			clean = s.service.CleanNodeWorkspace
		}
		if clean == nil {
			return "", core.Err("not_found")
		}
		_, err := clean(s.nodeContext(), nodeID)
		if err == nil {
			s.updateView(s.syncNodeState)
		}
		return l10n.TR("nodes.settings.clean_done"), err
	})
}

func (s *viewState) cleanRemoteReportCache() {
	clean := s.nodes.cleanReportCacheFn
	if clean == nil && s.service != nil {
		clean = s.service.CleanRemoteReportCache
	}
	if clean == nil {
		return
	}
	if s.preview != nil {
		_, _ = clean()
		return
	}
	s.workAs("clean-remote-report-cache", func() (string, *core.ApiError) {
		_, err := clean()
		if err == nil {
			s.updateView(s.refreshUnifiedReports)
		}
		return l10n.TR("nodes.settings.cache_clean_done"), err
	})
}
