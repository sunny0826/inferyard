package inferyard

import (
	"encoding/json"
	"strconv"
	"testing"
)

// studioRaw keeps exact JSON bytes so probes can carry nulls, fractional
// numbers and unknown keys without decoding into a broad value.
func studioRaw(value string) json.RawMessage {
	return json.RawMessage(value)
}

func studioRawObject(t *testing.T, raw json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("decode studio fixture object: %v", err)
	}
	return obj
}

func studioRawArray(t *testing.T, raw json.RawMessage) []json.RawMessage {
	t.Helper()
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatalf("decode studio fixture array: %v", err)
	}
	return items
}

// studioPatch returns the document with one value replaced at an object-key and
// array-index path; a nil value deletes the addressed object key.
func studioPatch(t *testing.T, raw json.RawMessage, path []string, value json.RawMessage) json.RawMessage {
	t.Helper()
	patched := studioRewrite(t, raw, path, value)
	if patched == nil {
		t.Fatal("studio fixture root cannot be deleted")
	}
	return patched
}

func studioRewrite(t *testing.T, raw json.RawMessage, path []string, value json.RawMessage) json.RawMessage {
	t.Helper()
	if len(path) == 0 {
		return value
	}
	if index, err := strconv.Atoi(path[0]); err == nil {
		items := studioRawArray(t, raw)
		if index < 0 || index >= len(items) {
			t.Fatalf("studio fixture index %d out of range", index)
		}
		items[index] = studioRewrite(t, items[index], path[1:], value)
		return studioBytes(t, items)
	}
	obj := studioRawObject(t, raw)
	child := studioRewrite(t, obj[path[0]], path[1:], value)
	if child == nil {
		delete(obj, path[0])
		return studioBytes(t, obj)
	}
	obj[path[0]] = child
	return studioBytes(t, obj)
}

func studioDashboardPath(keys ...string) []string {
	return append([]string{"runs", "0", "dashboard"}, keys...)
}

// studioProbe is one malformed-document edit applied to a fixture variant.
type studioProbe struct {
	name     string
	path     []string
	value    json.RawMessage
	sections bool
}

func (probe studioProbe) document(t *testing.T) json.RawMessage {
	t.Helper()
	if !probe.sections {
		return studioPatch(t, studioReportFixture, probe.path, probe.value)
	}
	return studioPatch(t, studioDocumentWithSections(t), probe.path, probe.value)
}

func rejectStudioReport(t *testing.T, probes []studioProbe) {
	t.Helper()
	for _, probe := range probes {
		t.Run(probe.name, func(t *testing.T) {
			if _, err := ParseStudioReport(probe.document(t)); err == nil {
				t.Fatalf("broken optional section accepted: %s", probe.name)
			}
		})
	}
}

func studioThroughputSection() map[string]json.RawMessage {
	return map[string]json.RawMessage{
		"requests_per_second":             studioRaw("0.25"),
		"requests_per_second_reason":      studioRaw("null"),
		"output_tokens_per_second":        studioRaw("12.5"),
		"output_tokens_per_second_reason": studioRaw("null"),
		"goodput_per_second":              studioRaw("0"),
		"goodput_per_second_reason":       studioRaw("null"),
	}
}

func studioIntervalsSection() map[string]json.RawMessage {
	return map[string]json.RawMessage{
		"interval_count": studioRaw("25"),
		"request_count":  studioRaw("3"),
		"min":            studioRaw("0"),
		"p50":            studioRaw("120.5"),
		"p95":            studioRaw("400"),
		"p95_reason":     studioRaw("null"),
		"max":            studioRaw("900.25"),
		"reason":         studioRaw("null"),
	}
}

func studioEmptyIntervalsSection() map[string]json.RawMessage {
	return map[string]json.RawMessage{
		"interval_count": studioRaw("0"),
		"request_count":  studioRaw("3"),
		"min":            studioRaw("null"),
		"p50":            studioRaw("null"),
		"p95":            studioRaw("null"),
		"p95_reason":     studioRaw(`"insufficient_samples"`),
		"max":            studioRaw("null"),
		"reason":         studioRaw(`"not_streaming"`),
	}
}

