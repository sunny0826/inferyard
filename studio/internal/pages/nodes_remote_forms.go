package pages

import (
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/icons/reicon"
)

func (s *viewState) remoteServiceStartCard(c *ui.Context, node core.NodeSnapshot) {
	t := tokens(c)
	toggleIcon := reicon.Plus
	if s.nodes.serviceFormOpen {
		toggleIcon = reicon.ChevronUp
	}
	applyMaterial(c, ui.Column(c.Key("remote-service-start")).FillWidth().Gap(t.Density.Row).Padding(t.Card.Padding), materialCard).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			heading(c, tr(c, "nodes.remote.start_service"), 2).Grow(1).MinWidth(0)
			if outlineIconButton(c, toggleIcon, tr(c, "nodes.remote.start_service")).Disabled(s.busy || !node.Online).Clicked() {
				s.nodes.serviceFormOpen = !s.nodes.serviceFormOpen
				if s.nodes.serviceContext == "" {
					s.nodes.serviceContext = "8192"
				}
			}
		})
		if !node.Online {
			ui.Text(c, tr(c, "nodes.remote.online_required")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		}
		if !s.nodes.serviceFormOpen {
			return
		}
		cols := 2
		if compactLayout(c) {
			cols = 1
		}
		monoField(c, tr(c, "nodes.remote.model_path"), "", "/models/model.gguf", "", &s.nodes.serviceModel)
		monoField(c, tr(c, "nodes.remote.engine_path"), "", "/opt/inferyard/engines/llama-server", "", &s.nodes.serviceEngine)
		ui.Grid(c).Columns(cols).Gap(t.Density.Row).Children(func() {
			monoField(c, tr(c, "nodes.remote.context_size"), "", "8192", "", &s.nodes.serviceContext)
			monoField(c, tr(c, "nodes.remote.port_optional"), "", "48857", "", &s.nodes.servicePort)
		})
		_, valid := parseRemoteServiceRequest(s.nodes.serviceModel, s.nodes.serviceEngine, s.nodes.serviceContext, s.nodes.servicePort)
		ui.Row(c).FillWidth().Justify(ui.End).Gap(t.Density.Row).Children(func() {
			if button(c, tr(c, "common.cancel")).Clicked() {
				s.nodes.serviceFormOpen = false
			}
			if primaryActionButton(c, tr(c, "nodes.remote.start_service_submit"), s.actionBusy("start-remote-service")).
				Disabled(s.busy || !node.Online || !valid).Clicked() {
				s.startRemoteService(node)
			}
		})
	})
}

func (s *viewState) remoteBenchmarkStartCard(c *ui.Context, node core.NodeSnapshot) {
	t := tokens(c)
	toggleIcon := reicon.Plus
	if s.nodes.benchmarkFormOpen {
		toggleIcon = reicon.ChevronUp
	}
	applyMaterial(c, ui.Column(c.Key("remote-benchmark-start")).FillWidth().Gap(t.Density.Row).Padding(t.Card.Padding), materialCard).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			heading(c, tr(c, "nodes.remote.start_benchmark"), 2).Grow(1).MinWidth(0)
			if outlineIconButton(c, toggleIcon, tr(c, "nodes.remote.start_benchmark")).Disabled(s.busy || !node.Online).Clicked() {
				s.nodes.benchmarkFormOpen = !s.nodes.benchmarkFormOpen
			}
		})
		if !node.Online {
			ui.Text(c, tr(c, "nodes.remote.online_required")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		}
		if !s.nodes.benchmarkFormOpen {
			return
		}
		monoField(c, tr(c, "nodes.remote.config_paths"), tr(c, "nodes.remote.config_paths_hint"), "/workspace/configs/benchmark.toml", "", &s.nodes.benchmarkConfigs)
		ui.Row(c).FillWidth().Justify(ui.End).Gap(t.Density.Row).Children(func() {
			if button(c, tr(c, "common.cancel")).Clicked() {
				s.nodes.benchmarkFormOpen = false
			}
			if primaryActionButton(c, tr(c, "nodes.remote.start_benchmark_submit"), s.actionBusy("start-remote-benchmark")).
				Disabled(s.busy || !node.Online || len(splitRemoteConfigPaths(s.nodes.benchmarkConfigs)) == 0).Clicked() {
				s.startRemoteBenchmark(node)
			}
		})
	})
}
