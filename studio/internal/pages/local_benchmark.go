package pages

import (
	"context"
	"inferyard/studio/internal/core"
)

type localBenchmarkBackend interface {
	PrepareLocalCandidates(context.Context, core.LocalCandidateRequest) ([]core.CandidateRun, *core.ApiError)
	StartCandidateRuns(core.CandidateRunsRequest) ([]core.RunTask, *core.ApiError)
}

func prepareLocalCombination(ctx context.Context, service *core.Service, request core.LocalCandidateRequest) (startChainResult, *core.ApiError) {
	candidates, failure := service.PrepareLocalCandidates(ctx, request)
	if failure != nil {
		return startChainResult{}, failure
	}
	settings := service.Settings()
	result := startChainResult{localCandidates: candidates, localRequest: &request, workspace: emptyText(settings.Effective.WorkspaceRoot)}
	if len(candidates) == 0 {
		return result, core.Err("candidate_mismatch")
	}
	result.candidate = candidates[0].CandidatePath
	if request.ServicePID > 0 {
		result.existingService = &core.EngineState{State: "running", Pid: request.ServicePID, Endpoint: request.Endpoint}
	}
	return result, nil
}

func runLocalBenchmarkFlow(ctx context.Context, backend benchmarkBackend, result startChainResult, names []string, bindings *benchmarkBindings, announce func(string), queued func([]string), report func([]string, string)) *core.ApiError {
	local, ok := backend.(localBenchmarkBackend)
	if !ok || bindings == nil {
		return core.Err("invalid_request")
	}
	if bindings.completed == nil {
		bindings.completed = map[string]string{}
	}
	candidates := []core.CandidateRun{}
	for _, candidate := range result.localCandidates {
		if bindings.completed[candidate.Bundle] == "" {
			candidates = append(candidates, candidate)
		}
	}
	initial := result.existingService
	if bindings.localTasks != nil && len(candidates) > 0 {
		if result.localRequest == nil {
			return core.Err("candidate_mismatch")
		}
		request := *result.localRequest
		request.Bundles = nil
		for _, candidate := range candidates {
			request.Bundles = append(request.Bundles, candidate.Bundle)
		}
		if candidates[0].Family != "lmstudio" {
			var failure *core.ApiError
			candidates, failure = local.PrepareLocalCandidates(ctx, request)
			if failure != nil {
				return failure
			}
		}
		initial = nil
	}
	if len(candidates) > 0 {
		announce("run")
		tasks, failure := local.StartCandidateRuns(core.CandidateRunsRequest{Candidates: candidates, AllowSingleBundle: len(candidates) == 1, ExistingService: initial})
		if failure != nil {
			return failure
		}
		bindings.localTasks = map[string]string{}
		ids := []string{}
		for _, task := range tasks {
			bindings.localTasks[task.TaskId] = emptyText(task.Bundle)
			ids = append(ids, task.TaskId)
		}
		queued(ids)
		for {
			remaining := len(bindings.localTasks)
			var stopped *core.ApiError
			for _, task := range backend.RunTasks() {
				bundle, found := bindings.localTasks[task.TaskId]
				if !found || !terminal(task.State) {
					continue
				}
				remaining--
				if task.State == "done" && task.Error == nil && task.Exit != nil && task.Exit.ExitCode == 0 && emptyText(task.Exit.Completeness) == "complete" && emptyText(task.RunPath) != "" {
					bindings.completed[bundle] = *task.RunPath
				} else if stopped == nil {
					stopped = task.Error
					if stopped == nil {
						stopped = core.Err("run_incomplete")
					}
				}
			}
			if remaining == 0 {
				if stopped != nil {
					return stopped
				}
				break
			}
			if !taskWait(ctx) {
				return core.Err("cancelled")
			}
		}
	}
	paths := []string{}
	for _, name := range names {
		path := bindings.completed[name]
		if path == "" {
			return core.Err("run_incomplete")
		}
		paths = append(paths, path)
	}
	return finishBenchmarkReport(ctx, backend, result.workspace, names, paths, announce, report)
}
