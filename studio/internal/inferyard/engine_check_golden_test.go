package inferyard

import (
	"encoding/json"
	"os"
	"testing"
)

// Golden wire cases mirror cli/tests/unit/test_engine_check.py and
// cli/src/inferyard/contracts/schemas_engine_check.py. Kept under Studio because
// this task cannot modify cli/. The CLI suite can consume this same JSON file.
func TestEngineCheckGoldenContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/engine_check_contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name     string
		Valid    bool
		ExitCode int `json:"exit_code"`
		Response json.RawMessage
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			_, err := ParseEngineCheck(tc.Response, tc.ExitCode)
			if (err == nil) != tc.Valid {
				t.Fatalf("valid=%v err=%v", tc.Valid, err)
			}
		})
	}
}
