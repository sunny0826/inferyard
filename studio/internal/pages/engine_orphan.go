package pages

import (
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func (s *viewState) stopOrphanButton(c *ui.Context) {
	if s.destructiveDialog(c, destructiveOption{
		kind: destructiveStopOrphan, label: "停止遗留服务", confirmLabel: "确认停止遗留服务",
		prompt:   "将终止上次 Studio 留下的服务进程；Studio 不再跟踪该进程的输出。",
		disabled: s.busy || s.running() != nil,
	}) {
		if s.simulate(previewStopEngine) {
			return
		}
		s.work(func() (string, *core.ApiError) {
			state, e := s.service.StopOrphanEngine()
			if state.State == "stopped" {
				return "遗留引擎已退出，已清除记录", e
			}
			return "已请求停止遗留服务，等待收尾", e
		})
	}
	if orphanRecordRecoverable(s.engineState) {
		ui.Text(c, "仅移除工作区 logs/engine-owner.json；原进程可能仍在运行，请自行确认。")
		if s.destructiveDialog(c, destructiveOption{
			kind: destructiveDiscardOrphan, label: "仅删除记录（不终止进程）", confirmLabel: "确认仅删除记录",
			prompt:   "将移除工作区 logs/engine-owner.json，且不会终止原服务进程；若原进程仍在运行，需自行停止。",
			disabled: s.busy || s.running() != nil,
		}) {
			if s.simulate(previewStopEngine) {
				return
			}
			s.work(func() (string, *core.ApiError) {
				_, e := s.service.DiscardOrphanRecord()
				return "已删除遗留记录，未终止任何进程", e
			})
		}
	}
}

func orphanRecordRecoverable(state core.EngineState) bool {
	if state.State != "orphaned" || state.Error == nil {
		return false
	}
	switch state.Error.Code {
	case "engine_owner_invalid", "service_identity_unreadable", "engine_owner_remove_failed":
		return true
	}
	return false
}
