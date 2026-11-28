package core

import (
	"math"
	"sort"
	"strings"
)

func normalizeRemoteModelEvidenceKey(model string) string {
	value := strings.TrimSpace(model)
	if value == "" {
		return ""
	}
	if index := strings.LastIndexAny(value, "/\\"); index >= 0 {
		value = value[index+1:]
	}
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.TrimSuffix(value, ".gguf")
}

func (s *Service) remoteModelBenchmarkEvidence(nodeID string) map[string]ModelEvidenceSummary {
	out := map[string]ModelEvidenceSummary{}
	summaries, apiErr := s.remoteReportSummaries(nodeID)
	if apiErr != nil {
		return out
	}
	type bucket struct {
		display  string
		byEngine map[string]EngineSpeedRecord
	}
	buckets := map[string]*bucket{}
	for _, summary := range summaries {
		report, reportErr := s.RemoteReportIndex(nodeID, summary.RemoteReportID)
		if reportErr != nil || report == nil {
			continue
		}
		for _, run := range report.Runs {
			if run.Dashboard.Throughput == nil || run.Dashboard.Throughput.OutputTokensPerSecond == nil {
				continue
			}
			tps := *run.Dashboard.Throughput.OutputTokensPerSecond
			if tps <= 0 || math.IsNaN(tps) || math.IsInf(tps, 0) {
				continue
			}
			modelKey := normalizeRemoteModelEvidenceKey(run.Model)
			engineKey, engineDisplay := engineFromReportProfile(run.Profile.Engine)
			if modelKey == "" || engineKey == "" {
				continue
			}
			if buckets[modelKey] == nil {
				buckets[modelKey] = &bucket{display: run.Model, byEngine: map[string]EngineSpeedRecord{}}
			}
			record := EngineSpeedRecord{
				Engine: engineDisplay, TokensPerSecond: tps, TTFTMs: nil,
				ReportRef: summary.ReportRef, Exclusivity: ExclusivityUnknown, CreatedAt: str(summary.ModifiedAt),
			}
			existing, ok := buckets[modelKey].byEngine[engineKey]
			if !ok || record.TokensPerSecond > existing.TokensPerSecond || record.TokensPerSecond == existing.TokensPerSecond && record.CreatedAt > existing.CreatedAt {
				buckets[modelKey].byEngine[engineKey] = record
			}
		}
	}
	for modelKey, bucket := range buckets {
		records := make([]EngineSpeedRecord, 0, len(bucket.byEngine))
		for _, record := range bucket.byEngine {
			records = append(records, record)
		}
		sort.SliceStable(records, func(i, j int) bool {
			if records[i].TokensPerSecond != records[j].TokensPerSecond {
				return records[i].TokensPerSecond > records[j].TokensPerSecond
			}
			return records[i].Engine < records[j].Engine
		})
		if len(records) == 0 {
			continue
		}
		best := records[0]
		out[modelKey] = ModelEvidenceSummary{Model: bucket.display, Best: &best, ByEngine: records}
	}
	return out
}
