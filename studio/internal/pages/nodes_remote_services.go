package pages

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/icons/reicon"
	"inferyard/studio/internal/l10n"
)

func (s *viewState) remoteServicesView(c *ui.Context) {
	t := tokens(c)
	node := s.activeNode()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
		s.header(c, tr(c, "services.page.title"))
		ui.Box(c).Grow(1)
		kind, label := stateSuccess, tr(c, "nodes.remote.online")
		if !node.Online {
			kind, label = stateNeutral, tr(c, "nodes.remote.offline")
		}
		inlineBadge(c, label, kind, badgeOptions{Size: sizeSM, Style: badgeDot})
	})
	for _, notice := range node.ExitNotices {
		s.remoteExitNoticeCard(c, notice)
	}
	if len(node.Services) == 0 {
		emptyGuideCard(c, emptyGuideOptions{Icon: reicon.Server, Title: tr(c, "nodes.remote.services_empty"), Surface: emptyCard})
		return
	}
	for _, service := range node.Services {
		s.remoteServiceCard(c, node, service)
	}
}

func (s *viewState) remoteServiceCard(c *ui.Context, node core.NodeSnapshot, service core.EngineState) {
	t := tokens(c)
	model := s.serviceModelTitle(c, service)
	tunnel := s.tunnelForService(node.Record.NodeID, service.ServiceID)
	applyMaterial(c, ui.Column(c.Key("remote-service-"+service.ServiceID)).FillWidth().Gap(t.Density.Stack).
		Padding(t.Card.Padding), materialCard).Children(func() {
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			modelIconBox(c, s.modelIconHint(model), 30, t.Radius.Inset)
			ui.Text(c, model).Bold().FontSize(t.Type.CardTitle)
			inlineBadge(c, serviceEngineTitle(service), stateNeutral, badgeOptions{Size: sizeSM})
			inlineBadge(c, stateLabelFor(contextLocale(c), service.State), engineStateKind(service.State), badgeOptions{Size: sizeSM, Style: badgeDot})
			ui.Box(c).Grow(1)
			if node.Online {
				if outlineIconButton(c, reicon.FileText, tr(c, "nodes.remote.logs")).Clicked() {
					s.openRemoteServiceLog(node.Record.NodeID, service)
				}
				if dangerIconButton(c, reicon.Square, tr(c, "nodes.remote.stop")).Disabled(s.busy || service.State != "running").Clicked() {
					s.stopRemoteService(node.Record.NodeID, service.ServiceID)
				}
			}
		})
		ui.Grid(c).Columns(3).Gap(t.Density.Row).Children(func() {
			serviceMetricTile(c, t.Radius.Inset, tr(c, "nodes.remote.remote_port"), fmt.Sprintf("127.0.0.1:%d", service.Port), "", true)
			serviceMetricTile(c, t.Radius.Inset, tr(c, "services.card.metric.memory"), serviceMemoryDisplay(c, service), "", true)
			serviceMetricTile(c, t.Radius.Inset, tr(c, "services.card.metric.status"), stateLabelFor(contextLocale(c), service.State), nodeDisplayName(node), false)
		})
		s.remoteTunnelBar(c, node.Record.NodeID, service.ServiceID, service.Port)
		studioTooltipGroup(c, ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row), func() {
			usageLabel := tr(c, "services.card.action.usage")
			usage := outlineIconButton(c, reicon.Code, usageLabel).Disabled(tunnel == nil)
			if tunnel == nil {
				usage = iconButtonWithTooltip(c, tr(c, "nodes.remote.usage_requires_tunnel"), func() ui.Element {
					return outlineIconButton(c, reicon.Code, "").Label(usageLabel).Disabled(true)
				})
			}
			if usage.Clicked() && tunnel != nil {
				apiModel := strings.TrimSpace(service.ServedModel)
				if apiModel == "" {
					apiModel = model
				}
				s.openServiceUsageDrawer(tunnel.LocalURL, apiModel, serviceEngineTitle(service))
			}
		})
	})
}

func (s *viewState) remoteExitNoticeCard(c *ui.Context, notice core.ServiceExitNotice) {
	t := tokens(c)
	scale := stateScale(c, stateDanger)
	ui.Column(c).FillWidth().Gap(t.Density.Row).Padding(t.Card.Padding).Radius(t.Radius.Surface).
		Background(scale.SubtleBg).Border(1, scale.Border).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			components.Icon(c, reicon.CircleAlert).Size(16, 16).TextColor(scale.Fg)
			ui.Text(c, tr(c, "nodes.remote.exit_title")).Bold().TextColor(scale.Fg)
		})
		if notice.ExitCode != nil {
			ui.Text(c, trf(c, "nodes.remote.exit_code", "code", *notice.ExitCode)).FontFeatures("tnum").TextColor(scale.Fg)
		}
		if len(notice.LogTail) > 0 {
			ui.Text(c, strings.Join(notice.LogTail, "\n")).FontSize(t.Type.Caption).Selectable()
		}
	})
}

func (s *viewState) stopRemoteService(nodeID, serviceID string) {
	s.workAs("stop-remote-service", func() (string, *core.ApiError) {
		_, err := s.service.StopNodeService(s.nodeContext(), nodeID, serviceID)
		if err != nil {
			return "", err
		}
		s.updateView(s.syncNodeState)
		return l10n.TR("models.serve.toast.stopping"), nil
	})
}

func (s *viewState) openRemoteServiceLog(nodeID string, service core.EngineState) {
	s.nodes.logTitle = s.serviceModelTitle(nil, service)
	s.nodes.logText = tr(nil, "reports.list.loading")
	s.nodes.logOpen = true
	go func() {
		logs, err := s.service.GetNodeServiceLogs(s.nodeContext(), nodeID, service.ServiceID, 200)
		s.updateView(func() {
			if err != nil {
				s.nodes.logText = err.Message
				return
			}
			s.nodes.logText = logs
		})
	}()
}