// studioIntervalsAt builds a measured section for one sample count; zero
// samples keep null quantiles and a reason.
func studioIntervalsAt(t *testing.T, count int64, p95, p95Reason string) map[string]json.RawMessage {
	t.Helper()
	section := studioIntervalsSection()
	section["interval_count"] = studioRaw(strconv.FormatInt(count, 10))
	section["p95"] = studioRaw(p95)
	section["p95_reason"] = studioRaw(p95Reason)
	if count == 0 {
		section["reason"] = studioRaw(`"not_streaming"`)
		for _, key := range []string{"min", "p50", "max"} {
			section[key] = studioRaw("null")
		}
	}
	return section
}

func studioDocumentWithSections(t *testing.T) json.RawMessage {
	t.Helper()
	raw := studioPatch(t, studioReportFixture, studioDashboardPath("throughput"), studioBytes(t, studioThroughputSection()))
	return studioPatch(t, raw, studioDashboardPath("output_block_intervals"), studioBytes(t, studioIntervalsSection()))
}

func TestParseStudioReportWithoutOptionalSections(t *testing.T) {
	report, err := ParseStudioReport(studioReportFixture)
	if err != nil {
		t.Fatal(err)
	}
	if dashboard := report.Runs[0].Dashboard; dashboard.Throughput != nil || dashboard.OutputBlockIntervals != nil {
		t.Fatalf("missing sections must stay nil: %+v", dashboard)
	}
}

func TestParseStudioReportDecodesOptionalSections(t *testing.T) {
	report, err := ParseStudioReport(studioDocumentWithSections(t))
	if err != nil {
		t.Fatal(err)
	}
	requireStudioThroughput(t, report.Runs[0].Dashboard.Throughput)
	requireStudioIntervals(t, report.Runs[0].Dashboard.OutputBlockIntervals)
}

func requireStudioThroughput(t *testing.T, throughput *StudioThroughput) {
	t.Helper()
	if throughput == nil {
		t.Fatal("throughput section not decoded")
	}
	if !studioNumberIs(throughput.RequestsPerSecond, 0.25) ||
		!studioNumberIs(throughput.OutputTokensPerSecond, 12.5) ||
		!studioNumberIs(throughput.GoodputPerSecond, 0) {
		t.Fatalf("throughput values not decoded: %+v", throughput)
	}
}

func requireStudioIntervals(t *testing.T, intervals *StudioOutputBlockIntervals) {
	t.Helper()
	if intervals == nil {
		t.Fatal("interval section not decoded")
	}
	if intervals.IntervalCount != 25 || intervals.RequestCount != 3 {
		t.Fatalf("interval counts not decoded: %+v", intervals)
	}
	if !studioNumberIs(intervals.Min, 0) || !studioNumberIs(intervals.P50, 120.5) ||
		!studioNumberIs(intervals.P95, 400) || !studioNumberIs(intervals.Max, 900.25) {
		t.Fatalf("interval quantiles not decoded: %+v", intervals)
	}
}

func studioNumberIs(value *float64, want float64) bool {
	return value != nil && *value == want
}

func requireStudioText(t *testing.T, label string, got *string, want string) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("%s = %v, want %q", label, got, want)
	}
}

func TestParseStudioReportKeepsMissingReasons(t *testing.T) {
	throughput := studioThroughputSection()
	throughput["goodput_per_second"] = studioRaw("null")
	throughput["goodput_per_second_reason"] = studioRaw(`"goodput_indeterminable"`)
	throughput["output_tokens_per_second"] = studioRaw("null")
	throughput["output_tokens_per_second_reason"] = studioRaw(`"output_token_counts_incomplete"`)
	raw := studioPatch(t, studioReportFixture, studioDashboardPath("throughput"), studioBytes(t, throughput))
	raw = studioPatch(t, raw, studioDashboardPath("output_block_intervals"), studioBytes(t, studioEmptyIntervalsSection()))
	report, err := ParseStudioReport(raw)
	if err != nil {
		t.Fatal(err)
	}
	dashboard := report.Runs[0].Dashboard
	if dashboard.Throughput == nil || dashboard.OutputBlockIntervals == nil {
		t.Fatalf("optional sections missing: %+v", dashboard)
	}
	requireStudioText(t, "goodput reason", dashboard.Throughput.GoodputPerSecondReason, "goodput_indeterminable")
	requireStudioText(t, "interval reason", dashboard.OutputBlockIntervals.Reason, "not_streaming")
	requireStudioText(t, "p95 reason", dashboard.OutputBlockIntervals.P95Reason, "insufficient_samples")
}

