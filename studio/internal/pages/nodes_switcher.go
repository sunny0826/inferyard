package pages

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/icons/reicon"
)

func nodeDisplayName(node core.NodeSnapshot) string {
	if name := strings.TrimSpace(node.Record.DisplayName); name != "" {
		return name
	}
	if name := strings.TrimSpace(node.Record.Hostname); name != "" {
		return name
	}
	return node.Record.NodeID
}

func nodePlatformLabel(node core.NodeSnapshot) string {
	if hardware := strings.TrimSpace(node.Record.HardwareSummary); hardware != "" {
		return hardware
	}
	platform := strings.TrimSpace(node.Record.Platform)
	if platform == "" {
		platform = strings.TrimSpace(node.Record.OS + " " + node.Record.Arch)
	}
	return platform
}

func (s *viewState) nodeSwitcher(c *ui.Context) {
	t := tokens(c)
	nodes := s.nodes.snapshots
	if len(nodes) == 0 {
		nodes = []core.NodeSnapshot{s.activeNode()}
	}
	selected := 0
	for i, node := range nodes {
		if node.Record.NodeID == s.activeNode().Record.NodeID {
			selected = i
			break
		}
	}
	addIndex := len(nodes)
	choice := studioSelect(c, &selected)
	active := s.activeNode()
	kind := stateSuccess
	if !active.Online {
		kind = stateNeutral
	}
	choice.Trigger.FillWidth().MinWidth(0).Label(tr(c, "nodes.switcher.label")).
		Padding(t.Density.Row, c.Theme().Space(6), t.Density.Row, t.Density.Stack).
		Children(func() {
			ui.Column(c).FillWidth().MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, tr(c, "nodes.switcher.label")).FontSize(t.Type.ShellLabel).TextColor(t.Color.FgTertiary).SingleLine()
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
					ui.Box(c).Size(7, 7).Shrink(0).Radius(t.Radius.Full).Background(stateColor(c, kind))
					ui.Text(c, nodeDisplayName(active)).Bold().FontSize(t.Type.Body).Grow(1).MinWidth(0).SingleLine()
				})
				if label := nodePlatformLabel(active); label != "" {
					ui.Text(c, label).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).MinWidth(0).SingleLine()
				}
			})
		})
	choice.Popup(func(ui.Element) {
		components.ScrollView(c, components.ScrollViewOptions{}).Label(tr(c, "nodes.switcher.list_aria")).MaxHeight(260).Children(func() {
			for i, node := range nodes {
				name := nodeDisplayName(node)
				label := name
				if platform := nodePlatformLabel(node); platform != "" {
					label += " · " + platform
				}
				choice.Item(i).Label(label).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
						dotKind := stateSuccess
						if !node.Online {
							dotKind = stateNeutral
						}
						ui.Box(c).Size(7, 7).Shrink(0).Radius(t.Radius.Full).Background(stateColor(c, dotKind))
						ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
							ui.Text(c, name).FontSize(t.Type.Body).SingleLine()
							ui.Text(c, nodePlatformLabel(node)).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).SingleLine()
						})
					})
				})
			}
			choice.Item(addIndex).Label(tr(c, "nodes.switcher.add")).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
					components.Icon(c, reicon.Plus).Size(14, 14)
					ui.Text(c, tr(c, "nodes.switcher.add")).FontSize(t.Type.Body)
				})
			})
		})
	})
	if choice.Trigger.Changed() {
		if selected == addIndex {
			s.openAddNodeDialog()
		} else if selected >= 0 && selected < len(nodes) {
			s.selectViewNode(nodes[selected].Record.NodeID)
		}
	}
}

func (s *viewState) sidebarTunnelRow(c *ui.Context) {
	tunnels := s.nodes.tunnels
	if len(tunnels) == 0 {
		return
	}
	label := trf(c, "nodes.tunnels.status", "count", len(tunnels))
	s.sidebarStatusRow(c, sidebarStatusSpec{
		key: "sidebar-tunnel-status", label: label, ariaLabel: label,
		badgeText: fmt.Sprintf("%d", len(tunnels)), kind: stateActive, highlight: true, progress: -1,
		leading: func() { components.Icon(c, reicon.Link).Size(14, 14).TextColor(stateColor(c, stateActive)) },
		open:    &s.nodes.tunnelsOpen,
		pop: func(close func()) {
			ui.Column(c).FillWidth().Gap(tokens(c).Density.Row).Children(func() {
				heading(c, tr(c, "nodes.tunnels.title"), 3)
				for _, tunnel := range tunnels {
					s.tunnelRow(c, tunnel)
				}
			})
		},
	})
}
