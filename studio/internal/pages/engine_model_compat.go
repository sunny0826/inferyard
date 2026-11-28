package pages

import (
	"path/filepath"
	"slices"
	"strings"

	"inferyard/studio/internal/core"
)

func isPrismOnlyGGUF(model core.ModelAsset) bool {
	if model.Format == "directory" || model.Format == "strata_pack" {
		return false
	}
	hay := strings.ToLower(model.Name + " " + model.Path)
	return strings.Contains(hay, "ptq1_0") || strings.Contains(hay, "pq2_0")
}

func isNonPrismLlamaEngine(eng core.EngineAsset) bool {
	if strings.EqualFold(eng.Distribution, "PrismML") {
		return false
	}
	if strings.EqualFold(eng.Distribution, "ggml-org") {
		return true
	}
	hay := strings.ToLower(eng.Name + " " + eng.Path + " " + eng.Version)
	if strings.Contains(hay, "prism") {
		return false
	}
	return eng.Kind == "local_installation" || strings.TrimSpace(eng.Version) != ""
}

func compatibleStandaloneEngine(model core.ModelAsset, eng core.EngineAsset) bool {
	switch eng.Family {
	case "lmstudio":
		return false
	case "", "llama-cpp":
		if model.Format != "gguf" {
			return false
		}
		if isPrismOnlyGGUF(model) && isNonPrismLlamaEngine(eng) {
			return false
		}
		return true
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
		if model.Format == "directory" || model.Format == "strata_pack" {
			return false
		}
		return !(isPrismOnlyGGUF(model) && isNonPrismLlamaEngine(eng))
	}
	switch eng.Family {
	case "lmstudio":
		return model.Format != "directory" && model.Format != "strata_pack" && !isPrismOnlyGGUF(model)
	case "", "llama-cpp":
		if model.Format == "directory" || model.Format == "strata_pack" {
			return false
		}
		if isPrismOnlyGGUF(model) && isNonPrismLlamaEngine(eng) {
			return false
		}
		return true
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
	if wantFamily := strings.TrimSpace(emptyText(r.EngineFamily)); wantFamily != "" {
		var familyMatches []core.EngineAsset
		for _, e := range s.registry.Engines {
			if s.isBenchmarkEngineUsable(e.Name) && e.Family == wantFamily {
				familyMatches = append(familyMatches, e)
			}
		}
		if len(familyMatches) == 1 {
			if matchedModel != nil && compatibleBenchmarkEngine(*matchedModel, familyMatches[0]) {
				return familyMatches[0].Name, matchedModel.Name
			}
			return familyMatches[0].Name, ""
		}
		return "", ""
	}
	var matchedEngine *core.EngineAsset
	if wantEngine := strings.TrimSpace(emptyText(r.Engine)); wantEngine != "" {
		for i := range s.registry.Engines {
			e := &s.registry.Engines[i]
			if s.isBenchmarkEngineUsable(e.Name) && historyEngineMatches(*e, wantEngine) {
				matchedEngine = e
				break
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
