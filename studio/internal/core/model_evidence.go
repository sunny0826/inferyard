package core

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type EngineSpeedRecord struct {
	Engine          string   `json:"engine"`
	TokensPerSecond float64  `json:"tokens_per_second"`
	TTFTMs          *float64 `json:"ttft_ms,omitempty"`
	ReportRef       string   `json:"report_ref"`
	Exclusivity     string   `json:"exclusivity,omitempty"` // "exclusive" | "external" | "unknown"
	CreatedAt       string   `json:"created_at,omitempty"`
}

type ModelEvidenceSummary struct {
	Model    string              `json:"model"`
	Best     *EngineSpeedRecord  `json:"best,omitempty"`
	ByEngine []EngineSpeedRecord `json:"by_engine,omitempty"` // sorted by TokensPerSecond descending
}

// NormalizeModelEvidenceKey normalizes a model name or path into a canonical lookup key
// for ModelBenchmarkEvidence.
func NormalizeModelEvidenceKey(model string) string {
	s := strings.TrimSpace(model)
	if s == "" {
		return ""
	}
	if filepath.IsAbs(s) || strings.HasPrefix(s, "~/") {
		s = filepath.Base(s)
	}
	lower := strings.ToLower(s)
	lower = strings.TrimSuffix(lower, ".gguf")
	return strings.TrimSpace(lower)
}

// NormalizeEvidenceEngineKey normalizes an engine/family/adapter identifier into a canonical key.
func NormalizeEvidenceEngineKey(raw string) string {
	key, _ := normalizeEvidenceEngine(raw)
	return key
}

func normalizeEvidenceEngine(raw string) (key, display string) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", ""
	}
	lower := strings.ToLower(s)
	switch {
	case lower == "polystrata" || strings.Contains(lower, "polystrata"):
		return "polystrata", "PolyStrata"
	case lower == "llama-cpp" || lower == "llama.cpp" || lower == "llama_cpp" || strings.Contains(lower, "llama-server") || strings.Contains(lower, "llama"):
		return "llama.cpp", "llama.cpp"
	case lower == "mlx-lm" || lower == "mlx_lm" || lower == "mlx" || strings.Contains(lower, "mlx"):
		return "mlx-lm", "mlx-lm"
	case lower == "vllm" || strings.Contains(lower, "vllm"):
		return "vllm", "vLLM"
	case lower == "sglang" || strings.Contains(lower, "sglang"):
		return "sglang", "SGLang"
	case lower == "lmstudio" || lower == "lm studio" || lower == "lm-studio" || strings.Contains(lower, "lmstudio") || strings.Contains(lower, "lm-studio") || strings.Contains(lower, "lm studio"):
		return "lmstudio", "LM Studio"
	case lower == "ollama" || strings.Contains(lower, "ollama"):
		return "ollama", "Ollama"
	default:
		return lower, s
	}
}

func engineFromReportProfile(eng ReportEngine) (key, display string) {
	if eng.BinaryPath != nil {
		bin := strings.ToLower(strings.TrimSpace(*eng.BinaryPath))
		switch {
		case strings.Contains(bin, "polystrata"):
			return "polystrata", "PolyStrata"
		case strings.Contains(bin, "llama-server") || strings.Contains(bin, "llama.cpp") || strings.Contains(bin, "llama-cpp"):
			return "llama.cpp", "llama.cpp"
		case strings.Contains(bin, "mlx_lm") || strings.Contains(bin, "mlx-lm"):
			return "mlx-lm", "mlx-lm"
		case strings.Contains(bin, "vllm"):
			return "vllm", "vLLM"
		case strings.Contains(bin, "sglang"):
			return "sglang", "SGLang"
		case strings.Contains(bin, "ollama"):
			return "ollama", "Ollama"
		case strings.Contains(bin, "lmstudio") || strings.Contains(bin, "lm-studio"):
			return "lmstudio", "LM Studio"
		}
	}
	return normalizeEvidenceEngine(eng.Adapter)
}

// ModelBenchmarkEvidence aggregates valid throughput measurements across all readable
// Studio reports, grouped by normalized model keys (report model name, filename, and
// registry aliases) and engine.
func (s *Service) ModelBenchmarkEvidence() map[string]ModelEvidenceSummary {
	_, summaries, apiErr := s.Reports()
	if apiErr != nil || len(summaries) == 0 {
		return map[string]ModelEvidenceSummary{}
	}
	history, _ := s.History()
	return s.ModelBenchmarkEvidenceWith(summaries, history)
}

