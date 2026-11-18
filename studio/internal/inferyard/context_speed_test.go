package inferyard

import (
	"encoding/json"
	"os"
	"testing"
)

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
