package core

import (
	"strings"
)

type ReportCompareMetrics struct {
	Passed                 *int64   `json:"passed"`
	Valid                  *int64   `json:"valid"`
	Rate                   *float64 `json:"rate"`
	TTFTP50MS              *float64 `json:"ttft_p50_ms"`
	TTFTP95MS              *float64 `json:"ttft_p95_ms"`
	LatencyP50Ms           *float64 `json:"latency_p50_ms"`
	LatencyP95Ms           *float64 `json:"latency_p95_ms"`
	DecodeTokensPerSecond  *float64 `json:"decode_tokens_per_second"`
	OutputTokensPerSecond  *float64 `json:"output_tokens_per_second"`
	PeakMemoryBytes        *float64 `json:"peak_memory_bytes"`
	PeakWorkingSetBytes    *float64 `json:"peak_working_set_bytes"`
	PeakTemperatureCelsius *float64 `json:"peak_temperature_celsius"`
}

type ReportCompareRun struct {
	LeftRunID    string                `json:"left_run_id,omitempty"`
	RightRunID   string                `json:"right_run_id,omitempty"`
	Model        string                `json:"model,omitempty"`
	LeftModel    string                `json:"left_model,omitempty"`
	RightModel   string                `json:"right_model,omitempty"`
	Left         *ReportCompareMetrics `json:"left"`
	Right        *ReportCompareMetrics `json:"right"`
	SpeedupRatio *float64              `json:"speedup_ratio"`
}

type ReportCompareCase struct {
	RunID      string         `json:"run_id,omitempty"`
	CaseID     string         `json:"case_id"`
	LeftRunID  string         `json:"left_run_id,omitempty"`
	RightRunID string         `json:"right_run_id,omitempty"`
	Left       *ReportRequest `json:"left"`
	Right      *ReportRequest `json:"right"`
}

type ReportComparison struct {
	LeftRef      string               `json:"left_ref"`
	RightRef     string               `json:"right_ref"`
	LeftQuality  ReportCompareMetrics `json:"left_quality"`
	RightQuality ReportCompareMetrics `json:"right_quality"`
	Runs         []ReportCompareRun   `json:"runs"`
	Cases        []ReportCompareCase  `json:"cases"`
}

func metricsForReportRun(run ReportRun) ReportCompareMetrics {
	metrics := ReportCompareMetrics{
		Passed: run.Dashboard.Quality.Numerator, Valid: run.Dashboard.Quality.Denominator,
		LatencyP50Ms: run.Dashboard.Latency.P50, LatencyP95Ms: run.Dashboard.Latency.P95,
		PeakMemoryBytes: run.Dashboard.SampledPeakRSSBytes,
	}
	if metrics.Passed != nil && metrics.Valid != nil && *metrics.Valid > 0 {
		rate := float64(*metrics.Passed) / float64(*metrics.Valid)
		metrics.Rate = &rate
	}
	if run.Dashboard.Throughput != nil {
		metrics.DecodeTokensPerSecond = run.Dashboard.Throughput.OutputTokensPerSecond
		metrics.OutputTokensPerSecond = run.Dashboard.Throughput.OutputTokensPerSecond
	}
	return metrics
}

func aggregateReportQuality(runs []ReportRun) ReportCompareMetrics {
	var passed, valid int64
	hasPassed, hasValid := false, false
	for _, run := range runs {
		if run.Dashboard.Quality.Numerator != nil {
			passed += *run.Dashboard.Quality.Numerator
			hasPassed = true
		}
		if run.Dashboard.Quality.Denominator != nil {
			valid += *run.Dashboard.Quality.Denominator
			hasValid = true
		}
	}
	var out ReportCompareMetrics
	if hasPassed {
		out.Passed = &passed
	}
	if hasValid {
		out.Valid = &valid
	}
	if hasPassed && hasValid && valid > 0 {
		rate := float64(passed) / float64(valid)
		out.Rate = &rate
	}
	return out
}

func (s *Service) reportForCompare(reference string) (*ReportIndex, *ApiError) {
	if strings.HasPrefix(reference, remoteReportRefPrefix) {
		parts := strings.Split(strings.TrimPrefix(reference, remoteReportRefPrefix), "/")
		if len(parts) != 2 {
			return nil, Err("invalid_request")
		}
		return s.RemoteReportIndex(parts[0], parts[1])
	}
	return s.ReportIndex(reference)
}

func cloneCompareRequest(request *ReportRequest) *ReportRequest {
	if request == nil {
		return nil
	}
	copy := clone(*request)
	return &copy
}

