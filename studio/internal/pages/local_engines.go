package pages

import (
	"fmt"
	"strconv"
	"time"

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
		return model.Format != "directory" && model.Format != "strata_pack"
	}
	switch engine.Family {
	case "llama-cpp", "lmstudio":
		return model.Format != "directory" && model.Format != "strata_pack"
	case "polystrata":
		return model.Format == "gguf" || model.Format == "strata_pack"
	case "mlx-lm", "vllm", "sglang":
		return model.Format == "directory"
	}
	return false
}

func modelLabel(model core.ModelAsset) string {
	switch model.Format {
	case "directory":
		return model.Name + " · 目录模型"
	case "gguf":
		return model.Name + " · GGUF"
	case "strata_pack":
		return model.Name + " · PolyStrata"
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
				return "请填写已启动的 LM Studio 后台进程 PID 与本机服务地址。"
			}
		}
	}
	return ""
}

type localServiceChoice struct {
	pid      int64
	name     string
	endpoint string
	models   int
}

func (s *viewState) localExpectedEndpoint() string {
	if s.task.start.localRequest != nil && s.task.start.localRequest.Endpoint != "" {
		return s.task.start.localRequest.Endpoint
	}
	return s.startup.Endpoint
}

func (s *viewState) localServiceChoices() []localServiceChoice {
	choices := []localServiceChoice{}
	expected := ""
	if s.awaitingLocalService() {
		expected = s.localExpectedEndpoint()
	}
	for _, service := range s.discovery.Services {
		if service.PID <= 0 || s.localPIDUsed(service.PID) {
			continue
		}
		for _, endpoint := range service.Endpoints {
			if endpoint.Models == nil || endpoint.AmbiguousListener || endpoint.URL == "" {
				continue
			}
			if expected != "" && endpoint.URL != expected {
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

func (s *viewState) localPIDUsed(pid int64) bool {
	if s.localUsedPIDs[pid] {
		return true
	}
	for _, reserved := range s.localPendingPIDs {
		if reserved == pid {
			return true
		}
	}
	return s.initialPIDHeld(pid)
}

func (s *viewState) initialPIDHeld(pid int64) bool {
	request := s.task.start.localRequest
	if request == nil || request.ServicePID != pid || len(request.Bundles) == 0 {
		return false
	}
	task := s.currentRoundTask(request.Bundles[0])
	if task == nil || task.AwaitingService {
		return false
	}
	if task.ConfigPath != nil && *task.ConfigPath != "" {
		return true
	}
	return task.State != "failed" && task.State != "cancelled"
}

func (s *viewState) currentRoundTask(bundle string) *core.RunTask {
	var id string
	for taskID, name := range s.roundTaskIds() {
		if name == bundle {
			id = taskID
		}
	}
	if id == "" {
		return nil
	}
	return localPendingTask(s.tasks, id)
}

func (s *viewState) roundTaskIds() map[string]string {
	if s.task.bindings == nil || len(s.task.bindings.localTasks) == 0 {
		return nil
	}
	return s.task.bindings.localTasks
}

func (s *viewState) localSelectionInvalid(pid int64, err error) bool {
	if err != nil || pid <= 0 || s.localEndpoint == "" || urlError(s.localEndpoint) != "" {
		return true
	}
	if expected := s.localExpectedEndpoint(); expected != "" && s.localEndpoint != expected {
		return true
	}
	return s.localPIDUsed(pid)
}

func (s *viewState) localServicePicker(c *ui.Context, pidLabel, endpointLabel string) {
	ui.Row(c).Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.Row).Children(func() {
		refreshButton(c, "刷新服务列表", s.discovering).Disabled(s.discovering).OnClick(s.refreshDiscovery)
	})
	if choices := s.localServiceChoices(); len(choices) > 0 {
		ui.Text(c, "已发现的本机服务（点击填入 PID 与服务地址）：").TextColor(c.Theme().TextMuted)
		for _, choice := range choices {
			label := fmt.Sprintf("PID %d · %s · %d 个模型", choice.pid, choice.endpoint, choice.models)
			if name := displayName(choice.name); name != "" {
				label += " · " + name
			}
			button(c, label).OnClick(func() { s.useDiscoveredService(choice) })
		}
	}
	field(c, pidLabel, &s.localPID).MaxWidth(360)
	field(c, endpointLabel, &s.localEndpoint).MaxWidth(360).Description("例如 http://127.0.0.1:1234")
}

func (s *viewState) localSelectionView(c *ui.Context) {
	if e := s.localEngine(); e != nil {
		ui.Text(c, e.Family+" · 每个测评题集单独连接服务；完成后再进入下一测评题集。").TextColor(c.Theme().TextMuted)
		if e.Family == "lmstudio" {
			ui.Text(c, "先在 LM Studio 启动后台服务并加载所选模型。Studio 只绑定已有服务；请使用后台服务 PID。")
			s.localServicePicker(c, "LM Studio 后台 PID", "LM Studio 服务地址")
		} else {
			ui.Text(c, "Studio 管理本次启动的服务；退出时停止其进程树。")
		}
	}
	if reason := s.localSelectionError(); reason != "" {
		noticeBanner(c, noticeBannerOptions{Kind: stateWarning, Size: bannerCompact, Message: reason})
	}
}

func (s *viewState) localInstallationsView(c *ui.Context) {
	card(c, "已有本地引擎", func() {
		s.localEngineWizardView(c)
		componentFold(c, "高级：直接导入已有 installation.json", func() {
			ui.Text(c, "注册本地引擎安装声明（支持 PolyStrata、llama.cpp、MLX、vLLM、SGLang、LM Studio）。")
			componentFold(c, "installation.json 字段说明", func() {
				ui.Text(c, "声明 definition 为 local-engine-installation.v1，填写 family、executable、working_directory、runtime_roots 与 environment 绝对路径。")
			})
			s.pathField(c, "安装声明 JSON 路径", &s.installationPath, browseInstallation).
				Description("已准备好的 installation.json 绝对路径。").
				Error(s.browseError(browseInstallation.key, pathError(s.installationPath)))
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.Row).Children(func() {
				if primaryActionButton(c, "导入并注册", s.actionBusy("install-register")).Disabled(s.busy || s.installationPath == "" || pathError(s.installationPath) != "").Clicked() {
					s.changeInstallation("register", s.installationPath)
				}
			})
		})
		for _, path := range s.settings.Settings.EngineInstallations {
			path := path
			ui.Row(c.Key(path)).Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.Row).Children(func() {
				ui.Text(c, path)
				if dangerButton(c, "移除注册").AlignSelf(ui.Start).Disabled(s.busy).Clicked() {
					s.removeInstallationWithUndo(path)
				}
			})
		}
	})
}

func (s *viewState) removeInstallationWithUndo(path string) {
	if s.simulate(previewInstallation{action: "remove", path: path}) {
		return
	}
	if s.service == nil {
		return
	}
	s.workAs("install-remove-"+path, func() (string, *core.ApiError) {
		_, err := s.service.RemoveEngineInstallation(path)
		if err == nil {
			removedPath := path
			s.enqueueActionToast("undo-remove-installation", "已移除引擎注册", "撤销", "已恢复引擎注册", 6*time.Second, func() {
				s.changeInstallation("register", removedPath)
			})
		}
		return "", err
	})
}

func (s *viewState) changeInstallation(action, path string) {
	if s.simulate(previewInstallation{action: action, path: path}) {
		return
	}
	key := "install-" + action
	if action == "remove" {
		key = "install-remove-" + path
	}
	s.workAs(key, func() (string, *core.ApiError) {
		var err *core.ApiError
		message := "本地引擎列表已更新"
		switch action {
		case "register":
			_, err = s.service.RegisterEngineInstallation(s.ctx, path)
			message = "已注册本地引擎"
		case "remove":
			_, err = s.service.RemoveEngineInstallation(path)
			message = "已移除引擎注册"
		case "recheck":
			s.service.InvalidateEngineChecks()
			message = "已开始重新核验引擎"
		default:
			err = core.Err("invalid_request")
		}
		return message, err
	})
}

func (s *viewState) awaitingLocalServiceView(c *ui.Context) {
	for _, task := range s.tasks {
		if !task.AwaitingService || terminal(task.State) {
			continue
		}
		if s.localBindingTask != task.TaskId {
			s.localBindingTask, s.localPID, s.handoffFailure = task.TaskId, "", nil
		}
		card(c, "连接下一测评题集的 LM Studio 服务", func() {
			ui.Text(c, emptyText(task.Bundle)+" · 请结束上一测评题集的服务，重新启动后台服务并加载同一模型。CLI 将重新校验身份。")
			if s.handoffFailure != nil {
				failureView(c, "连接此测评题集失败", s.handoffFailure, "服务就绪后可用同一 PID 重试。")
			}
			s.localServicePicker(c, "新的后台 PID", "本机服务地址")
			pid, err := strconv.ParseInt(s.localPID, 10, 64)
			if err == nil && pid > 0 && s.localPIDUsed(pid) {
				noticeBanner(c, noticeBannerOptions{Kind: stateWarning, Size: bannerCompact, Message: human("local_engine_service_reused")})
			}
			button(c, "连接并继续此测评题集").Disabled(s.localSelectionInvalid(pid, err)).OnClick(func() {
				s.connectHandoffService(task.TaskId, pid)
			})
		})
		ui.Text(c, fmt.Sprintf("等待服务 · %s", task.TaskId)).TextColor(c.Theme().TextMuted)
	}
}

func (s *viewState) connectHandoffService(taskID string, pid int64) {
	if s.simulate(previewLocalBinding{taskID: taskID, pid: pid, endpoint: s.localEndpoint}) {
		return
	}
	if e := s.service.BindCandidateService(taskID, pid, s.localEndpoint); e != nil {
		s.handoffFailure = e
		s.failure = e
		return
	}
	s.handoffFailure = nil
	s.failure = nil
	s.reserveLocalPID(taskID, pid)
}

func (s *viewState) reserveLocalPID(taskID string, pid int64) {
	if taskID == "" || pid <= 0 {
		return
	}
	if s.localPendingPIDs == nil {
		s.localPendingPIDs = map[string]int64{}
	}
	s.localPendingPIDs[taskID] = pid
}

func (s *viewState) reconcileLocalPIDs(tasks []core.RunTask) {
	for taskID, pid := range s.localPendingPIDs {
		task := localPendingTask(tasks, taskID)
		if task == nil {
			delete(s.localPendingPIDs, taskID)
			continue
		}
		if task.ConfigPath != nil && *task.ConfigPath != "" {
			s.markServiceUsed(pid)
			delete(s.localPendingPIDs, taskID)
			continue
		}
		if task.Error != nil || (!task.AwaitingService && task.PreparationStage == "") {
			delete(s.localPendingPIDs, taskID)
		}
	}
}

func localPendingTask(tasks []core.RunTask, taskID string) *core.RunTask {
	for i := range tasks {
		if tasks[i].TaskId == taskID {
			return &tasks[i]
		}
	}
	return nil
}
