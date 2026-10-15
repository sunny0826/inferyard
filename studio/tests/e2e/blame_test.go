package e2e

import (
	"context"
	"strings"
	"testing"

	"inferyard/studio/internal/inferyard"
)

// TestBlameAttribution pins the boundary rule: a non-zero CLI exit or
// unparsable stdout is CLI-side; a well-formed answer that Studio mishandles
// is Studio-side.
func TestBlameAttribution(t *testing.T) {
	cases := []struct {
		name    string
		out     inferyard.Execution
		verdict string
	}{
		{"cli exit 2 with limitations", inferyard.Execution{
			ExitCode: 2, Stdout: []byte(`{"limitations":["frozen_environment_mismatch"]}`),
		}, BlameCLI},
		{"cli killed by signal", inferyard.Execution{
			ExitCode: -1, Stderr: []byte("signal: killed"),
		}, BlameCLI},
		{"exit 0 but stdout not json", inferyard.Execution{
			ExitCode: 0, Stdout: []byte("panic: boom"),
		}, BlameCLI},
		{"exit 0 and valid json", inferyard.Execution{
			ExitCode: 0, Stdout: []byte(`{"kind":"progress"}`),
		}, BlameStudio},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := NewRecorder(t, t.TempDir(), func(context.Context, string, ...string) (inferyard.Execution, error) {
				return tc.out, nil
			})
			if _, err := recorder.Exec(context.Background(), "inferyard", "progress"); err != nil {
				t.Fatal(err)
			}
			report := analyzeFailure(recorder)
			if report.Verdict != tc.verdict {
				t.Fatalf("verdict %s, want %s (%s)", report.Verdict, tc.verdict, report)
			}
		})
	}
	t.Run("no call recorded", func(t *testing.T) {
		recorder := NewRecorder(t, t.TempDir(), nil)
		if report := analyzeFailure(recorder); report.Verdict != BlameNone {
			t.Fatalf("verdict %s, want %s", report.Verdict, BlameNone)
		}
	})
	t.Run("limitations surface in report", func(t *testing.T) {
		recorder := NewRecorder(t, t.TempDir(), func(context.Context, string, ...string) (inferyard.Execution, error) {
			return inferyard.Execution{ExitCode: 3, Stdout: []byte(`{"limitations":["run_incomplete"]}`)}, nil
		})
		_, _ = recorder.Exec(context.Background(), "inferyard", "run")
		report := analyzeFailure(recorder)
		if !strings.Contains(report.String(), "run_incomplete") {
			t.Fatalf("limitations missing from report: %s", report)
		}
	})
}
