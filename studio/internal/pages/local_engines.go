package pages

import (
	"slices"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

var polystrataSupportedGGUFArchs = []string{
	"qwen35moe",
	"gemma4",
	"lfm2moe",
	"flint",
	"glm5-next",
	"glm5next",
	"deepseek4",
	"kolibri1",
	"clover_moe",
}

func (s *viewState) localEngine() *core.EngineAsset {
	for i := range s.registry.Engines {
		e := &s.registry.Engines[i]
		if e.Name == s.engine && e.Kind == "local_installation" {
			return e
		}
	}
	return nil
}

func (s *viewState) selectedEnginePath() string {
	if s.engine == "" {
		return ""
	}
	for _, eng := range s.registry.Engines {
		if eng.Name == s.engine {
			return cleanMatchPath(eng.Path)
		}
	}
	return ""
}

func (s *viewState) reconcileSelectedEngine(prevEnginePath string) bool {
	if s.engine == "" {
		return false
	}
	for _, eng := range s.registry.Engines {
		if eng.Name == s.engine {
			return false
		}
	}
	if prevEnginePath != "" {
		for _, eng := range s.registry.Engines {
			if cleanMatchPath(eng.Path) == prevEnginePath {
				s.engine = eng.Name
				return false
			}
		}
		s.engine = ""
		return true
	}
	s.engine = ""
	return true
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
		if model.Format == "strata_pack" {
			return true
		}
		if model.Format != "gguf" {
			return false
		}
		return model.Architecture == "" || slices.Contains(polystrataSupportedGGUFArchs, model.Architecture)
	case "mlx-lm", "vllm", "sglang":
		return model.Format == "directory"
	}
	return false
}

func modelLabel(model core.ModelAsset) string {
	switch model.Format {
	case "directory":
		return l10n.TRf("models.label.directory", "name", model.Name)
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
			return l10n.TR("engine.local.error.format_mismatch")
		}
	}
	if e := s.localEngine(); e != nil {
		if s.eligibility(e.Name).State != core.EngineCheckEligible {
			return l10n.TR("engine.local.error.wait_verification")
		}
		if e.Family == "lmstudio" {
			if pid, err := strconv.ParseInt(s.localPID, 10, 64); err != nil || pid <= 0 || urlError(s.localEndpoint) != "" || s.localEndpoint == "" {
				return l10n.TR("engine.local.error.lmstudio_required")
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
		refreshButton(c, tr(c, "engine.discovery.refresh"), s.discovering).Disabled(s.discovering).OnClick(s.refreshDiscovery)
	})
	if choices := s.localServiceChoices(); len(choices) > 0 {
		ui.Text(c, tr(c, "engine.local.discovered_hint")).TextColor(c.Theme().TextMuted)
		for _, choice := range choices {
			label := trf(c, "engine.local.service_choice", "pid", strconv.FormatInt(choice.pid, 10), "endpoint", choice.endpoint, "count", strconv.Itoa(choice.models))
			if name := displayName(choice.name); name != "" {
				label += " · " + name
			}
			button(c, label).OnClick(func() { s.useDiscoveredService(choice) })
		}
	}
	field(c, pidLabel, &s.localPID).MaxWidth(360)
	field(c, endpointLabel, &s.localEndpoint).MaxWidth(360).Description(tr(c, "engine.local.endpoint_example"))
}

func (s *viewState) localSelectionView(c *ui.Context) {
	if e := s.localEngine(); e != nil && e.Family == "lmstudio" {
		ui.Text(c, tr(c, "engine.local.lmstudio_prompt"))
		s.localServicePicker(c, tr(c, "engine.local.lmstudio_pid"), tr(c, "engine.local.lmstudio_endpoint"))
	}
	if reason := s.localSelectionError(); reason != "" {
		noticeBanner(c, noticeBannerOptions{Kind: stateWarning, Size: bannerCompact, Message: reason})
	}
}

func (s *viewState) localInstallationsView(c *ui.Context) {
	card(c, tr(c, "engine.local.card_title"), func() {
		s.localEngineWizardView(c)
		componentFold(c, tr(c, "engine.local.import_fold"), func() {
			componentFold(c, tr(c, "engine.local.schema_fold"), func() {
				ui.Text(c, tr(c, "engine.local.schema_desc"))
			})
			s.pathField(c, tr(c, "engine.local.json_path_label"), &s.installationPath, browseInstallation).
				Error(s.browseError(browseInstallation.key, pathError(s.installationPath)))
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.Row).Children(func() {
				if primaryActionButton(c, tr(c, "engine.local.import_register"), s.actionBusy("install-register")).Disabled(s.busy || s.installationPath == "" || pathError(s.installationPath) != "").Clicked() {
					s.changeInstallation("register", s.installationPath)
				}
			})
		})
		for _, path := range s.settings.Settings.EngineInstallations {
			path := path
			ui.Row(c.Key(path)).Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.Row).Children(func() {
				ui.Text(c, path)
				if dangerButton(c, tr(c, "engine.local.remove_registration")).AlignSelf(ui.Start).Disabled(s.busy).Clicked() {
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
			s.enqueueActionToast("undo-remove-installation", l10n.TR("engine.local.toast.removed"), l10n.TR("engine.local.toast.undo"), l10n.TR("engine.local.toast.restored"), 6*time.Second, func() {
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
		message := l10n.TR("engine.local.toast.updated")
		switch action {
		case "register":
			_, err = s.service.RegisterEngineInstallation(s.ctx, path)
			message = l10n.TR("engine.local.toast.registered")
		case "remove":
			_, err = s.service.RemoveEngineInstallation(path)
			message = l10n.TR("engine.local.toast.removed")
		case "recheck":
			s.service.InvalidateEngineChecks()
			message = l10n.TR("engine.local.toast.recheck_started")
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
		card(c, tr(c, "engine.local.handoff.title"), func() {
			ui.Text(c, trf(c, "engine.local.handoff.prompt", "bundle", emptyText(task.Bundle)))
			if s.handoffFailure != nil {
				failureView(c, tr(c, "engine.local.handoff.failed_title"), s.handoffFailure, tr(c, "engine.local.handoff.retry_hint"))
			}
			s.localServicePicker(c, tr(c, "engine.local.handoff.new_pid"), tr(c, "engine.local.handoff.endpoint"))
			pid, err := strconv.ParseInt(s.localPID, 10, 64)
			if err == nil && pid > 0 && s.localPIDUsed(pid) {
				noticeBanner(c, noticeBannerOptions{Kind: stateWarning, Size: bannerCompact, Message: human("local_engine_service_reused")})
			}
			button(c, tr(c, "engine.local.handoff.connect_continue")).Disabled(s.localSelectionInvalid(pid, err)).OnClick(func() {
				s.connectHandoffService(task.TaskId, pid)
			})
		})
		ui.Text(c, trf(c, "engine.local.handoff.waiting", "taskId", task.TaskId)).TextColor(c.Theme().TextMuted)
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
