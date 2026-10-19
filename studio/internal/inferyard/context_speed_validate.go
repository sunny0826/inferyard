package inferyard

import (
	"encoding/json"
	"math"
	"reflect"
	"slices"
	"strings"
)

func validSpeedResult(r ContextSpeedResult) bool {
	m := r.Metadata
	contextCase := map[int]string{65536: "context-speed-64k", 131072: "context-speed-128k", 262144: "context-speed-256k"}[m.SelectedContextTokens]
	if contextCase == "" || m.Engine.Adapter != "prism_llama_server_v1" || m.Bundle.BundleID != "zh-context-speed" || m.Bundle.Version != "2026.10.1" ||
		!speedHash.MatchString(m.Model.SHA256) || !speedHash.MatchString(m.Model.TemplateSHA256) || !speedHash.MatchString(m.Engine.BinarySHA256) ||
		!speedHash.MatchString(r.Source.PlanSHA256) || !speedID.MatchString(r.Source.ExperimentID) || !speedID.MatchString(r.Source.WorkloadID) || len(r.Samples) != 5 {
		return false
	}
	for _, ref := range speedRefs(reflect.ValueOf(r)) {
		if !validSpeedRef(ref) {
			return false
		}
	}
	if !validSpeedValue(r.Execution.StopReason) {
		return false
	}
	counts := map[string]int{}
	seen := map[string]bool{}
	unique := func(kind, id string) bool {
		key := kind + ":" + id
		if seen[key] {
			return false
		}
		seen[key] = true
		return true
	}
	valid, runs := 0, 0
	for i, s := range r.Samples {
		if s.RepeatIndex != i || s.CaseID != contextCase || !speedID.MatchString(s.TrialID) || !unique("trial", s.TrialID) || !validSpeedSample(s, m.SelectedContextTokens) {
			return false
		}
		if s.RequestID != nil && (!speedID.MatchString(*s.RequestID) || !unique("request", *s.RequestID)) {
			return false
		}
		if s.Run != nil {
			runs++
			if !speedID.MatchString(s.Run.RunID) || !unique("run", s.Run.RunID) || !unique("path", s.Run.Path) {
				return false
			}
		}
		counts[s.ExecutionState]++
		if s.Measurement.Valid {
			valid++
		}
	}
	c := r.Execution.Counts
	if c.Planned != 5 || c.Completed != counts["completed"] || c.Failed != counts["failed"] || c.Cancelled != counts["cancelled"] || c.Invalid != counts["invalid"] || c.NotExecuted != counts["not_executed"] {
		return false
	}
	if runs == 0 && (r.Execution.StopReason.Value != nil || r.Execution.StopReason.MissingReason == nil || *r.Execution.StopReason.MissingReason != "unknown") {
		return false
	}
	reasons := []string{}
	if valid != 5 {
		reasons = append(reasons, "five_valid_samples_required")
	}
	unknown, mismatch := false, false
	for _, s := range r.Samples {
		if s.Measurement.CohortSHA256 == nil {
			unknown = true
		} else if r.Samples[0].Measurement.CohortSHA256 != nil && *s.Measurement.CohortSHA256 != *r.Samples[0].Measurement.CohortSHA256 {
			mismatch = true
		}
	}
	if unknown {
		reasons = append(reasons, "cohort_unknown")
	} else if mismatch {
		reasons = append(reasons, "cohort_mismatch")
	}
	for _, key := range []string{"context", "prompt_length"} {
		first := r.Samples[0].Columns.Context.Value
		if key == "prompt_length" {
			first = r.Samples[0].Columns.PromptLength.Value
		}
		for _, s := range r.Samples {
			value := s.Columns.Context.Value
			if key == "prompt_length" {
				value = s.Columns.PromptLength.Value
			}
			if first == nil || value == nil || *first != *value {
				reasons = append(reasons, "aggregate_"+key+"_mismatch")
				break
			}
		}
	}
	if r.Measurement.ValidCount != valid || !speedReasons(r.Measurement.Reasons, reasons) {
		return false
	}
	complete := len(r.Measurement.Reasons) == 0
	if (r.Measurement.Status == "complete") != complete || !containsSpeed([]string{"complete", "incomplete"}, r.Measurement.Status) || (r.Summary != nil) != complete {
		return false
	}
	if !complete {
		return true
	}
	summary := r.Summary
	if len(summary.TrialIDs) != 5 || !validSpeedValue(summary.Columns.Context) || !validSpeedValue(summary.Columns.PromptLength) {
		return false
	}
	for i, s := range r.Samples {
		if summary.TrialIDs[i] != s.TrialID {
			return false
		}
	}
	if summary.Columns.Context.Value == nil || summary.Columns.PromptLength.Value == nil || *summary.Columns.Context.Value != *r.Samples[0].Columns.Context.Value || *summary.Columns.PromptLength.Value != *r.Samples[0].Columns.PromptLength.Value {
		return false
	}
	for i, stat := range []SpeedStatistics{summary.Columns.Time, summary.Columns.Prefill, summary.Columns.Decode} {
		values := []float64{}
		for _, s := range r.Samples {
			v := []*float64{s.Columns.Time.Value, s.Columns.Prefill.Value, s.Columns.Decode.Value}[i]
			if v == nil {
				return false
			}
			values = append(values, *v)
		}
		slices.Sort(values)
		// Validate the supplied statistic, never replace it with a locally computed result.
		if stat.Min != values[0] || stat.Median != values[2] || stat.Max != values[4] {
			return false
		}
	}
	return true
}

