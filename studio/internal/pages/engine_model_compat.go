package pages

import (
	"path/filepath"
	"slices"
	"strings"

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
	case "mlx-lm", "vllm":
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

func (s *viewState) hasSelectedUsableEngine() bool {
	if s == nil || s.engine == "" {
		return false
	}
	if _, ok := s.findEngineAsset(s.engine); !ok {
		return false
	}
	return s.isBenchmarkEngineUsable(s.engine)
}

func (s *viewState) compatibleModelsForSelectedEngine() []core.ModelAsset {
	if !s.hasSelectedUsableEngine() {
		return nil
	}
	models := make([]core.ModelAsset, 0, len(s.registry.Models))
	for _, m := range s.registry.Models {
		if m.Available && s.compatibleModel(m) {
			models = append(models, m)
		}
	}
	return models
}

func (s *viewState) resolveCompatibleEngineForModel(modelName string) string {
	if s == nil || modelName == "" {
		return ""
	}
	model, ok := s.findModelAsset(modelName)
	if !ok || !model.Available {
		return ""
	}
	if s.engine != "" {
		if currentEng, exists := s.findEngineAsset(s.engine); exists &&
			s.isBenchmarkEngineUsable(currentEng.Name) &&
			compatibleBenchmarkEngine(model, currentEng) {
			return currentEng.Name
		}
	}
	for _, eng := range s.registry.Engines {
		if s.isBenchmarkEngineUsable(eng.Name) && compatibleBenchmarkEngine(model, eng) {
			return eng.Name
		}
	}
	return ""
}

func (s *viewState) reconcileCandidateSelection() {
	if s == nil {
		return
	}
	if s.engine != "" {
		if _, ok := s.findEngineAsset(s.engine); !ok {
			s.engine = ""
		}
	}
	if !s.hasSelectedUsableEngine() {
		if s.engine == "" || s.eligibility(s.engine).State == core.EngineCheckIneligible {
			s.model = ""
		} else if s.model != "" {
			if m, ok := s.findModelAsset(s.model); !ok || !m.Available || !s.compatibleModel(m) {
				s.model = ""
			}
		}
		return
	}
	if s.lastCandidateInit && s.engine != s.lastCandidateEngine {
		s.reconcileModelOnEngineChange(s.lastCandidateEngine)
		return
	}
	if s.model != "" {
		if m, ok := s.findModelAsset(s.model); !ok || !m.Available || !s.compatibleModel(m) {
			s.model = ""
		}
	}
}

func (s *viewState) reconcileModelOnEngineChange(prevEngine string) {
	if s == nil || s.engine == prevEngine {
		return
	}
	if !s.hasSelectedUsableEngine() {
		if s.engine == "" || s.eligibility(s.engine).State == core.EngineCheckIneligible {
			s.model = ""
		}
		return
	}
	compat := s.compatibleModelsForSelectedEngine()
	if s.model != "" {
		matched := false
		for _, m := range compat {
			if m.Name == s.model {
				matched = true
				break
			}
		}
		if !matched {
			s.model = ""
		}
	}
	if s.model == "" && !s.skipAutoModelAdopt && len(compat) == 1 {
		s.model = compat[0].Name
	}
}

func historyEngineMatches(e core.EngineAsset, wantEngine string) bool {
	if e.Name == wantEngine || e.Family == wantEngine || (e.Path != "" && filepath.Base(e.Path) == filepath.Base(wantEngine)) {
		return true
	}
	if wantEngine == "prism_llama_server_v1" || strings.HasPrefix(wantEngine, "llama_cpp_") {
		return e.Family == "" || e.Family == "llama-cpp"
	}
	return false
}

func pathEqualsOrContains(dirOrFile, target string) bool {
	if dirOrFile == "" || target == "" {
		return false
	}
	return target == dirOrFile || strings.HasPrefix(target, dirOrFile+string(filepath.Separator))
}

func (s *viewState) resolveHistoryRerunSelection(r core.RunSummary) (string, string) {
	if s == nil {
		return "", ""
	}
	var matchedModel *core.ModelAsset
	wantPath := cleanMatchPath(emptyText(r.ModelPath))
	if wantPath != "" {
		for i := range s.registry.Models {
			m := &s.registry.Models[i]
			if m.Available && m.Path != "" && cleanMatchPath(m.Path) == wantPath {
				matchedModel = m
				break
			}
		}
	}
	wantModel := strings.TrimSpace(emptyText(r.ModelName))
	wantServed := strings.TrimSpace(emptyText(r.ServedModel))
	if matchedModel == nil && (wantModel != "" || wantServed != "") {
		for i := range s.registry.Models {
			m := &s.registry.Models[i]
			if !m.Available {
				continue
			}
			if (wantModel != "" && (m.Name == wantModel || (m.Path != "" && filepath.Base(m.Path) == wantModel))) ||
				(wantServed != "" && m.Name == wantServed) {
				matchedModel = m
				break
			}
		}
	}
	if matchedModel == nil && wantPath != "" {
		for i := range s.registry.Models {
			m := &s.registry.Models[i]
			if !m.Available || m.Format != "strata_pack" {
				continue
			}
			for _, cp := range m.CoveredPaths {
				if pathEqualsOrContains(wantPath, cleanMatchPath(cp)) {
					matchedModel = m
					break
				}
			}
			if matchedModel != nil {
				break
			}
		}
	}
	var matchedEngine *core.EngineAsset
	if wantFamily := strings.TrimSpace(emptyText(r.EngineFamily)); wantFamily != "" {
		for i := range s.registry.Engines {
			e := &s.registry.Engines[i]
			if s.isBenchmarkEngineUsable(e.Name) && e.Family == wantFamily {
				matchedEngine = e
				break
			}
		}
	}
	if matchedEngine == nil {
		if wantEngine := strings.TrimSpace(emptyText(r.Engine)); wantEngine != "" {
			for i := range s.registry.Engines {
				e := &s.registry.Engines[i]
				if s.isBenchmarkEngineUsable(e.Name) && historyEngineMatches(*e, wantEngine) {
					matchedEngine = e
					break
				}
			}
		}
	}
	if matchedEngine != nil {
		if matchedModel != nil && compatibleBenchmarkEngine(*matchedModel, *matchedEngine) {
			return matchedEngine.Name, matchedModel.Name
		}
		return matchedEngine.Name, ""
	}
	if matchedModel != nil {
		var compat []core.EngineAsset
		for _, eng := range s.registry.Engines {
			if s.isBenchmarkEngineUsable(eng.Name) && compatibleBenchmarkEngine(*matchedModel, eng) {
				compat = append(compat, eng)
			}
		}
		if len(compat) == 1 {
			return compat[0].Name, matchedModel.Name
		}
	}
	return "", ""
}

func (s *viewState) recordCandidateSelection() {
	if s == nil {
		return
	}
	s.lastCandidateEngine = s.engine
	s.lastCandidateInit = true
}
