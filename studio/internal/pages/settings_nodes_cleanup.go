package pages

import (
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func (s *viewState) nodeCleanupSettingsView(c *ui.Context) {
	node := s.activeNode()
	card(c, tr(c, "nodes.settings.cleanup_title"), func() {
		ui.Text(c, trf(c, "nodes.settings.clean_current_desc", "node", nodeDisplayName(node))).FontSize(tokens(c).Type.Caption).TextColor(tokens(c).Color.FgSecondary)
		ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
			if dangerButton(c, tr(c, "nodes.settings.clean_current")).Disabled(s.busy || !node.Online).Clicked() {
				if node.Record.NodeID == core.LocalNodeID {
					s.cleanArtifactsConfirm = true
				} else {
					s.nodes.cleanNodeConfirm = true
				}
			}
		})
		ui.Box(c).FillWidth().Height(1).Background(tokens(c).Color.BorderSubtle)
		ui.Text(c, tr(c, "nodes.settings.clean_cache_desc")).FontSize(tokens(c).Type.Caption).TextColor(tokens(c).Color.FgSecondary)
		ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
			if dangerButton(c, tr(c, "nodes.settings.clean_cache")).Disabled(s.busy).Clicked() {
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