func speedReasons(declared, required []string) bool {
	seen := map[string]bool{}
	for _, s := range declared {
		if s == "" || seen[s] {
			return false
		}
		seen[s] = true
	}
	for _, s := range required {
		if !seen[s] {
			return false
		}
	}
	return true
}

func speedRefs(v reflect.Value) []SpeedRef {
	if !v.IsValid() {
		return nil
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		return speedRefs(v.Elem())
	}
	if v.Type() == reflect.TypeOf(SpeedRef{}) {
		return []SpeedRef{v.Interface().(SpeedRef)}
	}
	var out []SpeedRef
	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			out = append(out, speedRefs(v.Field(i))...)
		}
	case reflect.Slice:
		if v.Type() != reflect.TypeOf(json.RawMessage{}) {
			for i := 0; i < v.Len(); i++ {
				out = append(out, speedRefs(v.Index(i))...)
			}
		}
	}
	return out
}

func validSpeedSample(s SpeedSample, context int) bool {
	c, p := s.Columns, s.Proof
	if !containsSpeed([]string{"completed", "failed", "cancelled", "invalid", "not_executed"}, s.ExecutionState) || !validSpeedValue(s.StopReason) ||
		!validSpeedValue(c.Context) || !validSpeedValue(c.PromptLength) || !validSpeedValue(c.Time) || !validSpeedValue(c.Prefill) || !validSpeedValue(c.Decode) {
		return false
	}
	if c.Context.Value != nil && !slices.Contains([]int64{65536, 131072, 262144}, *c.Context.Value) || c.PromptLength.Value != nil && *c.PromptLength.Value < 1 {
		return false
	}
	if s.Measurement.CohortSHA256 != nil && !speedHash.MatchString(*s.Measurement.CohortSHA256) {
		return false
	}
	if s.Run == nil {
		if s.ExecutionState != "not_executed" || s.RequestID != nil || len(speedRefs(reflect.ValueOf(p))) != 0 || s.Measurement.CohortSHA256 != nil || c.Context.Value != nil || c.PromptLength.Value != nil || c.Time.Value != nil || c.Prefill.Value != nil || c.Decode.Value != nil {
			return false
		}
	} else {
		run := s.Run
		if !speedPath(run.Path) || !containsSpeed([]string{"sealed", "unsealed"}, run.Integrity) || run.Integrity == "sealed" && run.ManifestSHA256 == nil || run.ManifestSHA256 != nil && !speedHash.MatchString(*run.ManifestSHA256) {
			return false
		}
		refs := append([]SpeedRef{run.Inputs}, speedRefs(reflect.ValueOf(p))...)
		refs = append(refs, speedRefs(reflect.ValueOf(c))...)
		for _, ref := range refs {
			if !strings.HasPrefix(ref.Path, run.Path+"/") {
				return false
			}
			var m struct {
				Kind      string `json:"kind"`
				RequestID string `json:"request_id"`
			}
			_ = json.Unmarshal(ref.Member, &m)
			if m.Kind == "request" && (s.RequestID == nil || m.RequestID != *s.RequestID) {
				return false
			}
		}
		if p.Request != nil {
			var m struct {
				Kind      string `json:"kind"`
				RequestID string `json:"request_id"`
			}
			_ = json.Unmarshal(p.Request.Member, &m)
			if m.Kind != "request" || s.RequestID == nil || m.RequestID != *s.RequestID {
				return false
			}
		}
	}
	defects := []string{}
	need := func(ok bool, reason string) {
		if !ok {
			defects = append(defects, reason)
		}
	}
	need(s.ExecutionState == "completed", "request_not_completed")
	need(s.Run != nil && s.Run.Integrity == "sealed", "sealed_run_required")
	need(s.RequestID != nil, "request_missing")
	need(len(p.Identity) > 0, "identity_evidence_missing")
	need(p.TokenBudget != nil, "token_budget_evidence_missing")
	need(p.Request != nil, "request_evidence_missing")
	need(len(p.FixedOutput) > 0, "fixed_output_evidence_missing")
	need(c.Context.Value != nil && *c.Context.Value == int64(context), "context_unverified")
	need(c.PromptLength.Value != nil, "actual_input_unknown")
	if c.PromptLength.Value != nil {
		need(*c.PromptLength.Value <= int64(context)-256, "context_budget_exceeded")
	}
	if p.FinalTiming == nil {
		defects = append(defects, "final_timing_missing")
		if c.Time.Value != nil || c.Prefill.Value != nil || c.Decode.Value != nil {
			return false
		}
	} else {
		timing := p.FinalTiming.Record
		if p.FinalTiming.EventSeq <= 0 || !validSpeedTiming(timing, s) {
			return false
		}
		need(timing.Final && !timing.Speculative && timing.MissingReason == nil, "final_timing_unqualified")
		need(timing.CacheN != nil && *timing.CacheN == 0, "cold_cache_unproven")
		need(c.PromptLength.Value != nil && timing.PromptN != nil && timing.CacheN != nil && *timing.PromptN <= math.MaxInt64-*timing.CacheN && *timing.PromptN+*timing.CacheN == *c.PromptLength.Value, "input_token_mismatch")
		need(timing.PredictedN != nil && *timing.PredictedN == 256, "fixed_output_not_reached")
	}
	need(c.Time.Value != nil && c.Prefill.Value != nil && c.Decode.Value != nil, "metric_missing")
	return s.Measurement.Valid == (len(s.Measurement.Reasons) == 0) && speedReasons(s.Measurement.Reasons, defects)
}

