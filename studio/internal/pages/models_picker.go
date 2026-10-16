package pages

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

// engineLocked reports whether the managed engine is active; the prototype
// locks both pickers while a service is starting, running or stopping.
func engineLocked(state string) bool {
	return state == "starting" || state == "running" || state == "stopping"
}

func (s *viewState) eligibility(name string) core.EngineEligibility {
	e := s.checks.Engines[name]
	if e.State == "" {
		e.State = core.EngineCheckUnknown
	}
	return e
}

func (s *viewState) modelOptions() []pickerOption {
	locked := engineLocked(s.engineState.State)
	options := []pickerOption{}
	for _, m := range s.registry.Models {
		note, disabled := "", locked
		if !m.Available {
			note = "不可用 · " + deviceLabel(m.Reason)
			disabled = true
		}
		options = append(options, pickerOption{value: m.Name, note: note, disabled: disabled})
	}
	return options
}

// engineOptions annotates each engine with the verification verdict only:
// version check plus format, never anything the selected model could change
// (mode flips are carried by the device-check rerun, not by these notes).
func (s *viewState) engineOptions() []pickerOption {
	locked := engineLocked(s.engineState.State)
	options := []pickerOption{}
	for _, e := range s.registry.Engines {
		el := s.eligibility(e.Name)
		note, disabled := "", locked
		switch el.State {
		case core.EngineCheckChecking:
			note, disabled = "核验中…", true
		case core.EngineCheckEligible:
			note = "可用 · 版本已核验"
		case core.EngineCheckIneligible:
			note, disabled = "不可用 · "+human(el.Reason), true
		default:
			if el.Reason == "cli_outdated" {
				// Reviewed degradation: an outdated CLI cannot verify, so the
				// engine stays selectable and config create is the final gate.
				note = "未核验（CLI 过旧，建议升级）"
			} else {
				note, disabled = "未核验", true
				if el.Reason != "" {
					note += " · " + human(el.Reason)
				}
			}
		}
		options = append(options, pickerOption{value: e.Name, note: note, disabled: disabled})
	}
	return options
}

// selectionView is the models page selection zone: two searchable pickers
// with bidirectional compatibility annotations.
func (s *viewState) selectionView(c *ui.Context) {
	errorView(c, s.registry.ModelError)
	if s.registry.Refreshing {
		ui.Text(c, "正在刷新模型清单…")
	}
	if s.running() != nil || engineLocked(s.engineState.State) {
		ui.Text(c, "引擎："+displayName(s.engine))
		ui.Text(c, "模型："+displayName(s.model))
		return
	}
	if len(s.registry.Models) == 0 || len(s.registry.Engines) == 0 {
		ui.Text(c, "暂无可选组合；到下载页下载引擎与模型。")
		button(c, "到下载页").AlignSelf(ui.Start).OnClick(func() { s.selected = "downloads" })
		return
	}
	fields := func() {
		ui.Field(c, "引擎", func() {
			pickerSelect(c, "引擎", "请选择引擎", &s.engine, &s.engineQuery, s.engineOptions(), "没有匹配的引擎，请修改搜索词。")
		}).Description(s.engineFit())
		ui.Field(c, "模型", func() {
			pickerSelect(c, "模型", "请选择模型", &s.model, &s.modelQuery, s.modelOptions(), "没有匹配的模型，请修改搜索词。")
		})
	}
	if compactLayout(c) {
		fields()
	} else {
		ui.Form(c, fields)
	}
	if el := s.eligibility(s.engine); s.engine != "" && el.State == core.EngineCheckIneligible {
		ui.Row(c).Wrap().Gap(c.Theme().Space(2)).Children(func() {
			statusBadge(c, "当前选择不可用", c.Theme().Warning)
			ui.Text(c, s.engine+" · "+human(el.Reason)+"。请重新选择。")
		})
	}

}

func (s *viewState) engineFit() string {
	if !s.checks.Supported && len(s.registry.Engines) > 0 {
		return "未核验（CLI 过旧，建议升级）"
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
