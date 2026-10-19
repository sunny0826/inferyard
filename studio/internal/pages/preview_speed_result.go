//go:build ui_debug && !release

package pages

import (
	"encoding/json"
	"fmt"
	"inferyard/studio/internal/inferyard"
	"strings"
)

// Synthetic evidence exists only in the debug window; these references are never IO paths.
func previewSpeedResult(r *speedTask, completed int, missing bool) inferyard.ContextSpeedResult {
	hash := strings.Repeat("a", 64)
	ref := func(path string) inferyard.SpeedRef { return inferyard.SpeedRef{Path: path, SHA256: hash} }
	result := inferyard.ContextSpeedResult{SchemaVersion: 3, Kind: "context_speed_result.v1", ReadState: "readable", Profile: "context-speed.v1"}
	result.Source = inferyard.SpeedSource{BatchMetadata: ref("batch.json"), Plan: ref("frozen-plan/plan.json"), Config: ref("frozen-plan/config.json"), Bundle: ref("frozen-plan/bundle.json"), PlanSHA256: hash, ExperimentID: "preview", WorkloadID: "speed"}
	result.Metadata.Model.DisplayName, result.Metadata.Model.SHA256, result.Metadata.Model.TemplateSHA256 = r.model, hash, hash
	result.Metadata.Engine.Adapter, result.Metadata.Engine.Release, result.Metadata.Engine.BinarySHA256 = "prism_llama_server_v1", r.engine, hash
	result.Metadata.Bundle.BundleID, result.Metadata.Bundle.Version = "zh-context-speed", "2026.10.1"
	result.Metadata.SelectedContextTokens = r.context
	result.Execution.Counts.Planned, result.Execution.Counts.Completed, result.Execution.Counts.NotExecuted = 5, completed, 5-completed
	result.Execution.StopReason = previewMissing[string]("unknown")
	if completed > 0 {
		stop := "user_cancelled"
		if completed == 5 {
			stop = "plan_finished"
		}
		result.Execution.StopReason = previewMeasured(stop, ref("batch.json"))
	}
	result.Measurement = inferyard.SpeedMeasurement{Status: "incomplete", ValidCount: completed, Reasons: []string{"five_valid_samples_required", "cohort_unknown", "aggregate_context_mismatch", "aggregate_prompt_length_mismatch"}}
	for i := range 5 {
		sample := inferyard.SpeedSample{TrialID: fmt.Sprintf("trial-%d", i), RepeatIndex: i, CaseID: map[int]string{65536: "context-speed-64k", 131072: "context-speed-128k", 262144: "context-speed-256k"}[r.context], ExecutionState: "not_executed", StopReason: previewMissing[string]("unknown")}
		sample.Proof.Identity, sample.Proof.FixedOutput = []inferyard.SpeedRef{}, []inferyard.SpeedRef{}
		sample.Columns = inferyard.SpeedColumns{Context: previewMissing[int64]("not_executed"), PromptLength: previewMissing[int64]("not_executed"), Time: previewMissing[float64]("not_executed"), Prefill: previewMissing[float64]("not_executed"), Decode: previewMissing[float64]("not_executed")}
		sample.Measurement.Reasons = []string{"request_not_completed", "sealed_run_required", "request_missing", "identity_evidence_missing", "token_budget_evidence_missing", "request_evidence_missing", "fixed_output_evidence_missing", "context_unverified", "actual_input_unknown", "final_timing_missing", "metric_missing"}
		if i < completed {
			sample = previewCompletedSample(r, i, ref, hash)
		}
		if missing && i == 4 && i < completed {
			sample.Proof.FinalTiming = nil
			sample.Columns.Time, sample.Columns.Prefill, sample.Columns.Decode = previewMissing[float64]("final_timing_missing"), previewMissing[float64]("final_timing_missing"), previewMissing[float64]("final_timing_missing")
			sample.Measurement.Valid = false
			sample.Measurement.Reasons = []string{"final_timing_missing", "metric_missing"}
			result.Measurement.ValidCount--
		}
		result.Samples = append(result.Samples, sample)
	}
	if completed == 5 {
		result.Measurement.Reasons = []string{"five_valid_samples_required"}
		if !missing {
			result.Measurement.Status, result.Measurement.Reasons = "complete", []string{}
			summary := &inferyard.SpeedSummary{TrialIDs: []string{}}
			for _, s := range result.Samples {
				summary.TrialIDs = append(summary.TrialIDs, s.TrialID)
			}
			summary.Columns.Context, summary.Columns.PromptLength = result.Samples[0].Columns.Context, result.Samples[0].Columns.PromptLength
			summary.Columns.Time = inferyard.SpeedStatistics{Min: *result.Samples[0].Columns.Time.Value, Median: *result.Samples[2].Columns.Time.Value, Max: *result.Samples[4].Columns.Time.Value}
			summary.Columns.Prefill = inferyard.SpeedStatistics{Min: *result.Samples[4].Columns.Prefill.Value, Median: *result.Samples[2].Columns.Prefill.Value, Max: *result.Samples[0].Columns.Prefill.Value}
			summary.Columns.Decode = inferyard.SpeedStatistics{Min: 42, Median: 42, Max: 42}
			result.Summary = summary
		}
	}
	return result
}

