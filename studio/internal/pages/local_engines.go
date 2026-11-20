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

// localExpectedEndpoint is the endpoint frozen into the current candidates. A
// connection to another address would be rejected with
// candidate_endpoint_mismatch, so it is never offered.
func (s *viewState) localExpectedEndpoint() string {
	if s.task.start.localRequest != nil && s.task.start.localRequest.Endpoint != "" {
		return s.task.start.localRequest.Endpoint
	}
	return s.startup.Endpoint
}

// localServiceChoices lists discovered endpoints that expose a model catalogue,
// have an unambiguous owner and are not already connected. During the handoff
// they must also match the frozen candidate endpoint, because any other address
// would be rejected. Fresh task setup has no frozen candidate yet, so a leftover
// endpoint from an earlier round must not hide a newly started service.
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

// localPIDUsed reports an LM Studio service this round already verified, or one
// whose verification is still in flight. The service also rejects reuse, but the
// form should not invite it. Submitting the form is not verification: a rejected
// first bundle releases its service so the same PID can be retried.
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

// initialPIDHeld reports whether the first bundle's form service still belongs
// to this round. It is resolved through the round's own task id, never by bundle
// name: RunTasks is newest-first, so a previous round's task for the same bundle
// would otherwise answer for this one. Once the CLI verified the binding
// (ConfigPath) the service stays consumed even if the run later fails; a task
// that ended without a verified binding returns it to the user.
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

// currentRoundTask resolves this round's task for one bundle by id, so tasks
// from earlier rounds never stand in for it.
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

// roundTaskIds maps this round's local task ids to their bundles.
func (s *viewState) roundTaskIds() map[string]string {
	if s.task.bindings == nil || len(s.task.bindings.localTasks) == 0 {
		return nil
	}
	return s.task.bindings.localTasks
}

// localSelectionInvalid disables the handoff connect button for the same
// reasons the service rejects the request: a missing PID, an invalid endpoint,
// a mismatched frozen endpoint or an already connected service.
func (s *viewState) localSelectionInvalid(pid int64, err error) bool {
	if err != nil || pid <= 0 || s.localEndpoint == "" || urlError(s.localEndpoint) != "" {
		return true
	}
	if expected := s.localExpectedEndpoint(); expected != "" && s.localEndpoint != expected {
		return true
	}
	return s.localPIDUsed(pid)
}

// localServicePicker is the single LM Studio binding form used by the task page
// and the multi-bundle handoff: an in-place service refresh, one-click fill from
// a compatible discovered service, and the manual PID and endpoint inputs.
func (s *viewState) localServicePicker(c *ui.Context, pidLabel, endpointLabel string) {
	ui.Row(c).Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.Row).Children(func() {
		// Discovery is independent of the service work in flight: while awaiting
		// the next bundle's service, busy describes the run queue, not discovery.
		button(c, "刷新服务列表").Disabled(s.discovering).OnClick(s.refreshDiscovery)
		if s.discovering {
			ui.Text(c, "正在发现本机服务…").TextColor(c.Theme().TextMuted)
		}
	})
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
	field(c, pidLabel, &s.localPID).MaxWidth(360)
	field(c, endpointLabel, &s.localEndpoint).MaxWidth(360).Description("例如 http://127.0.0.1:1234")
}

func (s *viewState) localSelectionView(c *ui.Context) {
	if e := s.localEngine(); e != nil {
		ui.Text(c, e.Family+" · 每个测评题集单独连接服务；完成后再进入下一测评题集。").TextColor(c.Theme().TextMuted)
		if e.Family == "lmstudio" {
			ui.Text(c, "先在 LM Studio 启动后台服务并加载所选模型。Studio 只绑定已有服务；请使用后台服务 PID。")
			s.localServicePicker(c, "LM Studio 后台 PID", "LM Studio endpoint")
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
			s.localBindingTask, s.localPID, s.handoffFailure = task.TaskId, "", nil
		}
		card(c, "连接下一测评题集的 LM Studio 服务", func() {
			ui.Text(c, emptyText(task.Bundle)+" · 请结束上一测评题集的服务，重新启动后台服务并加载同一模型。CLI 将重新校验身份。")
			// The handoff owns its own diagnostic: the error appears here, next to
			// the form, and never only in the page banner.
			if s.handoffFailure != nil {
				failureView(c, "连接此测评题集失败", s.handoffFailure, "服务就绪后可用同一 PID 重试。")
			}
			s.localServicePicker(c, "新的后台 PID", "本机 endpoint")
			pid, err := strconv.ParseInt(s.localPID, 10, 64)
			if err == nil && pid > 0 && s.localPIDUsed(pid) {
				ui.Text(c, human("local_engine_service_reused")).TextColor(stateColor(c, stateWarning))
			}
			button(c, "连接并继续此测评题集").Disabled(s.localSelectionInvalid(pid, err)).OnClick(func() {
				s.connectHandoffService(task.TaskId, pid)
			})
		})
		ui.Text(c, fmt.Sprintf("等待服务 · %s", task.TaskId)).TextColor(c.Theme().TextMuted)
	}
}

// connectHandoffService connects the next bundle's service. A PID accepted by
// the service is only reserved: the CLI verifies the process identity
// afterwards, and only a verified task consumes the service.
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

// reserveLocalPID keeps an in-flight verification from being offered again while
// still leaving the PID retryable if that verification fails.
func (s *viewState) reserveLocalPID(taskID string, pid int64) {
	if taskID == "" || pid <= 0 {
		return
	}
	if s.localPendingPIDs == nil {
		s.localPendingPIDs = map[string]int64{}
	}
	s.localPendingPIDs[taskID] = pid
}

// reconcileLocalPIDs settles reserved services against the current task states.
// A verified binding (ConfigPath written by the CLI) consumes the service for the
// rest of the round even if the run that follows fails before the next refresh;
// only an attempt that ended without a verified binding releases the PID.
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

// localPendingTask resolves the task a reservation belongs to.
func localPendingTask(tasks []core.RunTask, taskID string) *core.RunTask {
	for i := range tasks {
		if tasks[i].TaskId == taskID {
			return &tasks[i]
		}
	}
	return nil
}
