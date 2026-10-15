package pages

import (
	"fmt"
	"net"
	"net/url"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

// runPanelView is the models page run zone. It renders only when both pickers
// have a selection: a status badge mapped from the managed engine state, a
// start/stop pair, and honest hints for the gate, lock and attribution states.
// No binding UI appears here.
func (s *viewState) runPanelView(c *ui.Context) {
	t := c.Theme()
	state := s.engineState
	ui.Text(c, s.model+" × "+s.engine).Bold()

	gated := !s.preflightReady()
	active := state.State == "starting" || state.State == "running" || state.State == "stopping"
	other, unknown := false, false
	if active {
		match, known := s.comboAttribution()
		other, unknown = known && !match, !known
	}
	runActive := s.running() != nil

	badge, color := "未启动", t.TextMuted
	switch {
	case state.State == "orphaned":
		badge, color = "遗留引擎", t.Warning
	case active && other:
		badge, color = fmt.Sprintf("%s · 另一个组合的服务（候选 %s）", stateLabel(state.State), displayName(state.Candidate)), t.Success
	case active && unknown:
		badge, color = fmt.Sprintf("%s · 无法确认属于当前组合（候选 %s）", stateLabel(state.State), displayName(state.Candidate)), t.Warning
	case state.State == "starting":
		badge, color = "启动中…", t.Accent
	case state.State == "stopping":
		badge, color = "停止中…", t.Accent
	case state.State == "running":
		badge, color = fmt.Sprintf("运行中 · PID %d · %s", state.Pid, loopbackEndpoint(state.Endpoint)), t.Success
	case state.State == "failed":
		badge, color = failureBadge(state), t.Danger
	case state.State == "stopped":
		badge = "已停止"
	}
	statusBadge(c, badge, color)

	if el := s.eligibility(s.engine); el.State == core.EngineCheckIneligible {
		ui.Text(c, s.engine+" · "+human(el.Reason)+"。请重新选择。").TextColor(t.Warning)
	}

	start := s.startEnabled(gated, active, other, unknown, runActive)
	stop := s.stopEnabled(active, runActive)
	ui.Row(c).Wrap().Gap(t.Space(2.5)).Children(func() {
		if primaryButton(c, "启动").AlignSelf(ui.Start).Disabled(!start).Clicked() {
			s.startCombination()
		}
		if button(c, "停止").AlignSelf(ui.Start).Disabled(!stop).Clicked() {
			s.stopEngine()
		}
	})

	switch {
	case state.State == "orphaned":
		ui.Text(c, "检测到上次 Studio 留下的引擎；核验启动标识后可显式停止。处理前不能启动新服务。")
		s.stopOrphanButton(c)
	case gated:
		ui.Text(c, "设备检查未通过：请先到 Dashboard 完成设备检查，再启动服务。").TextColor(t.Warning)
		button(c, "到 Dashboard 完成设备检查").AlignSelf(ui.Start).OnClick(func() { s.selected = "dashboard" })
	case runActive:
		ui.Text(c, "测评运行期间不能停止或重新启动服务。")
	case other:
		ui.Text(c, "当前选定组合没有运行中的服务；停止将终止另一个组合正在运行的服务。")
	case unknown && active:
		ui.Text(c, "无法确认运行中服务属于当前选定组合；停止将终止正在运行的服务。")
	}
	if state.State == "failed" && state.Error != nil {
		errorView(c, state.Error)
		ui.Text(c, "查看日志后可重新启动。").TextColor(t.TextMuted)
	}
	ui.Text(c, "Studio 启动的服务随 Studio 退出关闭，不会自动重启。").TextColor(t.TextMuted)
}

// failureBadge shows only the real fixed reason produced by the engine
// supervisor; unknown codes fall back to the generic human mapping, never an
// invented cause.
func failureBadge(state core.EngineState) string {
	if state.Error == nil {
		return "失败"
	}
	return "失败：" + state.Error.Code + " · " + human(state.Error.Code)
}

// preflightReady reports whether progress reached the candidate stage: the
// latest device preflight exists and the workspace is not still waiting for a
// device check.
func (s *viewState) preflightReady() bool {
	p := s.progress
	return p != nil && p.DevicePreflight.Status == "ok" && p.DevicePreflight.Path != nil && p.NextAction != "device_check"
}

func (s *viewState) startEnabled(gated, active, other, unknown, runActive bool) bool {
	return s.eligibility(s.engine).State != core.EngineCheckIneligible && !s.busy && !gated && !active && !other && !unknown && !runActive && s.engineState.State != "orphaned" && s.ready.CLI.Status == "ok"
}

func (s *viewState) stopEnabled(active, runActive bool) bool {
	if !active || runActive || s.busy {
		return false
	}
	return s.engineState.State == "starting" || s.engineState.State == "running"
}

// comboAttribution memoizes candidate matching per (candidate, model, engine)
// triple; each lookup reads one candidate.toml through core, never a path.
func (s *viewState) comboAttribution() (match, known bool) {
	key := s.engineState.Candidate + "|" + s.model + "|" + s.engine
	if s.attributionKey == key {
		return s.attributionMatch, s.attributionKnown
	}
	f := s.candidateMatches
	if f == nil {
		f = s.service.CandidateMatchesCombination
	}
	match, known = f(s.engineState.Candidate, s.model, s.engine)
	s.attributionKey, s.attributionMatch, s.attributionKnown = key, match, known
	return match, known
}

// loopbackEndpoint displays only loopback service endpoints.
func loopbackEndpoint(raw string) string {
	if u, err := url.Parse(raw); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.User == nil && u.RawQuery == "" && u.Fragment == "" {
		if host := u.Hostname(); host == "localhost" || net.ParseIP(host).IsLoopback() {
			return raw
		}
	}
	return "非本机地址（不显示）"
}