func TestParseStudioReportRejectsBrokenThroughput(t *testing.T) {
	rejectStudioReport(t, []studioProbe{
		{"null throughput section", studioDashboardPath("throughput"), studioRaw("null"), false},
		{"unknown throughput field", studioDashboardPath("throughput", "excluded_count"), studioRaw("0"), true},
		{"missing throughput reason", studioDashboardPath("throughput", "goodput_per_second_reason"), nil, true},
		{"throughput value without reason", studioDashboardPath("throughput", "requests_per_second"), studioRaw("null"), true},
		{"throughput reason without value", studioDashboardPath("throughput", "requests_per_second_reason"), studioRaw(`"no_formal_requests"`), true},
		{"negative throughput", studioDashboardPath("throughput", "requests_per_second"), studioRaw("-0.5"), true},
	})
}

func TestParseStudioReportRejectsBrokenZeroIntervals(t *testing.T) {
	withQuantiles := studioEmptyIntervalsSection()
	withQuantiles["min"] = studioRaw("0")
	withoutReason := studioEmptyIntervalsSection()
	withoutReason["reason"] = studioRaw("null")
	withoutP95Reason := studioEmptyIntervalsSection()
	withoutP95Reason["p95_reason"] = studioRaw("null")
	rejectStudioReport(t, []studioProbe{
		{"null interval section", studioDashboardPath("output_block_intervals"), studioRaw("null"), false},
		{"unknown interval field", studioDashboardPath("output_block_intervals", "excluded_interval_count"), studioRaw("0"), true},
		{"missing interval count", studioDashboardPath("output_block_intervals", "interval_count"), nil, true},
		{"fractional interval count", studioDashboardPath("output_block_intervals", "interval_count"), studioRaw("1.5"), true},
		{"negative request count", studioDashboardPath("output_block_intervals", "request_count"), studioRaw("-1"), true},
		{"zero intervals with quantiles", studioDashboardPath("output_block_intervals"), studioBytes(t, withQuantiles), false},
		{"zero intervals without reason", studioDashboardPath("output_block_intervals"), studioBytes(t, withoutReason), false},
		{"zero intervals without p95 reason", studioDashboardPath("output_block_intervals"), studioBytes(t, withoutP95Reason), false},
	})
}

func TestParseStudioReportRejectsBrokenMeasuredIntervals(t *testing.T) {
	belowTwenty := studioIntervalsAt(t, 5, "400", "null")
	rejectStudioReport(t, []studioProbe{
		{"measured intervals missing max", studioDashboardPath("output_block_intervals", "max"), studioRaw("null"), true},
		{"measured intervals with reason", studioDashboardPath("output_block_intervals", "reason"), studioRaw(`"insufficient_blocks"`), true},
		{"unordered intervals", studioDashboardPath("output_block_intervals", "min"), studioRaw("5000"), true},
		{"p95 below twenty samples", studioDashboardPath("output_block_intervals"), studioBytes(t, belowTwenty), false},
		{"p95 missing from twenty samples", studioDashboardPath("output_block_intervals", "p95"), studioRaw("null"), true},
		{"p95 reason from twenty samples", studioDashboardPath("output_block_intervals", "p95_reason"), studioRaw(`"insufficient_samples"`), true},
		{"unordered p95", studioDashboardPath("output_block_intervals", "p95"), studioRaw("0.5"), true},
	})
}
