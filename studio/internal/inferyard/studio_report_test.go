package inferyard

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"os"
	"strings"
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

func TestParseStudioReportAcceptsValidDocument(t *testing.T) {
	report, err := ParseStudioReport(studioReportFixture)
	if err != nil {
		t.Fatal(err)
	}
	if report.SchemaVersion != 3 || report.Kind != StudioReportKind || len(report.Runs) != 1 {
		t.Fatalf("%+v", report)
	}
	run := report.Runs[0]
	if run.Source.RunID != "fixture-run" || run.Model != "fixture-model" || *run.Threads != 8 || run.Diagnostic {
		t.Fatalf("%+v", run)
	}
	if run.Profile.Engine.Adapter != "prism_llama_server_v1" || run.Profile.Engine.Backend != "metal" {
		t.Fatalf("%+v", run.Profile.Engine)
	}
	if run.Profile.Title == nil || *run.Profile.Title != "Fixture Model Q4" {
		t.Fatalf("%+v", run.Profile)
	}
	if *run.Summary.CompletionRate.Numerator != 2 || run.Summary.StopReason != "plan_finished" {
		t.Fatalf("%+v", run.Summary)
	}
	if run.Dashboard.Quality.Complete || run.Dashboard.Quality.Reason == nil {
		t.Fatalf("%+v", run.Dashboard.Quality)
	}
	if *run.Dashboard.Latency.SampleCount != 2 || *run.Dashboard.Latency.Excluded != 1 || run.Dashboard.Latency.Min == nil || *run.Dashboard.Latency.Min != 842 {
		t.Fatalf("%+v", run.Dashboard.Latency)
	}
	if len(run.Requests) != 3 || run.Requests[0].Quality != nil || *run.Requests[2].LatencyMS != 1204 {
		t.Fatalf("%+v", run.Requests)
	}
}

func TestParseStudioReportAllowsNullableFields(t *testing.T) {
	doc := studioDocument(t, studioReportFixture)
	run := studioRun(t, doc)
	run["threads"] = nil
	run["profile"].(map[string]any)["title"] = nil
	engine := run["profile"].(map[string]any)["engine"].(map[string]any)
	engine["binary_path"] = nil
	machine := run["profile"].(map[string]any)["machine"].(map[string]any)
	machine["cpu"], machine["logical_cpus"], machine["memory_total_bytes"] = nil, nil, nil
	requests := run["requests"].([]any)
	requests[0].(map[string]any)["quality"] = nil
	if _, err := ParseStudioReport(studioBytes(t, doc)); err != nil {
		t.Fatal(err)
	}
}

