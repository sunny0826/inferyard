package inferyard

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"os"
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
