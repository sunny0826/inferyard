package core

import (
	"bytes"
	"encoding/json"
	"strings"
)

const StarterRecommendationDefinition = "starter-recommendation.v1"

type StarterAlternative struct {
	Engine                 string  `json:"engine"`
	Backend                string  `json:"backend"`
	ModelID                string  `json:"model_id"`
	ModelTitle             string  `json:"model_title"`
	Family                 *string `json:"family,omitempty"`
	Size                   *string `json:"size,omitempty"`
	EngineCore             *string `json:"engine_core,omitempty"`
	ModelFormat            string  `json:"model_format"`
	FitLevel               string  `json:"fit_level"`
	Tier                   string  `json:"tier,omitempty"`
	RecommendedContext     int64   `json:"recommended_context"`
	EstimatedDownloadBytes int64   `json:"estimated_download_bytes"`
	EstimatedRAMBytes      int64   `json:"estimated_ram_bytes"`
}

type StarterRecommendationGPU struct {
	Name               string `json:"name"`
	Vendor             string `json:"vendor"`
	ArchFamily         string `json:"arch_family,omitempty"`
	VRAMBytes          *int64 `json:"vram_bytes,omitempty"`
	UnifiedMemoryBytes *int64 `json:"unified_memory_bytes,omitempty"`
	Driver             string `json:"driver_version,omitempty"`
	ComputeCap         string `json:"compute_capability,omitempty"`
}

type StarterRecommendation struct {
	Definition             string                    `json:"definition"`
	Status                 string                    `json:"status"`
	Engine                 string                    `json:"engine,omitempty"`
	Backend                string                    `json:"backend,omitempty"`
	ModelID                string                    `json:"model_id,omitempty"`
	ModelTitle             string                    `json:"model_title,omitempty"`
	Family                 *string                   `json:"family,omitempty"`
	Size                   *string                   `json:"size,omitempty"`
	EngineCore             *string                   `json:"engine_core,omitempty"`
	ModelFormat            string                    `json:"model_format,omitempty"`
	FitLevel               string                    `json:"fit_level,omitempty"`
	Tier                   string                    `json:"tier,omitempty"`
	RecommendedContext     int64                     `json:"recommended_context,omitempty"`
	EstimatedDownloadBytes int64                     `json:"estimated_download_bytes,omitempty"`
	EstimatedRAMBytes      int64                     `json:"estimated_ram_bytes,omitempty"`
	DiskFreeBytes          *int64                    `json:"disk_free_bytes,omitempty"`
	Reason                 string                    `json:"reason,omitempty"`
	GPU                    *StarterRecommendationGPU `json:"gpu,omitempty"`
	Notes                  []string                  `json:"notes,omitempty"`
	Alternatives           []StarterAlternative      `json:"alternatives,omitempty"`
	PolystrataModels       []StarterAlternative      `json:"polystrata_models,omitempty"`
	FromSnapshot           bool                      `json:"from_snapshot"`
}

func ParseStarterRecommendation(snapshot json.RawMessage) (StarterRecommendation, bool) {
	trimmed := bytes.TrimSpace(snapshot)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return StarterRecommendation{}, false
	}
	var wrapper struct {
		StarterRecommendation json.RawMessage `json:"starter_recommendation"`
	}
	if err := json.Unmarshal(trimmed, &wrapper); err != nil {
		return StarterRecommendation{}, false
	}
	rawRec := bytes.TrimSpace(wrapper.StarterRecommendation)
	if len(rawRec) == 0 || bytes.Equal(rawRec, []byte("null")) || rawRec[0] != '{' {
		return StarterRecommendation{}, false
	}
	var rec StarterRecommendation
	if err := json.Unmarshal(rawRec, &rec); err != nil {
		return StarterRecommendation{}, false
	}
	if rec.Definition != StarterRecommendationDefinition {
		return StarterRecommendation{}, false
	}
	if rec.Status != "recommended" && rec.Status != "blocked" {
		return StarterRecommendation{}, false
	}
	if rec.Status == "recommended" && (strings.TrimSpace(rec.Engine) == "" || strings.TrimSpace(rec.ModelID) == "") {
		return StarterRecommendation{}, false
	}
	rec.FromSnapshot = true
	return rec, true
}

func StaticFallbackStarterRecommendation() StarterRecommendation {
	primaryTitle := "Qwen3-0.6B-Q8_0 (GGUF)"
	var primaryBytes int64 = 639446688
	if entry, ok := CatalogEntryByID("model-qwen3-0.6b-q8"); ok {
		primaryTitle = entry.Title
		primaryBytes = entry.Bytes
	}
	alternatives := []StarterAlternative{}
	if entry, ok := CatalogEntryByID("model-qwen3-4b-q4km"); ok {
		alternatives = append(alternatives, StarterAlternative{
			Engine:                 "llama-cpp",
			Backend:                "cpu",
			ModelID:                entry.ID,
			ModelTitle:             entry.Title,
			ModelFormat:            "gguf",
			FitLevel:               "fits",
			Tier:                   "balanced",
			RecommendedContext:     8192,
			EstimatedDownloadBytes: entry.Bytes,
			EstimatedRAMBytes:      6442450944,
		})
	}
	if entry, ok := CatalogEntryByID("model-smollm2-135m"); ok {
		alternatives = append(alternatives, StarterAlternative{
			Engine:                 "llama-cpp",
			Backend:                "cpu",
			ModelID:                entry.ID,
			ModelTitle:             entry.Title,
			ModelFormat:            "gguf",
			FitLevel:               "fits",
			Tier:                   "lightweight",
			RecommendedContext:     4096,
			EstimatedDownloadBytes: entry.Bytes,
			EstimatedRAMBytes:      1073741824,
		})
	}
	return StarterRecommendation{
		Definition:             StarterRecommendationDefinition,
		Status:                 "fallback_static",
		Engine:                 "llama-cpp",
		Backend:                "cpu",
		ModelID:                "model-qwen3-0.6b-q8",
		ModelTitle:             primaryTitle,
		ModelFormat:            "gguf",
		FitLevel:               "fits",
		Tier:                   "best",
		RecommendedContext:     8192,
		EstimatedDownloadBytes: primaryBytes,
		EstimatedRAMBytes:      2147483648,
		Reason:                 "device_snapshot_unavailable",
		Alternatives:           alternatives,
		FromSnapshot:           false,
	}
}

func (s *Service) RecommendedStarter() StarterRecommendation {
	dev := s.Device()
	if rec, ok := ParseStarterRecommendation(dev.Snapshot); ok {
		return rec
	}
	return StaticFallbackStarterRecommendation()
}
