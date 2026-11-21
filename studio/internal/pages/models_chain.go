package pages

import (
	"context"
	"strconv"
	"strings"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

var chooseFreePortFunc = core.ChooseFreeLoopbackPort

type startChainResult struct {
	candidate, workspace string
	localCandidates      []core.CandidateRun
	existingService      *core.EngineState
	localRequest         *core.LocalCandidateRequest
}

func runStartChain(ctx context.Context, modelName, engineName string,
	progress func(context.Context) (core.ProgressResponse, *core.ApiError),
	execute func(context.Context, core.WizardRequest) (core.WizardResult, *core.ApiError),
	preflightMatch func(preflightPath, modelName string) (bool, *core.ApiError),
	candidateMatch func(candidatePath, modelName, engineName string) (bool, bool),
	freshTask ...bool,
) (startChainResult, *core.ApiError) {
	current, e := progress(ctx)
	if e != nil {
		return startChainResult{}, e
	}
	p := current.Progress
	if current.ExitCode != 0 || p == nil || p.DevicePreflight.Status != "ok" || p.DevicePreflight.Path == nil {
		return startChainResult{}, core.Err("preflight_missing")
	}
	match, e := preflightMatch(*p.DevicePreflight.Path, modelName)
	if e != nil && e.Code != "device_preflight_blocked" {
		return startChainResult{}, e
	}
	if e != nil {
		match = false
	}
	if !match {
		out, e := core.NextOutput(p.Workspace, "preflight", "device")
		if e != nil {
			return startChainResult{}, e
		}
		r, e := execute(ctx, core.WizardRequest{
			Command:   "inferyard device-check --json --out " + quote(out),
			ModelName: modelName,
		})
		if e != nil {
			return startChainResult{}, e
		}
		if r.ExitCode != 0 {
			return startChainResult{}, chainFailure(r)
		}
		current, e = progress(ctx)
		if e != nil {
			return startChainResult{}, e
		}
		p = current.Progress
		if current.ExitCode != 0 || p == nil || p.DevicePreflight.Status != "ok" || p.DevicePreflight.Path == nil {
			return startChainResult{}, core.Err("device_preflight_blocked")
		}
		match, e = preflightMatch(*p.DevicePreflight.Path, modelName)
		if e != nil {
			return startChainResult{}, e
		}
		if !match {
			return startChainResult{}, core.Err("device_preflight_blocked")
		}
	}
	var selectedPort *int
	portForCreate := func() int {
		if selectedPort == nil {
			port := core.DefaultBenchmarkPort
			if allocated, err := chooseFreePortFunc(core.DefaultBenchmarkPort); err == nil && allocated > 0 {
				port = allocated
			}
			selectedPort = &port
		}
		return *selectedPort
	}
	force := len(freshTask) > 0 && freshTask[0]
	creates := []string{}
	for _, b := range p.Bundles {
		if b.Phase == "run_complete" && !force {
			continue
		}
		if b.CandidatePath != nil && !force {
			if match, known := candidateMatch(*b.CandidatePath, modelName, engineName); known && match {
				continue
			}
		}
		command := emptyText(b.NextCommand)
		if force || b.NextAction != "prepare_candidate" || command == "" {
			out, e := core.NextOutput(p.Workspace, "candidates", b.Bundle)
			if e != nil {
				return startChainResult{}, e
			}
			command = "inferyard config create --workspace " + quote(p.Workspace) +
				" --bundle " + quote(b.Bundle) + " --out " + quote(out)
		}
		if port := portForCreate(); port != core.DefaultBenchmarkPort && !commandHasFlag(command, "--port") {
			command += " --port " + strconv.Itoa(port)
		}
		creates = append(creates, command)
	}
	for _, command := range creates {
		if ctx.Err() != nil {
			return startChainResult{}, core.Err("cancelled")
		}
		r, e := execute(ctx, core.WizardRequest{Command: command, EngineName: engineName, PreflightPath: *p.DevicePreflight.Path})
		if e != nil {
			return startChainResult{}, e
		}
		if r.ExitCode != 0 {
			return startChainResult{}, chainFailure(r)
		}
	}
	current, e = progress(ctx)
	if e != nil {
		return startChainResult{}, e
	}
	p = current.Progress
	if current.ExitCode != 0 || p == nil || len(p.Bundles) == 0 {
		return startChainResult{}, core.Err("cli_error")
	}
	candidate, fallback := "", ""
	sealedMismatch := false
	for _, b := range p.Bundles {
		if b.Phase == "run_complete" {
			if b.CandidatePath == nil {
				sealedMismatch = true
				continue
			}
			match, known := candidateMatch(*b.CandidatePath, modelName, engineName)
			if !known || !match {
				sealedMismatch = true
			} else if fallback == "" {
				fallback = *b.CandidatePath
			}
			continue
		}
		if b.CandidatePath == nil {
			return startChainResult{}, core.Err("candidate_mismatch")
		}
		match, known := candidateMatch(*b.CandidatePath, modelName, engineName)
		if !known || !match {
			return startChainResult{}, core.Err("candidate_mismatch")
		}
		if candidate == "" {
			candidate = *b.CandidatePath
		}
	}
	if candidate == "" {
		if fallback == "" && sealedMismatch {
			return startChainResult{}, core.Err("candidate_mismatch")
		}
		candidate = fallback
	}
	return startChainResult{candidate: candidate, workspace: p.Workspace}, nil
}

func commandHasFlag(command, flag string) bool {
	args, err := inferyard.SplitCommand(command)
	if err != nil {
		return false
	}
	for _, arg := range args {
		if arg == flag || strings.HasPrefix(arg, flag+"=") {
			return true
		}
	}
	return false
}

func chainFailure(r core.WizardResult) *core.ApiError {
	e := wizardFailure(r)
	if len(e.Limitations) > 0 {
		if label, ok := deviceLabels[e.Limitations[0]]; ok {
			e.Message = label + "；请先解决设备检查阻断项。"
		}
	}
	return e
}
