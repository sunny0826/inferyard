package core

import (
	"math"
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

func normalizeEvidenceEngine(raw string) (key, display string) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", ""
	}
	lower := strings.ToLower(s)
	switch lower {
	case "llama-cpp", "llama.cpp", "llama_cpp", "llama-server", "prism_llama_server_v1", "llama_cpp_b11146_v1":
		return "llama.cpp", "llama.cpp"
	case "polystrata":
		return "polystrata", "PolyStrata"
	case "mlx-lm", "mlx_lm", "mlx":
		return "mlx-lm", "mlx-lm"
	case "vllm":
		return "vllm", "vLLM"
	case "sglang":
		return "sglang", "SGLang"
	case "lmstudio", "lm studio", "lm-studio":
		return "lmstudio", "LM Studio"
	case "ollama":
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
// Studio reports, grouped by normalized model name and engine.
func (s *Service) ModelBenchmarkEvidence() map[string]ModelEvidenceSummary {
	_, summaries, apiErr := s.Reports()
	out := map[string]ModelEvidenceSummary{}
	if apiErr != nil || len(summaries) == 0 {
		return out
	}

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
			modelKey := NormalizeModelEvidenceKey(modelDisplay)
			if modelKey == "" {
				continue
			}
			engineKey, engineDisplay := engineFromReportProfile(run.Profile.Engine)
			if engineKey == "" {
				continue
			}

			bucket, ok := buckets[modelKey]
			if !ok {
				bucket = &modelBucket{
					displayName: modelDisplay,
					byEngine:    map[string]EngineSpeedRecord{},
				}
				buckets[modelKey] = bucket
			}

			rec := EngineSpeedRecord{
				Engine:          engineDisplay,
				TokensPerSecond: tps,
				TTFTMs:          nil,
				ReportRef:       summary.ReportId,
				Exclusivity:     s.runExclusivityFromStore(run.Source.RunID, summary.ReportId),
				CreatedAt:       createdAt,
			}

			existing, exists := bucket.byEngine[engineKey]
			if !exists {
				bucket.byEngine[engineKey] = rec
				continue
			}
			// Reports are sorted newest-first; only replace if newer or same timestamp with higher throughput.
			if rec.CreatedAt > existing.CreatedAt || (rec.CreatedAt == existing.CreatedAt && rec.TokensPerSecond > existing.TokensPerSecond) {
				bucket.byEngine[engineKey] = rec
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
