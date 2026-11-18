package pages

import (
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func (s *viewState) stopOrphanButton(c *ui.Context) {
	if s.destructiveAction(c, destructiveOption{
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
				return "遗留引擎已退出，已清除记录。", e
			}
			return "已请求停止遗留服务，等待收尾。", e
		})
	}
	if orphanRecordRecoverable(s.engineState) {
		ui.Text(c, "仅移除工作区 logs/engine-owner.json；原进程可能仍在运行，请自行确认。")
		if dangerButton(c, "仅删除记录（不终止进程）").Disabled(s.busy || s.running() != nil).Clicked() {
			if s.simulate(previewStopEngine) {
				return
			}
			s.work(func() (string, *core.ApiError) {
				_, e := s.service.DiscardOrphanRecord()
				return "已删除遗留记录，未终止任何进程。", e
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
