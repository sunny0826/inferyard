package pages

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func (s *viewState) engineDownloadView(c *ui.Context) {
	ui.Text(c, "引擎目录："+filepath.Join(emptyText(s.settings.Effective.WorkspaceRoot), "engines"))
	ui.Button(c, "下载预置引擎").AlignSelf(ui.Start).Disabled(s.busy).OnClick(func() {
		s.acquireEngine(core.DefaultEngineURL(), fmt.Sprintf("engine-%d", uniqueID()))
	})
	field(c, "引擎 URL", &s.engineURL)
	field(c, "引擎新目录名", &s.engineName)
	ui.Button(c, "下载引擎").AlignSelf(ui.Start).Disabled(s.busy || !validName(s.engineName) || strings.TrimSpace(s.engineURL) == "").OnClick(func() {
		s.acquireEngine(strings.TrimSpace(s.engineURL), s.engineName)
	})
}

func (s *viewState) acquireEngine(source, name string) {
	s.work(func() (string, *core.ApiError) {
		_, e := s.service.CreateEngineDownload(source, name)
		return "引擎已加入下载队列。", e
	})
}

func (s *viewState) adoptDownloadedEngine() {
	if s.engine != "" {
		return
	}
	var latest *core.DownloadTask
	var latestTime time.Time
	for i := range s.downloads {
		task := &s.downloads[i]
		if task.Kind != "engine" || task.State != "done" || emptyText(task.EnginePath) == "" {
			continue
		}
		finished, _ := time.Parse(time.RFC3339Nano, emptyText(task.FinishedAt))
		if latest == nil || finished.After(latestTime) {
			latest, latestTime = task, finished
		}
	}
	if latest != nil {
		s.engine = *latest.EnginePath
	}
}

func (s *viewState) modelEngineServiceView(c *ui.Context) {
	card(c, "引擎服务", func() {
		state := s.engineState
		label := map[string]string{"": "已停止", "stopped": "已停止", "starting": "启动中", "running": "运行中", "stopping": "停止中", "failed": "失败"}[state.State]
		ui.Text(c, "服务状态："+label)
		pid := "null · 无活动引擎进程"
		if state.Pid != 0 {
			pid = fmt.Sprintf("%d", state.Pid)
		}
		ui.Text(c, "PID："+pid)
		endpoint := "null · 无本地服务地址"
		if u, err := url.Parse(state.Endpoint); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.User == nil && u.RawQuery == "" && u.Fragment == "" {
			if host := u.Hostname(); host == "localhost" || net.ParseIP(host).IsLoopback() {
				endpoint = state.Endpoint
			}
		}
		ui.Text(c, "Endpoint（仅 loopback）："+endpoint).Selectable()
		ui.Text(c, "Candidate："+state.Candidate).Selectable()
		ui.Text(c, "LogPath："+state.LogPath).Selectable()
		errorView(c, state.Error)
		ui.Text(c, "不会自动重启；Studio 只管理自己启动的进程。")
		if s.candidate == "" {
			ui.Text(c, "请先到向导页准备冻结配置。")
		}
		if state.State == "starting" || state.State == "running" || state.State == "stopping" {
			ui.Text(c, "重复启动返回当前状态，未创建新进程。")
		}
		ui.Button(c, "启动服务").AlignSelf(ui.Start).Disabled(s.busy || s.candidate == "").OnClick(func() {
			candidate := s.candidate
			s.work(func() (string, *core.ApiError) {
				before := s.service.Engine()
				result, e := s.service.StartEngine(candidate)
				if before.Pid != 0 && before.Pid == result.Pid && before.LogPath == result.LogPath {
					return "返回当前状态，未创建新进程。", e
				}
				return "服务状态：" + stateLabel(result.State), e
			})
		})
		ui.Button(c, "停止服务").AlignSelf(ui.Start).Disabled(s.busy || (state.State != "starting" && state.State != "running")).OnClick(func() {
			s.work(func() (string, *core.ApiError) {
				_, e := s.service.StopEngine()
				return "已请求停止服务。", e
			})
		})
	})
}

func (s *viewState) modelBindingView(c *ui.Context) {
	card(c, "绑定存活提示", func() {
		label := "尚未绑定服务"
		if b := s.active(); b != nil && emptyText(b.BoundPath) != "" {
			label = "存活提示匹配"
			if s.bindingStale {
				label = "原绑定可能已失效，请到运行页重新绑定"
			}
		}
		ui.Text(c, label)
		ui.Text(c, "存活提示只比对 server_pid / process_start_ticks；身份核验始终由 CLI 在 bind / run 时完成；服务运行中不代表绑定有效。")
	})
}
