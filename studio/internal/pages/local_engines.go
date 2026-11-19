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

// localServiceChoice is one discovered endpoint that can fill the LM Studio
// manual binding fields. Discovery never grants process ownership; the CLI
// still re-verifies the identity during binding.
type localServiceChoice struct {
	pid      int64
	name     string
	endpoint string
	models   int
}

// localServiceChoices lists discovered endpoints that expose a model catalogue
// and have an unambiguous owner, so a one-click fill cannot paste an unknown
// listener.
func (s *viewState) localServiceChoices() []localServiceChoice {
	choices := []localServiceChoice{}
	for _, service := range s.discovery.Services {
		if service.PID <= 0 {
			continue
		}
		for _, endpoint := range service.Endpoints {
			if endpoint.Models == nil || endpoint.AmbiguousListener || endpoint.URL == "" {
				continue
			}
			choices = append(choices, localServiceChoice{pid: service.PID, name: service.ProcessName, endpoint: endpoint.URL, models: len(endpoint.Models)})
		}
	}
	return choices
}

func (s *viewState) useDiscoveredService(choice localServiceChoice) {
	s.localPID, s.localEndpoint = strconv.FormatInt(choice.pid, 10), choice.endpoint
}

func (s *viewState) localSelectionView(c *ui.Context) {
	if e := s.localEngine(); e != nil {
		ui.Text(c, e.Family+" · 每个测评题集单独连接服务；完成后再进入下一测评题集。").TextColor(c.Theme().TextMuted)
		if e.Family == "lmstudio" {
			ui.Text(c, "先在 LM Studio 启动后台服务并加载所选模型。Studio 只绑定已有服务；请使用后台服务 PID。")
			if choices := s.localServiceChoices(); len(choices) > 0 {
				ui.Text(c, "已发现的本机服务（点击填入 PID 与 endpoint）：").TextColor(c.Theme().TextMuted)
				for _, choice := range choices {
					label := fmt.Sprintf("PID %d · %s · %d 个模型", choice.pid, choice.endpoint, choice.models)
					if name := displayName(choice.name); name != "" {
						label += " · " + name
					}
					button(c, label).OnClick(func() { s.useDiscoveredService(choice) })
				}
			}
			field(c, "LM Studio 后台 PID", &s.localPID).MaxWidth(360)
			field(c, "LM Studio endpoint", &s.localEndpoint).MaxWidth(360).Description("例如 http://127.0.0.1:1234")
		} else {
			ui.Text(c, "Studio 管理本次启动的服务；退出时停止其进程树。")
		}
	}
	if reason := s.localSelectionError(); reason != "" {
		ui.Text(c, reason).TextColor(stateColor(c, stateWarning))
	}
}

func (s *viewState) localInstallationsView(c *ui.Context) {
	card(c, "已有本地引擎", func() {
		ui.Text(c, "注册 CLI 安装声明后，可选择 llama.cpp、MLX、vLLM、SGLang 或 LM Studio。核验安装不代表模型已通过测评。")
		s.pathField(c, "安装声明 JSON 路径", &s.installationPath, browseInstallation).Description("已有 installation.json 的绝对路径；引擎、环境和模型不会自动下载。").Error(s.browseError(browseInstallation.key, ""))
		button(c, "注册引擎").Disabled(s.busy || s.installationPath == "" || pathError(s.installationPath) != "").OnClick(func() { s.changeInstallation("register", s.installationPath) })
		for _, path := range s.settings.Settings.EngineInstallations {
			ui.Row(c.Key(path)).Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.Row).Children(func() {
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
		card(c, "连接下一测评题集的 LM Studio 服务", func() {
			ui.Text(c, emptyText(task.Bundle)+" · 请结束上一测评题集的服务，重新启动后台服务并加载同一模型。CLI 将重新校验身份。")
			if choices := s.localServiceChoices(); len(choices) > 0 {
				ui.Text(c, "已发现的本机服务（点击填入 PID 与 endpoint）：").TextColor(c.Theme().TextMuted)
				for _, choice := range choices {
					label := fmt.Sprintf("PID %d · %s · %d 个模型", choice.pid, choice.endpoint, choice.models)
					if name := displayName(choice.name); name != "" {
						label += " · " + name
					}
					button(c, label).OnClick(func() { s.useDiscoveredService(choice) })
				}
			}
			field(c, "新的后台 PID", &s.localPID).MaxWidth(360)
			field(c, "本机 endpoint", &s.localEndpoint).MaxWidth(360)
			pid, err := strconv.ParseInt(s.localPID, 10, 64)
			button(c, "连接并继续此测评题集").Disabled(err != nil || pid <= 0 || s.localEndpoint == "" || urlError(s.localEndpoint) != "").OnClick(func() {
				if s.simulate(previewLocalBinding{taskID: task.TaskId, pid: pid, endpoint: s.localEndpoint}) {
					return
				}
				s.failure = s.service.BindCandidateService(task.TaskId, pid, s.localEndpoint)
			})
		})
		ui.Text(c, fmt.Sprintf("等待服务 · %s", task.TaskId)).TextColor(c.Theme().TextMuted)
	}
}
