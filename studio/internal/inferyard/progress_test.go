package inferyard

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fullProgress = `{
  "kind": "progress",
  "progress_version": 1,
  "workspace": "/work/ws",
  "device_preflight": {"status": "ok", "path": "/work/ws/preflight/device-preflight.json"},
  "bundles": [
    {
      "bundle": "zh-core",
      "phase": "bound_missing",
      "candidate_path": "/work/ws/candidates/zh-core-1",
      "bound_path": null,
      "runs": [{"path": "/work/ws/results/run-a", "complete": true, "sealed": true}],
      "next_action": "bind",
      "next_command": "inferyard config bind --candidate /work/ws/candidates/zh-core-1/candidate.toml --pid 123 --endpoint http://127.0.0.1:48857 --out /work/ws/bound/zh-core-1",
      "blocked_reason": null
    },
    {
      "bundle": "zh-svg-pelican",
      "phase": "preflight_missing",
      "candidate_path": null,
      "bound_path": null,
      "runs": [],
      "next_action": "device_check",
      "next_command": null,
      "blocked_reason": null
    }
  ],
  "report": {"status": "missing", "path": null, "runs": []},
  "next_action": "bind",
  "next_command": "inferyard config bind --candidate /work/ws/candidates/zh-core-1/candidate.toml",
  "default_scope_complete": false
}`

func TestParseProgressFullFixture(t *testing.T) {
	p, err := ParseProgress([]byte(fullProgress))
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != "progress" || p.ProgressVersion != 1 || p.Workspace != "/work/ws" {
		t.Fatal(p)
	}
	if p.DevicePreflight.Status != "ok" || *p.DevicePreflight.Path != "/work/ws/preflight/device-preflight.json" {
		t.Fatal(p.DevicePreflight)
	}
	if len(p.Bundles) != 2 {
		t.Fatal(p.Bundles)
	}
	core := p.Bundles[0]
	if core.Bundle != "zh-core" || core.Phase != "bound_missing" || core.NextAction != "bind" {
		t.Fatal(core)
	}
	if core.CandidatePath == nil || *core.CandidatePath != "/work/ws/candidates/zh-core-1" || core.BoundPath != nil {
		t.Fatal(core)
	}
	if len(core.Runs) != 1 || core.Runs[0].Path != "/work/ws/results/run-a" || !core.Runs[0].Complete || !core.Runs[0].Sealed {
		t.Fatal(core.Runs)
	}
	if core.NextCommand == nil || *core.NextCommand == "" || core.BlockedReason != nil {
		t.Fatal(core)
	}
	pelican := p.Bundles[1]
	if pelican.Phase != "preflight_missing" || pelican.NextAction != "device_check" || pelican.NextCommand != nil {
		t.Fatal(pelican)
	}
	if p.Report.Status != "missing" || p.Report.Path != nil || len(p.Report.Runs) != 0 {
		t.Fatal(p.Report)
	}
	if p.NextAction != "bind" || p.NextCommand == nil || p.DefaultScopeComplete {
		t.Fatal(p)
	}
}

func TestParseProgressVerifiedFixture(t *testing.T) {
	p, err := ParseProgress([]byte(`{
  "kind": "progress", "progress_version": 1, "workspace": "/w",
  "device_preflight": {"status": "ok", "path": "/w/p"},
  "bundles": [
    {"bundle": "zh-core", "phase": "run_complete", "candidate_path": "/w/c", "bound_path": "/w/b",
     "runs": [{"path": "/w/r1", "complete": true, "sealed": true}], "next_action": "none", "next_command": null, "blocked_reason": null},
    {"bundle": "zh-svg-pelican", "phase": "run_complete", "candidate_path": "/w/c2", "bound_path": "/w/b2",
     "runs": [{"path": "/w/r2", "complete": true, "sealed": true}], "next_action": "none", "next_command": null, "blocked_reason": null}
  ],
  "report": {"status": "verified", "path": "/w/reports/summary-1", "runs": ["/w/r1", "/w/r2"]},
  "next_action": "done", "next_command": null, "default_scope_complete": true}`))
	if err != nil {
		t.Fatal(err)
	}
	if !p.DefaultScopeComplete || p.Report.Status != "verified" || *p.Report.Path != "/w/reports/summary-1" {
		t.Fatal(p)
	}
	if len(p.Report.Runs) != 2 {
		t.Fatal(p.Report.Runs)
	}
}

