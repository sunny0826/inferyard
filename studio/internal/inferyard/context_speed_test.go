package inferyard

import (
	"encoding/json"
	"os"
	"testing"
)

func TestContextSpeedGolden(t *testing.T) {
	raw, err := os.ReadFile("testdata/context_speed_contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name     string          `json:"name"`
		Valid    bool            `json:"valid"`
		ExitCode int             `json:"exit_code"`
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 28 {
		t.Fatalf("fixture count = %d", len(cases))
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			_, err := ParseContextSpeed(c.Response, c.ExitCode)
			if (err == nil) != c.Valid {
				t.Fatalf("valid=%v error=%v", c.Valid, err)
			}
		})
	}
}

func TestContextSpeedRawRejections(t *testing.T) {
	raw, _ := os.ReadFile("testdata/context_speed_contract.json")
	var cases []struct {
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	good := cases[0].Response
	mutations := map[string][]byte{
		"truncated": good[:len(good)-1],
		"duplicate": append([]byte(`{"command":"speed-results",`), good[1:]...),
		"trailing":  append(append([]byte{}, good...), []byte(` {}`)...),
	}
	for name, replacement := range map[string]string{"unknown_version": "4", "missing_version": "null"} {
		var obj map[string]any
		_ = json.Unmarshal(good, &obj)
		details := obj["details"].(map[string]any)
		if replacement == "4" {
			details["schema_version"] = 4
		} else {
			delete(details, "schema_version")
		}
		mutations[name], _ = json.Marshal(obj)
	}
	for name, raw := range mutations {
		t.Run(name, func(t *testing.T) {
			got, err := ParseContextSpeed(raw, 0)
			if err == nil || got.Samples != nil || got.Summary != nil {
				t.Fatalf("retained invalid rows: %v", err)
			}
		})
	}
	for _, code := range []int{2, 4, 130} {
		if _, err := ParseContextSpeed(good, code); err == nil {
			t.Fatalf("accepted readable complete with exit %d", code)
		}
	}
}

func TestContextSpeedCompleteRequiresFinishedPlan(t *testing.T) {
	raw, err := os.ReadFile("testdata/context_speed_contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Response map[string]any `json:"response"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	response := rows[0].Response
	details := response["details"].(map[string]any)
	details["execution"].(map[string]any)["stop_reason"].(map[string]any)["value"] = "user_cancelled"
	raw, _ = json.Marshal(response)
	if result, err := ParseContextSpeed(raw, 0); err == nil || result.Samples != nil {
		t.Fatal("accepted cancelled plan as complete")
	}
}

func TestContextSpeedSchemaCapability(t *testing.T) {
	raw, err := os.ReadFile("../../../cli/schemas/context_speed_result.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	if !SupportsContextSpeedSchema(raw) {
		t.Fatal("current schema unsupported")
	}
	for _, bad := range []string{`{}`, `{"oneOf":[{},{}]}`, `{"oneOf":[]}`, `{"oneOf":[],"oneOf":[]}`} {
		if SupportsContextSpeedSchema([]byte(bad)) {
			t.Fatal("accepted", bad)
		}
	}
	var schema map[string]any
	_ = json.Unmarshal(raw, &schema)
	branch := schema["oneOf"].([]any)[0].(map[string]any)
	delete(branch["properties"].(map[string]any), "metadata")
	raw, _ = json.Marshal(schema)
	if SupportsContextSpeedSchema(raw) {
		t.Fatal("missing metadata advertised as compatible")
	}
}
