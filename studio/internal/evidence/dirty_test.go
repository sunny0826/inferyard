package evidence

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunStatusRealStopReasons(t *testing.T) {
	for _, dirty := range []bool{false, true} {
		if got := RunStatus(true, "incomplete", "user_cancelled", dirty); got != "cancelled" {
			t.Fatalf("user_cancelled dirty=%v: %s", dirty, got)
		}
	}
	for _, reason := range []string{"duration_window_interrupted", "single_wall_budget_exhausted", "trial_wall_budget_exhausted", "temperature_safety_threshold_reached", "diagnostic_run_not_formal", "check_only_no_formal_cases", "tool_interrupted"} {
		t.Run(reason, func(t *testing.T) {
			if got := RunStatus(true, "incomplete", reason, false); got != "partial" {
				t.Fatal(got)
			}
			if got := RunStatus(true, "incomplete", reason, true); got != "dirty" {
				t.Fatal(got)
			}
		})
	}
	for _, tc := range []struct {
		sealed       bool
		completeness string
		dirty        bool
		want         string
	}{
		{false, "complete", false, "partial"}, {true, "incomplete", false, "partial"},
		{true, "", false, "partial"}, {true, "complete", false, "complete"}, {true, "complete", true, "dirty"},
	} {
		if got := RunStatus(tc.sealed, tc.completeness, "", tc.dirty); got != tc.want {
			t.Fatalf("%+v: %s", tc, got)
		}
	}
}

func TestDirtyRunSchemaVersions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host.json")
	for _, tc := range []struct {
		name, raw   string
		dirty, fail bool
	}{
		{"future", `{"schema_version":2}`, false, false},
		{"future_matching", `{"schema_version":2,"dirty":true,"run_id":"r","kind":"run"}`, false, false},
		{"current", `{"schema_version":1,"dirty":true,"run_id":"r","kind":"run"}`, true, false},
		{"zero", `{"schema_version":0}`, false, true},
		{"negative", `{"schema_version":-1}`, false, true},
		{"absent", `{}`, false, true},
		{"invalid_version", `{"schema_version":"2"}`, false, true},
		{"fraction", `{"schema_version":1.5}`, false, true},
		{"invalid_dirty", `{"schema_version":1,"dirty":"true"}`, false, true},
		{"missing_dirty", `{"schema_version":1}`, false, true},
		{"missing_identity", `{"schema_version":1,"dirty":true}`, false, true},
		{"malformed", `{`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(tc.raw), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := DirtyRun(path, "r")
			if (err != nil) != tc.fail || got != tc.dirty {
				t.Fatalf("dirty=%v err=%v", got, err)
			}
		})
	}
}