func TestParseProgressRejections(t *testing.T) {
	valid := `{"kind":"progress","progress_version":1,"workspace":"/w","device_preflight":{"status":"missing","path":null},
	"bundles":[{"bundle":"zh-core","phase":"candidate_missing","candidate_path":null,"bound_path":null,"runs":[],"next_action":"prepare_candidate","next_command":null,"blocked_reason":null}],
	"report":{"status":"missing","path":null,"runs":[]},"next_action":"prepare_candidate","next_command":null,"default_scope_complete":false}`
	if _, err := ParseProgress([]byte(valid)); err != nil {
		t.Fatal(err)
	}
	// Unknown CLI-added fields stay readable.
	extended := strings.Replace(fullProgress, `"kind": "progress",`, `"kind": "progress", "future_field": 42,`, 1)
	if _, err := ParseProgress([]byte(extended)); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		input string
		want  string
	}{
		"not json":              {input: ``, want: "one JSON object"},
		"array":                 {input: `[]`, want: "one JSON object"},
		"wrong kind":            {input: `{"kind":"device_preflight.v1","progress_version":1,"workspace":"/w","bundles":[]}`, want: "unsupported progress kind"},
		"wrong version":         {input: `{"kind":"progress","progress_version":2,"workspace":"/w","bundles":[]}`, want: "unsupported progress version"},
		"empty workspace":       {input: `{"kind":"progress","progress_version":1,"workspace":"","bundles":[]}`, want: "workspace is empty"},
		"null bundles":          {input: `{"kind":"progress","progress_version":1,"workspace":"/w","bundles":null}`, want: "bundles must be an array"},
		"missing bundle id":     {input: `{"kind":"progress","progress_version":1,"workspace":"/w","bundles":[{"bundle":"","phase":"run_missing","next_action":"run"}]}`, want: "missing required fields"},
		"missing phase":         {input: `{"kind":"progress","progress_version":1,"workspace":"/w","bundles":[{"bundle":"zh-core","phase":"","next_action":"run"}]}`, want: "missing required fields"},
		"missing action":        {input: `{"kind":"progress","progress_version":1,"workspace":"/w","bundles":[{"bundle":"zh-core","phase":"run_missing","next_action":""}]}`, want: "missing required fields"},
		"commandResult wrapper": {input: `{"command":"progress","status":"complete","completeness":"complete","limitations":[]}`, want: "unsupported progress kind"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseProgress([]byte(c.input))
			if err == nil || !stringContains(err.Error(), c.want) {
				t.Fatalf("want %q, got %v", c.want, err)
			}
		})
	}
}

func stringContains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// Client.Progress runs the read-only command with the workspace flag; exit 2
// stays data (Blocked) and exit 0 parses progress.v1 from stdout.
func TestClientProgress(t *testing.T) {
	fixture := filepath.Join(t.TempDir(), "progress.json")
	if err := os.WriteFile(fixture, []byte(fullProgress), 0600); err != nil {
		t.Fatal(err)
	}
	c := Client{Path: script(t, fmt.Sprintf(`if [ "$3" = "/work/ws" ]; then cat %q; exit 0; fi
echo 'workspace unusable' >&2
exit 2`, fixture))}
	p, out, err := c.Progress(context.Background(), "/work/ws")
	if err != nil {
		t.Fatal(err)
	}
	if out.ExitCode != 0 || out.Status != Complete || p.Workspace != "/work/ws" || p.Bundles[0].Bundle != "zh-core" {
		t.Fatalf("%+v %+v", out, p)
	}
	p, out, err = c.Progress(context.Background(), "/other")
	if err != nil || out.ExitCode != 2 || out.Status != Blocked || p.Workspace != "" {
		t.Fatalf("%+v %+v %v", out, p, err)
	}
}