func compareReportCases(left, right *ReportRun) []ReportCompareCase {
	var leftRequests, rightRequests []ReportRequest
	leftRunID, rightRunID, runID := "", "", ""
	if left != nil {
		leftRequests, leftRunID, runID = left.Requests, left.Source.RunID, left.Source.RunID
	}
	if right != nil {
		rightRequests, rightRunID = right.Requests, right.Source.RunID
		if runID == "" {
			runID = rightRunID
		}
	}
	rightByCase := map[string][]int{}
	for i := range rightRequests {
		caseID := rightRequests[i].CaseID
		rightByCase[caseID] = append(rightByCase[caseID], i)
	}
	matched := make([]bool, len(rightRequests))
	occurrences := map[string]int{}
	out := []ReportCompareCase{}
	appendCase := func(caseID string, leftRequest, rightRequest *ReportRequest) {
		out = append(out, ReportCompareCase{
			RunID: runID, CaseID: caseID, LeftRunID: leftRunID, RightRunID: rightRunID,
			Left: cloneCompareRequest(leftRequest), Right: cloneCompareRequest(rightRequest),
		})
	}
	for i := range leftRequests {
		request := &leftRequests[i]
		occurrence := occurrences[request.CaseID]
		occurrences[request.CaseID]++
		var other *ReportRequest
		indices := rightByCase[request.CaseID]
		if occurrence < len(indices) {
			index := indices[occurrence]
			other = &rightRequests[index]
			matched[index] = true
		}
		appendCase(request.CaseID, request, other)
	}
	for i := range rightRequests {
		if !matched[i] {
			appendCase(rightRequests[i].CaseID, nil, &rightRequests[i])
		}
	}
	return out
}

type reportRunPair struct {
	left  *ReportRun
	right *ReportRun
}

func reportRunComparisonKey(run ReportRun) string {
	return normalizeRemoteModelEvidenceKey(run.Model) + "\x00" + strings.ToLower(strings.TrimSpace(run.Facets.BundleLabel))
}

func pairReportRuns(left, right []ReportRun) []reportRunPair {
	if len(left) == 1 && len(right) == 1 {
		return []reportRunPair{{left: &left[0], right: &right[0]}}
	}
	rightUsed := make([]bool, len(right))
	pairs := make([]reportRunPair, 0, len(left)+len(right))
	for i := range left {
		pair := reportRunPair{left: &left[i]}
		key := reportRunComparisonKey(left[i])
		for j := range right {
			if !rightUsed[j] && reportRunComparisonKey(right[j]) == key {
				pair.right = &right[j]
				rightUsed[j] = true
				break
			}
		}
		pairs = append(pairs, pair)
	}
	for j := range right {
		if !rightUsed[j] {
			pairs = append(pairs, reportRunPair{right: &right[j]})
		}
	}
	return pairs
}

func (s *Service) CompareReports(leftRef, rightRef string) (*ReportComparison, *ApiError) {
	left, apiErr := s.reportForCompare(leftRef)
	if apiErr != nil {
		return nil, apiErr
	}
	right, apiErr := s.reportForCompare(rightRef)
	if apiErr != nil {
		return nil, apiErr
	}
	comparison := &ReportComparison{
		LeftRef: leftRef, RightRef: rightRef,
		LeftQuality: aggregateReportQuality(left.Runs), RightQuality: aggregateReportQuality(right.Runs),
		Runs: []ReportCompareRun{}, Cases: []ReportCompareCase{},
	}
	for _, runs := range pairReportRuns(left.Runs, right.Runs) {
		pair := ReportCompareRun{}
		if runs.left != nil {
			metrics := metricsForReportRun(*runs.left)
			pair.Left, pair.LeftRunID, pair.Model = &metrics, runs.left.Source.RunID, runs.left.Model
			pair.LeftModel = runs.left.Model
		}
		if runs.right != nil {
			metrics := metricsForReportRun(*runs.right)
			pair.Right, pair.RightRunID, pair.RightModel = &metrics, runs.right.Source.RunID, runs.right.Model
			if pair.Model == "" {
				pair.Model = runs.right.Model
			}
		}
		if pair.Left != nil && pair.Right != nil && pair.Left.DecodeTokensPerSecond != nil && pair.Right.DecodeTokensPerSecond != nil && *pair.Left.DecodeTokensPerSecond > 0 {
			ratio := *pair.Right.DecodeTokensPerSecond / *pair.Left.DecodeTokensPerSecond
			pair.SpeedupRatio = &ratio
		}
		comparison.Runs = append(comparison.Runs, pair)
		comparison.Cases = append(comparison.Cases, compareReportCases(runs.left, runs.right)...)
	}
	return comparison, nil
}
