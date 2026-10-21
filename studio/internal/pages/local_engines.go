package pages

import (
	"fmt"
	"strconv"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func (s *viewState) localEngine() *core.EngineAsset {
	for i := range s.registry.Engines {
		e := &s.registry.Engines[i]
		if e.Name == s.engine && e.Kind == "local_installation" {
			return e
		}
	}
	return nil
}

func (s *viewState) hasLocalEngine() bool {
	for _, e := range s.registry.Engines {
		if e.Kind == "local_installation" {
			return true
		}
	}
	return false
}

func (s *viewState) localCandidateRequest(bundles []string) *core.LocalCandidateRequest {
	e := s.localEngine()
	if e == nil {
		return nil
	}
	request := &core.LocalCandidateRequest{ModelName: s.model, EngineName: s.engine, Bundles: bundles}
	if e.Family == "lmstudio" {
		request.ServicePID, _ = strconv.ParseInt(s.localPID, 10, 64)
		request.Endpoint = s.localEndpoint
	}
	return request
}

func (s *viewState) compatibleModel(model core.ModelAsset) bool {
	engine := s.localEngine()
	if engine == nil {
		return model.Format != "directory"
	}
	switch engine.Family {
	case "llama-cpp", "lmstudio":
		return model.Format != "directory"
	case "mlx-lm", "vllm", "sglang":
		return model.Format == "directory"
	}
	return false
}

func modelLabel(model core.ModelAsset) string {
	if model.Format == "directory" {
		return model.Name + " · 目录模型"
	}
	if model.Format == "gguf" {
		return model.Name + " · GGUF"
	}
	return model.Name
}

func (s *viewState) localSelectionError() string {
	for _, model := range s.registry.Models {
		if model.Name == s.model && !s.compatibleModel(model) {
			return "模型格式与所选引擎不匹配，请重新选择。"
		}
	}
	if e := s.localEngine(); e != nil {
		if s.eligibility(e.Name).State != core.EngineCheckEligible {
			return "请等待安装声明核验通过。"
		}
		if e.Family == "lmstudio" {
			if pid, err := strconv.ParseInt(s.localPID, 10, 64); err != nil || pid <= 0 || urlError(s.localEndpoint) != "" || s.localEndpoint == "" {
				return "请填写已启动的 LM Studio 后台进程 PID 与本机 endpoint。"
			}
		}
	}
	return ""
}

func (s *viewState) localSelectionView(c *ui.Context) {
	if e := s.localEngine(); e != nil {
		ui.Text(c, e.Family+" · 每个题包单独绑定服务；完成后再进入下一题包。").TextColor(c.Theme().TextMuted)
		if e.Family == "lmstudio" {
			ui.Text(c, "先在 LM Studio 启动后台服务并加载所选模型。Studio 只绑定已有服务；请使用后台服务 PID。")
			field(c, "LM Studio 后台 PID", &s.localPID)
			field(c, "LM Studio endpoint", &s.localEndpoint).Description("例如 http://127.0.0.1:1234")
		} else {
			ui.Text(c, "Studio 管理本次启动的服务；退出时停止其进程树。")
		}
	}
	if reason := s.localSelectionError(); reason != "" {
		ui.Text(c, reason).TextColor(c.Theme().Warning)
	}
}

func (s *viewState) localInstallationsView(c *ui.Context) {
	card(c, "已有本地引擎", func() {
		ui.Text(c, "注册 CLI 安装声明后，可选择 llama.cpp、MLX、vLLM、SGLang 或 LM Studio。核验安装不代表模型已通过测评。")
		field(c, "安装声明 JSON 路径", &s.installationPath).Description("已有 installation.json 的绝对路径；引擎、环境和模型不会自动下载。")
		button(c, "注册引擎").Disabled(s.busy || s.installationPath == "" || pathError(s.installationPath) != "").OnClick(func() { s.changeInstallation("register", s.installationPath) })
		for _, path := range s.settings.Settings.EngineInstallations {
			ui.Row(c.Key(path)).Wrap().Children(func() {
				ui.Text(c, path)
				button(c, "移除注册").Disabled(s.busy).OnClick(func() { s.changeInstallation("remove", path) })
			})
		}
		button(c, "重新核验引擎").Disabled(s.busy).OnClick(func() { s.changeInstallation("recheck", "") })
	})
}

func (s *viewState) changeInstallation(action, path string) {
	if s.simulate(previewInstallation{action: action, path: path}) {
		return
	}
	s.work(func() (string, *core.ApiError) {
		var err *core.ApiError
		switch action {
		case "register":
			_, err = s.service.RegisterEngineInstallation(s.ctx, path)
		case "remove":
			_, err = s.service.RemoveEngineInstallation(path)
		case "recheck":
			s.service.InvalidateEngineChecks()
		default:
			err = core.Err("invalid_request")
		}
		return "本地引擎列表已更新。", err
	})
}

func (s *viewState) awaitingLocalServiceView(c *ui.Context) {
	for _, task := range s.tasks {
		if !task.AwaitingService || terminal(task.State) {
			continue
		}
		if s.localBindingTask != task.TaskId {
			s.localBindingTask, s.localPID = task.TaskId, ""
		}
		card(c, "绑定下一题包的 LM Studio 服务", func() {
			ui.Text(c, emptyText(task.Bundle)+" · 请结束上一题包的服务，重新启动后台服务并加载同一模型。CLI 将重新校验身份。")
			field(c, "新的后台 PID", &s.localPID)
			field(c, "本机 endpoint", &s.localEndpoint)
			pid, err := strconv.ParseInt(s.localPID, 10, 64)
			button(c, "绑定并继续此题包").Disabled(err != nil || pid <= 0 || s.localEndpoint == "" || urlError(s.localEndpoint) != "").OnClick(func() {
				if s.simulate(previewLocalBinding{taskID: task.TaskId, pid: pid, endpoint: s.localEndpoint}) {
					return
				}
				s.failure = s.service.BindCandidateService(task.TaskId, pid, s.localEndpoint)
			})
		})
		ui.Text(c, fmt.Sprintf("等待服务 · %s", task.TaskId)).TextColor(c.Theme().TextMuted)
	}
}