func TestParseStudioReportRejectsStructuralViolations(t *testing.T) {
	cases := map[string]func(map[string]any){
		"wrong kind":            func(doc map[string]any) { doc["kind"] = "studio-report.v2" },
		"wrong version":         func(doc map[string]any) { doc["schema_version"] = 2 },
		"empty runs":            func(doc map[string]any) { doc["runs"] = []any{} },
		"unknown top-level key": func(doc map[string]any) { doc["extra"] = 1 },
		"unknown run key": func(doc map[string]any) {
			studioRun(t, doc)["extra"] = 1
		},
		"unknown engine key": func(doc map[string]any) {
			engine := studioRun(t, doc)["profile"].(map[string]any)["engine"].(map[string]any)
			engine["release"] = "x"
		},
		"missing facets": func(doc map[string]any) {
			delete(studioRun(t, doc), "facets")
		},
		"missing latency p50": func(doc map[string]any) {
			latency := studioRun(t, doc)["dashboard"].(map[string]any)["latency"].(map[string]any)
			delete(latency, "p50")
		},
		"missing minimum reason": func(doc map[string]any) {
			delete(studioRun(t, doc)["dashboard"].(map[string]any), "minimum_available_reason")
		},
		"null model": func(doc map[string]any) {
			studioRun(t, doc)["model"] = nil
		},
		"null threads is rejected when bool": func(doc map[string]any) {
			studioRun(t, doc)["threads"] = true
		},
		"bool numerator": func(doc map[string]any) {
			quality := studioRun(t, doc)["dashboard"].(map[string]any)["quality"].(map[string]any)
			quality["numerator"] = true
		},
		"invalid backend": func(doc map[string]any) {
			engine := studioRun(t, doc)["profile"].(map[string]any)["engine"].(map[string]any)
			engine["backend"] = "gpu"
		},
		"invalid run id": func(doc map[string]any) {
			studioRun(t, doc)["source"].(map[string]any)["run_id"] = "bad id!"
		},
		"invalid request state": func(doc map[string]any) {
			requests := studioRun(t, doc)["requests"].([]any)
			requests[0].(map[string]any)["state"] = "unknown"
		},
		"invalid request quality": func(doc map[string]any) {
			requests := studioRun(t, doc)["requests"].([]any)
			requests[1].(map[string]any)["quality"] = "maybe"
		},
		"invalid category id": func(doc map[string]any) {
			categories := studioRun(t, doc)["dashboard"].(map[string]any)["categories"].([]any)
			categories[0].(map[string]any)["id"] = "other"
		},
		"too many categories": func(doc map[string]any) {
			dashboard := studioRun(t, doc)["dashboard"].(map[string]any)
			base := dashboard["categories"].([]any)[0]
			for range 5 {
				dashboard["categories"] = append(dashboard["categories"].([]any), base)
			}
		},
		"duplicate category": func(doc map[string]any) {
			dashboard := studioRun(t, doc)["dashboard"].(map[string]any)
			categories := dashboard["categories"].([]any)
			categories[1].(map[string]any)["id"] = categories[0].(map[string]any)["id"]
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			doc := studioDocument(t, studioReportFixture)
			mutate(doc)
			if _, err := ParseStudioReport(studioBytes(t, doc)); err == nil {
				t.Fatal("structural violation accepted")
			}
		})
	}
	if _, err := ParseStudioReport(append(append([]byte{}, studioReportFixture...), []byte(`{}`)...)); err == nil {
		t.Fatal("trailing JSON accepted")
	}
	duplicate := strings.Replace(string(studioReportFixture), `"kind": "studio-report.v1"`, `"kind": "studio-report.v1", "kind": "studio-report.v1"`, 1)
	if _, err := ParseStudioReport([]byte(duplicate)); err == nil {
		t.Fatal("duplicate JSON key accepted")
	}
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

// The repository Schema is the structural authority; cross-field rules above
// are parser-owned, mirroring cli/contracts/schemas_studio_report.py.
func TestStudioReportParserMatchesExportedSchema(t *testing.T) {
	schema := compileStudioSchema(t)
	valid := studioDocument(t, studioReportFixture)
	samples := map[string]map[string]any{"valid fixture": valid}
	mutations := map[string]func(map[string]any){
		"unknown run field": func(doc map[string]any) { studioRun(t, doc)["extra"] = 1 },
		"missing facets":    func(doc map[string]any) { delete(studioRun(t, doc), "facets") },
		"null model":        func(doc map[string]any) { studioRun(t, doc)["model"] = nil },
		"bool threads":      func(doc map[string]any) { studioRun(t, doc)["threads"] = true },
		"bad backend": func(doc map[string]any) {
			studioRun(t, doc)["profile"].(map[string]any)["engine"].(map[string]any)["backend"] = "gpu"
		},
		"bad state": func(doc map[string]any) {
			studioRun(t, doc)["requests"].([]any)[0].(map[string]any)["state"] = "unknown"
		},
		"unknown root field": func(doc map[string]any) { doc["extra"] = 1 },
	}
	for name, mutate := range mutations {
		doc := studioDocument(t, studioReportFixture)
		mutate(doc)
		samples[name] = doc
	}
	for name, doc := range samples {
		t.Run(name, func(t *testing.T) {
			raw := studioBytes(t, doc)
			var generic any
			if err := json.Unmarshal(raw, &generic); err != nil {
				t.Fatal(err)
			}
			schemaErr := schema.Validate(generic)
			_, parseErr := ParseStudioReport(raw)
			if (schemaErr == nil) != (parseErr == nil) {
				t.Fatalf("schema err=%v parser err=%v", schemaErr, parseErr)
			}
		})
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
