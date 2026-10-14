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
	button(c, "下载预置引擎").AlignSelf(ui.Start).Disabled(s.busy || core.DefaultEngineURL() == "").OnClick(func() {
		s.acquireEngine(core.DefaultEngineURL(), fmt.Sprintf("engine-%d", uniqueID()))
	})
	field(c, "引擎 URL", &s.engineURL).Description("必填；引擎归档下载地址。").Error(urlError(s.engineURL))
	field(c, "引擎新目录名", &s.engineName).Description("必填；不会覆盖已有目录。").Error(nameError(s.engineName))
	button(c, "下载引擎").AlignSelf(ui.Start).Disabled(s.busy || !validName(s.engineName) || strings.TrimSpace(s.engineURL) == "").OnClick(func() {
		s.acquireEngine(strings.TrimSpace(s.engineURL), s.engineName)
	})
}

func (s *viewState) acquireEngine(source, name string) {
	if message := urlError(source); message != "" {
		s.submissionFailed(&core.ApiError{Code: "invalid_request", Message: message})
		return
	}
	s.work(func() (string, *core.ApiError) {
		_, e := s.service.CreateEngineDownload(source, name)
		return "引擎已加入下载队列。", e
	})
}

func (s *viewState) adoptDownloadedEngine() {
	if s.engine != "" {
		return
	}
	enginesRoot := filepath.Join(emptyText(s.settings.Effective.WorkspaceRoot), "engines")
	if emptyText(s.settings.Effective.WorkspaceRoot) == "" {
		return
	}
	var latest *core.DownloadTask
	var latestTime time.Time
	for i := range s.downloads {
		task := &s.downloads[i]
		if task.Kind != "engine" || task.State != "done" || emptyText(task.EnginePath) == "" {
			continue
		}
		if rel, err := filepath.Rel(enginesRoot, task.OutDir); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
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
		presentations := map[string][2]string{
			"stopped":  {"已停止", "启动前检查冻结配置的端口，启动后等待健康端点就绪。"},
			"starting": {"正在加载模型", "正在等待健康检查通过，加载期间请稍候。"},
			"running":  {"服务已就绪", "健康端点已响应，可以到运行页绑定服务。"},
			"stopping": {"正在停止服务", "已请求停止，等待进程安全退出。"},
			"failed":   {"服务异常退出", "不会自动重启。检查日志后手动启动，并重新绑定。"},
		}
		code := state.State
		if code == "" {
			code = "stopped"
		}
		presentation, known := presentations[code]
		if !known {
			presentation = [2]string{"未知服务状态", "尚未识别状态码：" + code + "。请刷新后查看诊断。"}
		}
		color := c.Theme().TextMuted
		if code == "running" {
			color = c.Theme().Success
		} else if code == "starting" || code == "stopping" {
			color = c.Theme().Accent
		} else if code == "failed" {
			color = c.Theme().Danger
		}
		label := stateLabel(state.State)
		if state.State == "" {
			label = "未启动"
		}
		statusPanel(c, label, presentation[0], presentation[1], color)
		pid := "无活动进程"
		if state.Pid != 0 {
			pid = fmt.Sprintf("%d", state.Pid)
		}
		endpoint := "未启动"
		if u, err := url.Parse(state.Endpoint); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.User == nil && u.RawQuery == "" && u.Fragment == "" {
			if host := u.Hostname(); host == "localhost" || net.ParseIP(host).IsLoopback() {
				endpoint = state.Endpoint
			}
		}
		kvGrid(c, [][2]string{
			{"冻结配置", "Candidate：" + configuredValue(state.Candidate)},
			{"本地地址", endpoint},
			{"进程", "PID：" + pid},
			{"内存", "缺测 · 此页无内存采样"},
		}, map[string]string{"内存": "此页不采样引擎进程内存，不补零。"})

		errorView(c, state.Error)
		ui.Text(c, "不会自动重启；Studio 只管理自己启动的进程。")
		if s.candidate == "" {
			ui.Text(c, "请先到运行页准备冻结配置。")
			button(c, "到运行页准备配置").AlignSelf(ui.Start).OnClick(func() { s.selected = "runs" })
		}
		if state.State == "starting" || state.State == "running" || state.State == "stopping" {
			ui.Text(c, "重复启动返回当前状态，未创建新进程。")
		}
		button(c, "启动服务").AlignSelf(ui.Start).Disabled(s.busy || s.candidate == "").OnClick(func() {
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
		button(c, "停止服务").AlignSelf(ui.Start).Disabled(s.busy || (state.State != "starting" && state.State != "running")).OnClick(func() {
			s.work(func() (string, *core.ApiError) {
				_, e := s.service.StopEngine()
				return "已请求停止服务。", e
			})
		})
		current := -1
		switch state.State {
		case "", "stopped":
			current = 0
		case "starting":
			current = 1
		case "running":
			current = 2
		case "stopping":
			current = 3
		}
		stepsRow(c, []string{"已停止", "启动中", "服务就绪", "停止中", "已停止"}, current, c.Theme().Accent)
		ui.Text(c, "服务诊断：每秒探测 /health；最长等待 10 分钟；停止先发 SIGTERM，10 秒后仍未退出则终止进程。")
		ui.Text(c, "启动行（安全展示）").FontSize(max(12, c.Theme().Rem(.85))).TextColor(c.Theme().TextMuted)
		deviceMono(c, "LogPath："+configuredValue(state.LogPath))
		deviceMono(c, "Candidate："+configuredValue(state.Candidate))
		ui.Text(c, "从冻结 candidate.toml 的 [engine] 读取；凭据参数与完整进程 argv 不展示。")
	})
}

func (s *viewState) modelBindingView(c *ui.Context) {
	card(c, "绑定存活提示", func() {
		label := "尚未绑定服务"
		if b := s.active(); b != nil && emptyText(b.BoundPath) != "" {
			switch {
			case s.bindingUnknown:
				label = "无法确认存活，不据此判定绑定有效"
			case s.bindingStale:
				label = "原绑定可能已失效，请到运行页重新绑定"
			default:
				label = "存活提示匹配"
			}
		}
		ui.Row(c).Wrap().Gap(c.Theme().Space(3)).AlignItems(ui.Stretch).Children(func() {
			ui.Box(c).Width(3).Background(c.Theme().Accent).Radius(2)
			ui.Column(c).Grow(1).Children(func() { ui.Text(c, label).Bold() })
		})
		ui.Text(c, "存活提示只比对 server_pid / process_start_ticks；身份核验始终由 CLI 在 bind / run 时完成；服务运行中不代表绑定有效。")
		button(c, "到运行页绑定").AlignSelf(ui.Start).OnClick(func() { s.selected = "runs" })
	})
}
