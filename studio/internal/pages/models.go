package pages

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

var environmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validName(s string) bool { return core.ValidDownloadName(s) }
func quote(s string) string   { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

func (s *viewState) modelsView(c *ui.Context) {
	s.header(c, "模型")
	card(c, "已安装的模型", func() {
		errorView(c, s.registry.ModelError)
		ui.Row(c).Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.CardGap).Children(func() {
			button(c, "下载更多").OnClick(func() { s.selected = "downloads" })
			button(c, "刷新").Disabled(s.registry.Refreshing || s.busy).OnClick(s.refreshAction)
		})
		if s.registry.Refreshing {
			ui.Text(c, "正在读取本地资源…")
		}
		if len(s.registry.Models) == 0 {
			ui.Text(c, "尚未发现本地模型。")
			button(c, "到下载页").OnClick(func() { s.selected = "downloads" })
			return
		}
		for _, model := range s.registry.Models {
			ui.Column(c.Key(model.Name)).Gap(tokens(c).Density.Hairline).Children(func() {
				ui.Text(c, modelLabel(model)).SingleLine()
				ui.Text(c, modelDetails(model)).FontSize(tokens(c).Type.Caption).TextColor(c.Theme().TextMuted)
				modelTaskAction(c, s, model)
			})
		}
	})
	card(c, "已安装的引擎", func() {
		if len(s.registry.Engines) == 0 {
			ui.Text(c, "尚未发现可用引擎。")
			button(c, "到下载页").OnClick(func() { s.selected = "downloads" })
			return
		}
		ui.Text(c, s.engineFit()).TextColor(c.Theme().TextMuted)
		for _, engine := range s.registry.Engines {
			ui.Column(c.Key(engine.Name)).Gap(tokens(c).Density.Hairline).Children(func() {
				ui.Text(c, engine.Name).SingleLine()
				ui.Text(c, s.engineStateLine(engine)).FontSize(tokens(c).Type.Caption).TextColor(c.Theme().TextMuted)
			})
		}
	})
	card(c, "本机运行的服务", func() {
		if len(s.registry.Models) == 0 || len(s.registry.Engines) == 0 {
			s.managedServiceView(c)
			return
		}
		s.discoveryView(c, false)
	})
}

func modelTaskAction(c *ui.Context, s *viewState, model core.ModelAsset) {
	if !model.Available {
		return
	}
	label := "用此模型测评"
	hint := "测评进行中，请在任务页取消后再新建任务"
	blocked := s.busy || s.roundWorkActive()
	b := button(c, label).Label(label + " · " + model.Name).Disabled(blocked)
	if blocked {
		b = b.Description(hint).Tooltip(hint)
	}
	if b.Clicked() && !blocked {
		s.model = model.Name
		s.newBenchmark()
	}
}

func modelDetails(model core.ModelAsset) string {
	parts := []string{}
	if model.Format != "" {
		parts = append(parts, model.Format)
	}
	if model.Architecture != "" {
		parts = append(parts, "架构 "+model.Architecture)
	}
	if model.SizeBytes != nil {
		parts = append(parts, fmt.Sprintf("约 %.1f GiB", float64(*model.SizeBytes)/(1<<30)))
	}
	if model.ContextLength != nil {
		parts = append(parts, fmt.Sprintf("上下文 %d tokens", *model.ContextLength))
	}
	if !model.Available {
		reason := deviceLabel(model.Reason)
		if reason == "" {
			reason = model.Reason
		}
		parts = append(parts, "不可用："+reason)
	}
	if len(parts) == 0 {
		return "未记录额外信息"
	}
	return joinParts(parts)
}

func (s *viewState) engineStateLine(engine core.EngineAsset) string {
	parts := []string{}
	if engine.Family != "" {
		parts = append(parts, engine.Family)
	}
	if engine.Kind == "local_installation" {
		parts = append(parts, "安装声明")
	}
	if engine.Source != "" {
		parts = append(parts, engine.Source)
	}
	el := s.eligibility(engine.Name)
	switch el.State {
	case core.EngineCheckEligible:
		parts = append(parts, "版本已核验")
	case core.EngineCheckChecking:
		parts = append(parts, "核验中…")
	case core.EngineCheckIneligible:
		parts = append(parts, "不可用："+human(el.Reason))
	default:
		if !s.checks.Supported {
			parts = append(parts, "未核验（CLI 过旧）")
		} else if el.Reason != "" {
			parts = append(parts, "未核验："+human(el.Reason))
		} else {
			parts = append(parts, "未核验")
		}
	}
	return joinParts(parts)
}

func joinParts(parts []string) string {
	out := ""
	for i, part := range parts {
		if i > 0 {
			out += " · "
		}
		out += part
	}
	return out
}

func (s *viewState) engineFit() string {
	if !s.checks.Supported && len(s.registry.Engines) > 0 {
		return "当前 CLI 不支持引擎核验，所列引擎均未核验。"
	}
	eligible, ineligible := 0, 0
	for _, e := range s.registry.Engines {
		switch s.eligibility(e.Name).State {
		case core.EngineCheckEligible:
			eligible++
		case core.EngineCheckIneligible:
			ineligible++
		}
	}
	if s.checks.Checking > 0 {
		return fmt.Sprintf("核验中…（%d 个可用 · %d 个不可用）", eligible, ineligible)
	}
	return fmt.Sprintf("%d 个可用 · %d 个不可用", eligible, ineligible)
}

func (s *viewState) adoptDownloadedEngine() {
	if s.engine != "" {
		return
	}
	for _, e := range s.registry.Engines {
		if s.checks.Engines[e.Name].State == core.EngineCheckEligible {
			s.engine = e.Name
			return
		}
	}
	if !s.checks.Supported && len(s.registry.Engines) > 0 {
		s.engine = s.registry.DefaultEngine
	}
}
