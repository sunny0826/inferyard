package pages

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"inferyard/studio/internal/core"
)

type startChainResult struct {
	candidate, workspace string
}

// runStartChain prepares candidates for the task page. Every
// step re-reads progress instead of trusting earlier snapshots; commands come
// from progress next_command or the config create flag surface, never from
// hand-assembled engine argv.
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
	// Step 1: a selected model that is not the latest preflight recommendation
	// requires a fresh device-check before anything is frozen.
	match, e := preflightMatch(*p.DevicePreflight.Path, modelName)
	if e != nil && e.Code != "device_preflight_blocked" {
		return startChainResult{}, e
	}
	if e != nil {
		match = false
	}
	if !match {
		out, e := newNumberedOutput(p.Workspace, "preflight", "device")
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
	// Step 2: a bundle "has a candidate for this combination" only when its
	// frozen candidate.toml names the selected model and engine; every unfinished
	// bundle in the default scope must match. Sealed runs keep their candidates.
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
			out, e := newNumberedOutput(p.Workspace, "candidates", b.Bundle)
			if e != nil {
				return startChainResult{}, e
			}
			command = "inferyard config create --workspace " + quote(p.Workspace) +
				" --bundle " + quote(b.Bundle) + " --out " + quote(out)
		}
		creates = append(creates, command)
	}
	// Step 3: create candidates for the mismatched bundles, one command each.
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
	// Step 4: re-verify from fresh progress and pick the candidate to start.
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

// chainFailure surfaces the CLI's fixed reason, translating device-check
// blockers with the device label table the models page already uses.
func chainFailure(r core.WizardResult) *core.ApiError {
	e := wizardFailure(r)
	if len(e.Limitations) > 0 {
		if label, ok := deviceLabels[e.Limitations[0]]; ok {
			e.Message = label + "；请先解决设备检查阻断项。"
		}
	}
	return e
}

// newNumberedOutput picks a fresh "<prefix>-N" child inside workspace/dir,
// mirroring the CLI's new_output pattern for device and candidate outputs.
func newNumberedOutput(workspace, dir, prefix string) (string, *core.ApiError) {
	if workspace == "" || !validName(prefix) {
		return "", core.Err("invalid_request")
	}
	for n := 1; n <= 1000; n++ {
		path := filepath.Join(workspace, dir, fmt.Sprintf("%s-%d", prefix, n))
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			return path, nil
		} else if err != nil {
			return "", core.Err("internal_error")
		}
	}
	return "", core.Err("output_name_exhausted")
}
