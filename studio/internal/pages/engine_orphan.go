package pages

import (
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func (s *viewState) stopOrphanButton(c *ui.Context) {
	button(c, "停止遗留服务").AlignSelf(ui.Start).Disabled(s.busy || s.running() != nil).OnClick(func() {
		s.work(func() (string, *core.ApiError) {
			state, e := s.service.StopOrphanEngine()
			if state.State == "stopped" {
				return "遗留引擎已退出，已清除记录。", e
			}
			return "已请求停止遗留服务，等待收尾。", e
		})
	})
	if orphanRecordRecoverable(s.engineState) {
		ui.Text(c, "仅移除工作区 logs/engine-owner.json；原进程可能仍在运行，请自行确认。")
		button(c, "仅删除记录（不终止进程）").AlignSelf(ui.Start).Disabled(s.busy || s.running() != nil).OnClick(func() {
			s.work(func() (string, *core.ApiError) {
				_, e := s.service.DiscardOrphanRecord()
				return "已删除遗留记录，未终止任何进程。", e
			})
		})
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
