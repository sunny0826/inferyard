package pages

import (
	"slices"

	"inferyard/studio/internal/core"
)

func compatibleStandaloneEngine(model core.ModelAsset, eng core.EngineAsset) bool {
	switch eng.Family {
	case "lmstudio":
		return false
	case "", "llama-cpp":
		return model.Format == "gguf"
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
	default:
		return false
	}
}

func compatibleBenchmarkEngine(model core.ModelAsset, eng core.EngineAsset) bool {
	if eng.Kind != "local_installation" && eng.Family == "" {
		return model.Format != "directory" && model.Format != "strata_pack"
	}
	switch eng.Family {
	case "", "llama-cpp", "lmstudio":
		return model.Format != "directory" && model.Format != "strata_pack"
	default:
		return compatibleStandaloneEngine(model, eng)
	}
}

func (s *viewState) findEngineAsset(name string) (core.EngineAsset, bool) {
	if s == nil || name == "" {
		return core.EngineAsset{}, false
	}
	for _, eng := range s.registry.Engines {
		if eng.Name == name {
			return eng, true
		}
	}
	return core.EngineAsset{}, false
}

func (s *viewState) isBenchmarkEngineUsable(engName string) bool {
	if s == nil || engName == "" {
		return false
	}
	el := s.eligibility(engName)
	if s.checks.Supported {
		return el.State == core.EngineCheckEligible
	}
	return el.State != core.EngineCheckIneligible
}

func (s *viewState) hasCompatibleUsableBenchmarkEngine(model core.ModelAsset) bool {
	if s == nil {
		return false
	}
	for _, eng := range s.registry.Engines {
		if s.isBenchmarkEngineUsable(eng.Name) && compatibleBenchmarkEngine(model, eng) {
			return true
		}
	}
	return false
}

func (s *viewState) candidateModelSuffix(model core.ModelAsset) string {
	if s == nil || s.engine == "" || s.compatibleModel(model) {
		return ""
	}
	if s.hasCompatibleUsableBenchmarkEngine(model) {
		return ""
	}
	return s.incompatibleModelSuffix(model)
}

func (s *viewState) reconcileEngineForModel(modelName string) {
	if s == nil || modelName == "" {
		return
	}
	model, ok := s.findModelAsset(modelName)
	if !ok || !model.Available {
		return
	}
	if s.engine != "" {
		if currentEng, exists := s.findEngineAsset(s.engine); exists &&
			s.isBenchmarkEngineUsable(currentEng.Name) &&
			compatibleBenchmarkEngine(model, currentEng) {
			return
		}
	}
	for _, eng := range s.registry.Engines {
		if s.isBenchmarkEngineUsable(eng.Name) && compatibleBenchmarkEngine(model, eng) {
			s.engine = eng.Name
			return
		}
	}
}

func (s *viewState) reconcileCandidateBeforeRender() {
	if s == nil {
		return
	}
	if !s.lastCandidateInit {
		s.recordCandidateSelection()
		return
	}
	s.applyCandidateSelectionDiff(s.lastCandidateModel, s.lastCandidateEngine)
	s.recordCandidateSelection()
}

func (s *viewState) applyCandidateSelectionDiff(prevModel, prevEngine string) {
	if s == nil {
		return
	}
	if s.model != prevModel && s.model != "" {
		s.reconcileEngineForModel(s.model)
		return
	}
	if s.engine != prevEngine && s.engine != "" && s.model != "" {
		if m, ok := s.findModelAsset(s.model); ok && !s.compatibleModel(m) {
			compat := ""
			compatCount := 0
			for _, cand := range s.registry.Models {
				if cand.Available && s.compatibleModel(cand) {
					compat = cand.Name
					compatCount++
				}
			}
			if compatCount == 1 {
				s.model = compat
			}
		}
	}
}

func (s *viewState) recordCandidateSelection() {
	if s == nil {
		return
	}
	s.lastCandidateModel = s.model
	s.lastCandidateEngine = s.engine
	s.lastCandidateInit = true
}