// ModelBenchmarkEvidenceWith aggregates throughput measurements reusing already-scanned
// report summaries and run history to avoid redundant disk scans during full refreshes.
func (s *Service) ModelBenchmarkEvidenceWith(summaries []ReportSummary, history RunHistoryResponse) map[string]ModelEvidenceSummary {
	if len(summaries) == 0 {
		return map[string]ModelEvidenceSummary{}
	}
	return s.modelBenchmarkEvidenceFromData(summaries, history, s.registry())
}

func (s *Service) modelBenchmarkEvidenceFromData(summaries []ReportSummary, history RunHistoryResponse, reg registryData) map[string]ModelEvidenceSummary {
	out := map[string]ModelEvidenceSummary{}
	if len(summaries) == 0 {
		return out
	}

	runAliases := buildRunModelKeyAliasesFromHistory(history)
	regAliases := buildRegistryModelKeyAliases(reg)
	exclPath := s.exclusivityFilePath()
	s.exclusivityMu.Lock()
	exclStore := readExclusivityStore(exclPath)
	s.exclusivityMu.Unlock()

	type modelBucket struct {
		displayName string
		byEngine    map[string]EngineSpeedRecord
	}
	buckets := map[string]*modelBucket{}

	for _, summary := range summaries {
		if summary.Unreadable || summary.ReportId == "" {
			continue
		}
		report, err := s.ReportIndex(summary.ReportId)
		if err != nil || report == nil {
			continue
		}
		createdAt := str(summary.ModifiedAt)
		for _, run := range report.Runs {
			if run.Dashboard.Throughput == nil || run.Dashboard.Throughput.OutputTokensPerSecond == nil {
				continue
			}
			tps := *run.Dashboard.Throughput.OutputTokensPerSecond
			if tps <= 0 || math.IsNaN(tps) || math.IsInf(tps, 0) {
				continue
			}
			modelDisplay := strings.TrimSpace(run.Model)
			inputsEngineFamily, inputsModelPath := s.readRunInputsConfigCached(run.Source.Path, summary.Path)
			modelKeys := collectRunModelKeys(run, inputsModelPath, runAliases, regAliases)
			if len(modelKeys) == 0 {
				continue
			}
			var engineKey, engineDisplay string
			if inputsEngineFamily != "" {
				engineKey, engineDisplay = normalizeEvidenceEngine(inputsEngineFamily)
			}
			if engineKey == "" {
				engineKey, engineDisplay = engineFromReportProfile(run.Profile.Engine)
			}
			if engineKey == "" {
				continue
			}

			rec := EngineSpeedRecord{
				Engine:          engineDisplay,
				TokensPerSecond: tps,
				TTFTMs:          nil,
				ReportRef:       summary.ReportId,
				Exclusivity:     exclusivityFromStore(exclStore, run.Source.RunID, summary.ReportId),
				CreatedAt:       createdAt,
			}

			for _, modelKey := range modelKeys {
				bucket, ok := buckets[modelKey]
				if !ok {
					bucket = &modelBucket{
						displayName: modelDisplay,
						byEngine:    map[string]EngineSpeedRecord{},
					}
					buckets[modelKey] = bucket
				}
				existing, exists := bucket.byEngine[engineKey]
				if !exists {
					bucket.byEngine[engineKey] = rec
					continue
				}
				if rec.TokensPerSecond > existing.TokensPerSecond || (rec.TokensPerSecond == existing.TokensPerSecond && rec.CreatedAt > existing.CreatedAt) {
					bucket.byEngine[engineKey] = rec
				}
			}
		}
	}

	for modelKey, bucket := range buckets {
		if len(bucket.byEngine) == 0 {
			continue
		}
		records := make([]EngineSpeedRecord, 0, len(bucket.byEngine))
		for _, rec := range bucket.byEngine {
			records = append(records, rec)
		}
		sort.SliceStable(records, func(i, j int) bool {
			if records[i].TokensPerSecond != records[j].TokensPerSecond {
				return records[i].TokensPerSecond > records[j].TokensPerSecond
			}
			return records[i].Engine < records[j].Engine
		})
		best := records[0]
		out[modelKey] = ModelEvidenceSummary{
			Model:    bucket.displayName,
			Best:     &best,
			ByEngine: records,
		}
	}
	return out
}

func exclusivityFromStore(store exclusivityStore, runID, reportRef string) string {
	if runID != "" {
		if st, ok := store.Runs[runID]; ok && validExclusivityStatus(st) {
			return st
		}
	}
	if reportRef != "" {
		if st, ok := store.Reports[reportRef]; ok && validExclusivityStatus(st) {
			return st
		}
	}
	return ExclusivityUnknown
}

