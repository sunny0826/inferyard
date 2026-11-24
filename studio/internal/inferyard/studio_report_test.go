package inferyard

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed testdata/studio-report.json
var studioReportFixture []byte

func studioDocument(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var doc map[string]any
	if err := decoder.Decode(&doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func studioRun(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	runs, ok := doc["runs"].([]any)
	if !ok || len(runs) == 0 {
		t.Fatal("fixture has no runs")
	}
	run, ok := runs[0].(map[string]any)
	if !ok {
		t.Fatal("fixture run is not an object")
	}
	return run
}

func studioBytes(t *testing.T, doc any) []byte {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseStudioReportRejectsInvariantViolations(t *testing.T) {
	cases := map[string]func(map[string]any){
		"duplicate run": func(doc map[string]any) {
			doc["runs"] = append(doc["runs"].([]any), studioRun(t, doc))
		},
		"completion above denominator": func(doc map[string]any) {
			rate := studioRun(t, doc)["summary"].(map[string]any)["completion_rate"].(map[string]any)
			rate["numerator"] = 4
		},
		"quality not the category sum": func(doc map[string]any) {
			quality := studioRun(t, doc)["dashboard"].(map[string]any)["quality"].(map[string]any)
			quality["numerator"] = 2
		},
		"quality reason with complete": func(doc map[string]any) {
			quality := studioRun(t, doc)["dashboard"].(map[string]any)["quality"].(map[string]any)
			quality["complete"] = true
		},
		"incomplete quality without reason": func(doc map[string]any) {
			quality := studioRun(t, doc)["dashboard"].(map[string]any)["quality"].(map[string]any)
			quality["reason"] = nil
		},
		"latency counts skip a request": func(doc map[string]any) {
			latency := studioRun(t, doc)["dashboard"].(map[string]any)["latency"].(map[string]any)
			latency["excluded"] = 0
		},
		"p95 below twenty samples": func(doc map[string]any) {
			latency := studioRun(t, doc)["dashboard"].(map[string]any)["latency"].(map[string]any)
			latency["p95"] = 1000
		},
		"unordered latency": func(doc map[string]any) {
			latency := studioRun(t, doc)["dashboard"].(map[string]any)["latency"].(map[string]any)
			latency["min"] = 5000
		},
		"empty latency with quantiles": func(doc map[string]any) {
			latency := studioRun(t, doc)["dashboard"].(map[string]any)["latency"].(map[string]any)
			latency["sample_count"], latency["excluded"], latency["min"], latency["p50"], latency["max"] = 0, 3, 842, 842, 1204
		},
		"missing memory without reason": func(doc map[string]any) {
			dashboard := studioRun(t, doc)["dashboard"].(map[string]any)
			dashboard["sampled_peak_rss_reason"] = nil
		},
		"present memory with reason": func(doc map[string]any) {
			dashboard := studioRun(t, doc)["dashboard"].(map[string]any)
			dashboard["sampled_peak_rss_bytes"] = json.Number("1024")
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			doc := studioDocument(t, studioReportFixture)
			mutate(doc)
			if _, err := ParseStudioReport(studioBytes(t, doc)); err == nil {
				t.Fatal("invariant violation accepted")
			}
		})
	}
}

func TestStudioReportParserMatchesExportedSchema(t *testing.T) {
	schema := compileStudioSchema(t)
	for name, raw := range studioSchemaSamples(t) {
		t.Run(name, func(t *testing.T) {
			generic := studioDocument(t, raw)
			schemaErr := schema.Validate(generic)
			_, parseErr := ParseStudioReport(raw)
			if (schemaErr == nil) != (parseErr == nil) {
				t.Fatalf("schema err=%v parser err=%v", schemaErr, parseErr)
			}
		})
	}
}

func studioSchemaSamples(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	samples := map[string]json.RawMessage{
		"valid fixture":           studioReportFixture,
		"valid optional sections": studioDocumentWithSections(t),
	}
	for _, probe := range studioSchemaMutations() {
		samples[probe.name] = probe.document(t)
	}
	return samples
}

func studioSchemaMutations() []studioProbe {
	return []studioProbe{
		{"unknown run field", []string{"runs", "0", "extra"}, studioRaw("1"), false},
		{"missing facets", []string{"runs", "0", "facets"}, nil, false},
		{"null model", []string{"runs", "0", "model"}, studioRaw("null"), false},
		{"bool threads", []string{"runs", "0", "threads"}, studioRaw("true"), false},
		{"bad backend", []string{"runs", "0", "profile", "engine", "backend"}, studioRaw(`"gpu"`), false},
		{"bad state", []string{"runs", "0", "requests", "0", "state"}, studioRaw(`"unknown"`), false},
		{"unknown root field", []string{"extra"}, studioRaw("1"), false},
		{"unknown throughput field", studioDashboardPath("throughput", "excluded_count"), studioRaw("0"), true},
		{"missing throughput reason", studioDashboardPath("throughput", "goodput_per_second_reason"), nil, true},
		{"null throughput section", studioDashboardPath("throughput"), studioRaw("null"), false},
		{"unknown interval field", studioDashboardPath("output_block_intervals", "excluded_interval_count"), studioRaw("0"), true},
		{"fractional interval count", studioDashboardPath("output_block_intervals", "interval_count"), studioRaw("1.5"), true},
		{"empty svg outputs", []string{"runs", "0", "svg_outputs"}, studioRaw("[]"), false},
		{"null svg outputs", []string{"runs", "0", "svg_outputs"}, studioRaw("null"), false},
		{"bad svg status", []string{"runs", "0", "svg_outputs", "0", "status"}, studioRaw(`"pending"`), true},
		{"unknown svg field", []string{"runs", "0", "svg_outputs", "0", "extra"}, studioRaw("1"), true},
		{"ok svg null payload", []string{"runs", "0", "svg_outputs", "0", "svg"}, studioRaw("null"), true},
		{"not_found svg nonzero bytes", []string{"runs", "0", "svg_outputs", "0"}, studioRaw(`{"case_id":"svg-pelican-01","prompt":"p","status":"not_found","svg":null,"bytes":1}`), true},
		{"too_large svg within limit", []string{"runs", "0", "svg_outputs", "0"}, studioRaw(`{"case_id":"svg-pelican-01","prompt":"p","status":"too_large","svg":null,"bytes":1}`), true},
	}
}

func compileStudioSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	path := "../../../cli/schemas/studio_report.schema.json"
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		t.Skip("repository schema not available")
	}
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("studio_report.schema.json", doc); err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile("studio_report.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

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

func studioSVGOutputsSection() []map[string]json.RawMessage {
	svg := `<svg xmlns="http://www.w3.org/2000/svg"><circle r="4"/></svg>`
	encoded, _ := json.Marshal(svg)
	return []map[string]json.RawMessage{
		{
			"case_id": studioRaw(`"svg-pelican-01"`),
			"prompt":  studioRaw(`"Generate an animated SVG of a pelican riding a bicycle"`),
			"status":  studioRaw(`"ok"`),
			"svg":     encoded,
			"bytes":   studioRaw(strconv.Itoa(len(svg))),
		},
	}
}

func studioDocumentWithSections(t *testing.T) json.RawMessage {
	t.Helper()
	raw := studioPatch(t, studioReportFixture, studioDashboardPath("throughput"), studioBytes(t, studioThroughputSection()))
	raw = studioPatch(t, raw, studioDashboardPath("output_block_intervals"), studioBytes(t, studioIntervalsSection()))
	return studioPatch(t, raw, []string{"runs", "0", "svg_outputs"}, studioBytes(t, studioSVGOutputsSection()))
}

func TestParseStudioReportSVGOutputsInvariants(t *testing.T) {
	valid := studioDocumentWithSections(t)
	parsed, err := ParseStudioReport(valid)
	if err != nil {
		t.Fatalf("expected valid svg_outputs report, got: %v", err)
	}
	if len(parsed.Runs[0].SVGOutputs) != 1 || parsed.Runs[0].SVGOutputs[0].Status != "ok" || parsed.Runs[0].SVGOutputs[0].SVG == nil {
		t.Fatalf("unexpected parsed svg_outputs: %+v", parsed.Runs[0].SVGOutputs)
	}
	cases := map[string]func(map[string]any){
		"ok without svg": func(item map[string]any) { item["svg"] = nil },
		"not_found with svg": func(item map[string]any) {
			item["status"] = "not_found"
			item["bytes"] = 0
		},
		"not_found nonzero bytes": func(item map[string]any) {
			item["status"] = "not_found"
			item["svg"] = nil
			item["bytes"] = 12
		},
		"ok byte mismatch": func(item map[string]any) { item["bytes"] = 1 },
		"too_large within limit": func(item map[string]any) {
			item["status"] = "too_large"
			item["svg"] = nil
			item["bytes"] = StudioMaxSVGBytes
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			doc := studioDocument(t, valid)
			items := studioRun(t, doc)["svg_outputs"].([]any)
			mutate(items[0].(map[string]any))
			if _, err := ParseStudioReport(studioBytes(t, doc)); err == nil {
				t.Fatal("svg_outputs invariant violation accepted")
			}
		})
	}
}