func previewCompletedSample(r *speedTask, i int, ref func(string) inferyard.SpeedRef, hash string) inferyard.SpeedSample {
	values := previewSpeedSample(r.context, i)
	path := fmt.Sprintf("runs/preview-%d", i)
	request := fmt.Sprintf("request-%d", i)
	sample := inferyard.SpeedSample{TrialID: fmt.Sprintf("trial-%d", i), RepeatIndex: i, CaseID: map[int]string{65536: "context-speed-64k", 131072: "context-speed-128k", 262144: "context-speed-256k"}[r.context], ExecutionState: "completed", RequestID: &request}
	sample.Run = &inferyard.SpeedRunRef{Path: path, RunID: fmt.Sprintf("run-%d", i), Inputs: ref(path + "/inputs.json"), ManifestSHA256: &hash, Integrity: "sealed"}
	sample.StopReason = previewMeasured("completed", ref(path+"/events.jsonl"))
	sample.Columns = inferyard.SpeedColumns{Context: previewMeasured(int64(r.context), ref(path+"/inputs.json")), PromptLength: previewMeasured(int64(*values.prompt), ref(path+"/events.jsonl")), Time: previewMeasured(*values.seconds, ref(path+"/events.jsonl")), Prefill: previewMeasured(*values.prefill, ref(path+"/events.jsonl")), Decode: previewMeasured(*values.decode, ref(path+"/events.jsonl"))}
	sample.Measurement.Valid, sample.Measurement.Reasons, sample.Measurement.CohortSHA256 = true, []string{}, &hash
	sample.Proof.Identity, sample.Proof.FixedOutput = []inferyard.SpeedRef{ref(path + "/inputs.json")}, []inferyard.SpeedRef{ref(path + "/events.jsonl")}
	token := ref(path + "/events.jsonl")
	sample.Proof.TokenBudget = &token
	req := ref(path + "/requests.jsonl")
	req.Member, _ = json.Marshal(map[string]string{"kind": "request", "request_id": request, "sha256": hash})
	sample.Proof.Request = &req
	raw, _ := json.Marshal(map[string]any{"file": ref(path + "/events.jsonl"), "event_seq": 1, "record": inferyard.SpeedTiming{Definition: "prism.adfffbe41.timings.v1", Final: true, CacheN: previewValue(int64(0)), PromptN: previewValue(int64(*values.prompt)), PredictedN: previewValue(int64(256)), PromptMS: previewValue(*values.seconds * 1000), PredictedMS: previewValue(255.0 / 42 * 1000)}})
	_ = json.Unmarshal(raw, &sample.Proof.FinalTiming)
	return sample
}

func previewMissing[T any](reason string) inferyard.SpeedValue[T] {
	return inferyard.SpeedValue[T]{MissingReason: &reason, EvidenceRefs: []inferyard.SpeedRef{}}
}
func previewMeasured[T any](value T, ref inferyard.SpeedRef) inferyard.SpeedValue[T] {
	return inferyard.SpeedValue[T]{Value: &value, EvidenceRefs: []inferyard.SpeedRef{ref}}
}
