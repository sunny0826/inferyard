package pages

import (
	"context"
	"inferyard/studio/internal/core"
)

// prepareCombination is shared by Dashboard candidate creation and Models startup.
func prepareCombination(ctx context.Context, service *core.Service, model, engine string) (startChainResult, *core.ApiError) {
	if model == "" {
		current, e := service.Progress(ctx)
		if e != nil {
			return startChainResult{}, e
		}
		if current.ExitCode != 0 || current.Progress == nil || current.Progress.DevicePreflight.Path == nil {
			return startChainResult{}, core.Err("preflight_missing")
		}
		preflight := *current.Progress.DevicePreflight.Path
		for _, asset := range service.Registry().Models {
			if !asset.Available {
				continue
			}
			match, e := service.PreflightMatchesModel(preflight, asset.Name)
			if e != nil {
				return startChainResult{}, e
			}
			if match {
				model = asset.Name
				break
			}
		}
		if model == "" {
			return startChainResult{}, core.Err("device_preflight_blocked")
		}
	}
	return runStartChain(ctx, model, engine, service.Progress, service.WizardExecute,
		service.PreflightMatchesModel, service.CandidateMatchesCombination)
}

func (w *viewState) prepareCandidates() {
	model, engine := w.model, w.engine
	w.autoBind = bindingArm{}
	w.guideWork(func() (string, *core.ApiError) {
		_, e := prepareCombination(w.ctx, w.service, model, engine)
		if e != nil {
			return "", e
		}
		return "候选已创建，已更新工作区进度。", nil
	})
}
