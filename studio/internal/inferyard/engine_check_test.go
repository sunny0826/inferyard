package inferyard

import (
	"strings"
	"testing"
)

const engineCheckWire = `{"command":"engine check","status":"checked","completeness":"complete","run_id":null,"evidence_dir":null,"limitations":[],"details":{"eligible":true,"reason":null,"reported_version":"prism-b10743-adfffbe","expected_release":"prism-b10743-adfffbe","modes":{"cpu":true,"metal":true},"mode":null}}`

func replaceWire(raw string, pairs ...string) string {
	return strings.NewReplacer(pairs...).Replace(raw)
}

func TestParseEngineCheckContract(t *testing.T) {
	r, err := ParseEngineCheck([]byte(engineCheckWire), 0)
	if err != nil || !r.Eligible || r.Modes == nil || !r.Modes.CPU || !r.Modes.Metal || r.Mode != nil ||
		r.ReportedVersion == nil || *r.ReportedVersion != "prism-b10743-adfffbe" || r.ExpectedRelease != "prism-b10743-adfffbe" {
		t.Fatalf("%+v %v", r, err)
	}
	// Windows shape: receipt profile mode, modes null.
	windows := replaceWire(engineCheckWire, `"modes":{"cpu":true,"metal":true},"mode":null`, `"modes":null,"mode":"cuda"`)
	r, err = ParseEngineCheck([]byte(windows), 0)
	if err != nil || r.Modes != nil || r.Mode == nil || *r.Mode != "cuda" {
		t.Fatalf("%+v %v", r, err)
	}
	// Ineligible is a successful verification with a fixed reason.
	ineligible := replaceWire(engineCheckWire,
		`"eligible":true`, `"eligible":false`,
		`"reason":null`, `"reason":"unsupported_engine_build_requires_adapter_validation"`,
		`"reported_version":"prism-b10743-adfffbe"`, `"reported_version":null`,
		`"modes":{"cpu":true,"metal":true}`, `"modes":null`)
	r, err = ParseEngineCheck([]byte(ineligible), 0)
	if err != nil || r.Eligible || r.Reason == nil || *r.Reason != "unsupported_engine_build_requires_adapter_validation" || r.ExpectedRelease == "" {
		t.Fatalf("%+v %v", r, err)
	}
	for name, raw := range map[string]string{
		"eligible as string":   replaceWire(engineCheckWire, `"eligible":true`, `"eligible":"yes"`),
		"duplicate key":        replaceWire(engineCheckWire, `"eligible":true`, `"eligible":true,"eligible":true`),
		"empty version":        replaceWire(engineCheckWire, `"reported_version":"prism-b10743-adfffbe"`, `"reported_version":""`),
		"empty release":        replaceWire(engineCheckWire, `"expected_release":"prism-b10743-adfffbe"`, `"expected_release":""`),
		"free-form reason":     replaceWire(engineCheckWire, `"reason":null`, `"reason":"Free Text"`),
		"eligible with reason": replaceWire(engineCheckWire, `"reason":null`, `"reason":"invalid_library_manifest"`),
		"both mode shapes":     replaceWire(engineCheckWire, `"mode":null`, `"mode":"cpu"`),
		"neither mode shape":   replaceWire(engineCheckWire, `"modes":{"cpu":true,"metal":true}`, `"modes":null`),
		"macOS mode value":     replaceWire(engineCheckWire, `"modes":{"cpu":true,"metal":true},"mode":null`, `"modes":null,"mode":"metal"`),
		"unknown command":      replaceWire(engineCheckWire, `"command":"engine check"`, `"command":"unknown"`),
		"wrong status":         replaceWire(engineCheckWire, `"status":"checked"`, `"status":"complete"`),
		"extra detail":         replaceWire(engineCheckWire, `"mode":null`, `"mode":null,"extra":1`),
		"trailing JSON":        engineCheckWire + `{}`,
		"ineligible missing reason": replaceWire(engineCheckWire,
			`"eligible":true`, `"eligible":false`,
			`"reported_version":"prism-b10743-adfffbe"`, `"reported_version":null`,
			`"modes":{"cpu":true,"metal":true}`, `"modes":null`),
	} {
		if _, err := ParseEngineCheck([]byte(raw), 0); err == nil {
			t.Fatalf("%s accepted: %s", name, raw)
		}
	}
	if _, err := ParseEngineCheck([]byte(engineCheckWire), 3); err == nil {
		t.Fatal("unexpected exit code accepted")
	}
}

