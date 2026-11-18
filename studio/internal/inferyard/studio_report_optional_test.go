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
