package inferyard

import (
	"encoding/json"
	"os"
	"testing"
)

// These wire samples are shared with cli/tests/unit/test_engine_check.py,
// which validates them against the CLI schema. Studio validates its parser.
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
		// Full valid responses are covered with mode/version assertions by TestParseEngineCheckContract.
		if tc.Name == "macos_dual" || tc.Name == "windows_single" {
			continue
		}
		t.Run(tc.Name, func(t *testing.T) {
			_, err := ParseEngineCheck(tc.Response, tc.ExitCode)
			if (err == nil) != tc.Valid {
				t.Fatalf("valid=%v err=%v", tc.Valid, err)
			}
		})
	}
}