func TestParseEngineCheckFailureShapes(t *testing.T) {
	blocked := `{"command":"engine check","status":"blocked","completeness":"incomplete","run_id":null,"evidence_dir":null,"limitations":["invalid_input"],"details":null}`
	toolError := `{"command":"engine check","status":"error","completeness":"incomplete","run_id":null,"evidence_dir":null,"limitations":["engine_version_query_failed"],"details":null}`
	cancelled := `{"command":"engine check","status":"interrupted","completeness":"incomplete","run_id":null,"evidence_dir":null,"limitations":["cancelled"],"details":null}`
	r, err := ParseEngineCheck([]byte(blocked), 2)
	if err != nil || r.Status != "blocked" || len(r.Limitations) != 1 || r.Limitations[0] != "invalid_input" {
		t.Fatalf("%+v %v", r, err)
	}
	r, err = ParseEngineCheck([]byte(toolError), 4)
	if err != nil || r.Status != "error" || r.Limitations[0] != "engine_version_query_failed" {
		t.Fatalf("%+v %v", r, err)
	}
	r, err = ParseEngineCheck([]byte(cancelled), 130)
	if err != nil || r.Status != "interrupted" {
		t.Fatalf("%+v %v", r, err)
	}
	for name, tc := range map[string]struct {
		raw  string
		exit int
	}{
		"blocked with details":   {replaceWire(blocked, `"details":null`, `"details":{}`), 2},
		"blocked wrong status":   {replaceWire(blocked, `"status":"blocked"`, `"status":"error"`), 2},
		"blocked unknown reason": {replaceWire(blocked, `"invalid_input"`, `"not_a_limitation"`), 2},
		"cancelled extra code":   {replaceWire(cancelled, `["cancelled"]`, `["cancelled","io_error"]`), 130},
		"cancelled wrong code":   {replaceWire(cancelled, `"cancelled"`, `"io_error"`), 130},
		"error empty limits":     {replaceWire(toolError, `["engine_version_query_failed"]`, `[]`), 4},
		"checked exit 2":         {engineCheckWire, 2},
	} {
		if _, err := ParseEngineCheck([]byte(tc.raw), tc.exit); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestParseLocalEngineCheckContract(t *testing.T) {
	wire := `{"command":"engine check","status":"checked","completeness":"complete","run_id":null,"evidence_dir":null,"limitations":[],"details":{"definition":"local-engine-check.v2","eligible":true,"reason":null,"family":"mlx-lm","declaration_sha256":"` + strings.Repeat("a", 64) + `","verification_scope":"installation_runtime_closure_only","model_requests_sent":0,"ready_to_run":false}}`
	for _, family := range []string{"llama-cpp", "mlx-lm", "vllm", "sglang", "lmstudio"} {
		r, err := ParseEngineCheck([]byte(strings.Replace(wire, "mlx-lm", family, 1)), 0)
		if err != nil || !r.Eligible || r.Family == nil || *r.Family != family || r.DeclarationSHA256 == nil || *r.DeclarationSHA256 != strings.Repeat("a", 64) {
			t.Fatalf("%+v %v", r, err)
		}
	}
	ineligible := replaceWire(wire, `"eligible":true`, `"eligible":false`, `"reason":null`, `"reason":"local_engine_runtime_root_invalid"`, `"declaration_sha256":"`+strings.Repeat("a", 64)+`"`, `"declaration_sha256":null`)
	for _, raw := range []string{ineligible, strings.Replace(ineligible, `"family":"mlx-lm"`, `"family":null`, 1)} {
		r, err := ParseEngineCheck([]byte(raw), 0)
		if err != nil || r.Eligible || r.DeclarationSHA256 != nil {
			t.Fatalf("%+v %v", r, err)
		}
	}
	for _, pair := range [][2]string{
		{`"definition":"local-engine-check.v2"`, `"definition":"future"`},
		{`"definition":"local-engine-check.v2"`, `"definition":"local-engine-check.v1"`},
		{`"eligible":true`, `"eligible":null`},
		{`"eligible":true`, `"eligible":1`},
		{`"eligible":true`, `"eligible":true,"eligible":true`},
		{`"family":"mlx-lm"`, `"family":null`},
		{`"family":"mlx-lm"`, `"family":"other"`},
		{`"ready_to_run":false`, `"ready_to_run":true`},
		{`"ready_to_run":false`, `"ready_to_run":null`},
		{`"model_requests_sent":0`, `"model_requests_sent":false`},
		{`"model_requests_sent":0`, `"model_requests_sent":1`},
		{`"model_requests_sent":0`, `"model_requests_sent":null`},
		{`"verification_scope":"installation_runtime_closure_only"`, `"verification_scope":"run_eligible"`},
		{`"declaration_sha256":"` + strings.Repeat("a", 64) + `"`, `"declaration_sha256":null`},
		{`"declaration_sha256":"` + strings.Repeat("a", 64) + `"`, `"declaration_sha256":"bad"`},
	} {
		if _, err := ParseEngineCheck([]byte(strings.Replace(wire, pair[0], pair[1], 1)), 0); err == nil {
			t.Fatalf("accepted %v", pair)
		}
	}
	if _, err := ParseEngineCheck([]byte(strings.Replace(ineligible, `"declaration_sha256":null`, `"declaration_sha256":"`+strings.Repeat("a", 64)+`"`, 1)), 0); err == nil {
		t.Fatal("ineligible retained verified digest")
	}
}