func buildRunModelKeyAliasesFromHistory(history RunHistoryResponse) map[string][]string {
	aliases := map[string][]string{}
	addRun := func(r RunSummary) {
		if r.RunId == "" {
			return
		}
		var keys []string
		for _, raw := range []string{str(r.ModelName), str(r.ModelPath)} {
			if k := NormalizeModelEvidenceKey(raw); k != "" {
				keys = append(keys, k)
			}
		}
		if len(keys) > 0 {
			aliases[r.RunId] = keys
		}
	}
	for _, b := range history.Batches {
		for _, r := range b.Runs {
			addRun(r)
		}
	}
	for _, r := range history.StandaloneRuns {
		addRun(r)
	}
	return aliases
}

func buildRegistryModelKeyAliases(reg registryData) map[string][]string {
	aliases := map[string][]string{}
	for _, entry := range reg.models {
		rawNames := []string{entry.model.Name, entry.displayName}
		if entry.path != "" {
			rawNames = append(rawNames, filepath.Base(entry.path))
		}
		if entry.rawPath != "" {
			rawNames = append(rawNames, filepath.Base(entry.rawPath))
		}
		seen := map[string]bool{}
		var group []string
		for _, raw := range rawNames {
			if k := NormalizeModelEvidenceKey(raw); k != "" && !seen[k] {
				seen[k] = true
				group = append(group, k)
			}
		}
		if len(group) < 2 {
			continue
		}
		for _, k := range group {
			aliases[k] = append(aliases[k], group...)
		}
	}
	return aliases
}

func collectRunModelKeys(run ReportRun, inputsModelPath string, runAliases, regAliases map[string][]string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(k string) {
		if k == "" || seen[k] {
			return
		}
		seen[k] = true
		out = append(out, k)
	}
	add(NormalizeModelEvidenceKey(run.Model))
	if run.Source.RunID != "" {
		for _, k := range runAliases[run.Source.RunID] {
			add(k)
		}
	}
	if inputsModelPath != "" {
		add(NormalizeModelEvidenceKey(filepath.Base(inputsModelPath)))
		add(NormalizeModelEvidenceKey(inputsModelPath))
	}
	for i := 0; i < len(out); i++ {
		for _, alias := range regAliases[out[i]] {
			add(alias)
		}
	}
	return out
}

func readRunInputsConfig(sourcePath, reportDir string) (engineFamily, modelPath string) {
	p := strings.TrimSpace(sourcePath)
	if p == "" {
		return "", ""
	}
	if !filepath.IsAbs(p) && reportDir != "" {
		p = filepath.Join(reportDir, p)
	}
	raw, err := os.ReadFile(filepath.Join(p, "inputs.json"))
	if err != nil {
		return "", ""
	}
	if cfg := payload(raw, "config"); cfg != nil {
		engineFamily = strings.TrimSpace(str(text(obj(cfg["engine"])["family"])))
		if lp := strings.TrimSpace(str(text(obj(cfg["model"])["local_path"]))); lp != "" {
			modelPath = lp
		} else if dn := strings.TrimSpace(str(text(obj(cfg["model"])["display_name"]))); dn != "" {
			modelPath = dn
		}
	}
	if modelPath != "" {
		return engineFamily, modelPath
	}
	var doc struct {
		ModelPath string `json:"model_path"`
		Model     struct {
			Path string `json:"path"`
			Name string `json:"name"`
		} `json:"model"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return engineFamily, ""
	}
	if strings.TrimSpace(doc.ModelPath) != "" {
		return engineFamily, doc.ModelPath
	}
	if strings.TrimSpace(doc.Model.Path) != "" {
		return engineFamily, doc.Model.Path
	}
	return engineFamily, doc.Model.Name
}

// BestSpeedFor returns the best recorded throughput for model (optionally filtered to a
// specific engine). Returns (nil, false) when no valid measurement exists.
func (s *Service) BestSpeedFor(model, engine string) (*EngineSpeedRecord, bool) {
	modelKey := NormalizeModelEvidenceKey(model)
	if modelKey == "" {
		return nil, false
	}
	evidence := s.ModelBenchmarkEvidence()
	summary, ok := evidence[modelKey]
	if !ok || summary.Best == nil {
		return nil, false
	}
	engineKey, _ := normalizeEvidenceEngine(engine)
	if engineKey == "" {
		best := *summary.Best
		return &best, true
	}
	for _, rec := range summary.ByEngine {
		recKey, _ := normalizeEvidenceEngine(rec.Engine)
		if recKey == engineKey {
			copied := rec
			return &copied, true
		}
	}
	return nil, false
}