func validSpeedTiming(t SpeedTiming, s SpeedSample) bool {
	if t.Definition != "prism.adfffbe41.timings.v1" {
		return false
	}
	all := t.CacheN != nil && t.PromptN != nil && t.PredictedN != nil && t.PromptMS != nil && t.PredictedMS != nil
	if all {
		if t.MissingReason != nil || *t.PromptMS > 1e15 || *t.PredictedMS > 1e15 {
			return false
		}
	} else if t.CacheN != nil || t.PromptN != nil || t.PredictedN != nil || t.PromptMS != nil || t.PredictedMS != nil || t.MissingReason == nil || *t.MissingReason != "invalid_engine_timings" {
		return false
	}
	c := s.Columns
	close := func(a, b float64) bool { return math.Abs(a-b) <= 1e-12*math.Max(math.Abs(a), math.Abs(b)) }
	if c.Time.Value != nil && (!all || !t.Final || t.Speculative || *t.PromptMS <= 0 || !close(*c.Time.Value, *t.PromptMS/1000)) {
		return false
	}
	for i, v := range []*float64{c.Prefill.Value, c.Decode.Value} {
		if v == nil {
			continue
		}
		if !all || !t.Final || t.Speculative || s.ExecutionState != "completed" {
			return false
		}
		count, duration := *t.PromptN, *t.PromptMS
		if i == 1 {
			count, duration = max(0, *t.PredictedN-1), *t.PredictedMS
		}
		if count <= 0 || duration <= 0 || !close(*v, float64(count)/(duration/1000)) {
			return false
		}
	}
	return true
}
