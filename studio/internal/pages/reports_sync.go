package pages

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
)

func (s *viewState) openRemoteReportSync() {
	nodes := s.onlineRemoteNodes()
	if len(nodes) == 0 {
		return
	}
	selectedValid := false
	for _, node := range nodes {
		selectedValid = selectedValid || node.Record.NodeID == s.nodes.reportSyncNodeID
	}
	if !selectedValid {
		s.nodes.reportSyncNodeID = nodes[0].Record.NodeID
	}
	s.nodes.reportSyncOpen = true
}

func (s *viewState) remoteReportSyncDialog(c *ui.Context) {
	studioDialog(c.Key("remote-report-sync"), &s.nodes.reportSyncOpen, components.DialogOptions{Label: tr(c, "nodes.reports.sync")}, func(panel ui.Element) {
		panel.Width(c.Theme().Space(130)).Children(func() {
			ui.Column(c).FillWidth().Gap(tokens(c).Density.CardGap).Children(func() {
				heading(c, tr(c, "nodes.reports.sync"), 2)
				s.remoteReportNodeSelect(c)
				monoField(c, tr(c, "nodes.reports.remote_id"), "", "report-20261128-001", "", &s.nodes.reportSyncID)
				ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
					if primaryActionButton(c, tr(c, "nodes.reports.sync_submit"), s.actionBusy("sync-remote-report")).
						Disabled(s.busy || s.nodes.reportSyncNodeID == "" || strings.TrimSpace(s.nodes.reportSyncID) == "").Clicked() {
						s.syncRemoteReport()
					}
				})
			})
		})
	})
}

func (s *viewState) remoteReportNodeSelect(c *ui.Context) {
	label := tr(c, "nodes.reports.sync_node")
	choice := studioSelect(c, &s.nodes.reportSyncNodeID)
	choice.Trigger.Label(label).Value(s.nodes.reportSyncNodeID).Children(func() {
		ui.Text(c, s.remoteNodeDisplayName(s.nodes.reportSyncNodeID)).SingleLine()
	})
	choice.Popup(func(ui.Element) {
		components.ScrollView(c, components.ScrollViewOptions{}).Label(label).MaxHeight(300).Children(func() {
			for _, node := range s.onlineRemoteNodes() {
				nodeID, name := node.Record.NodeID, nodeDisplayName(node)
				choice.Item(nodeID).Label(name).Children(func() { ui.Text(c, name).SingleLine() })
			}
		})
	})
	choice.Trigger.Changed()
}

func (s *viewState) remoteNodeDisplayName(nodeID string) string {
	for _, node := range s.nodes.snapshots {
		if node.Record.NodeID == nodeID {
			return nodeDisplayName(node)
		}
	}
	return nodeID
}
