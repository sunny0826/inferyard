package inferyard

import "testing"

const fullProgress = `{
  "kind": "progress",
  "progress_version": 2,
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

func TestParseProgressRejections(t *testing.T) {
	valid := `{"kind":"progress","progress_version":2,"workspace":"/w","device_preflight":{"status":"missing","path":null},
	"bundles":[{"bundle":"zh-core","phase":"candidate_missing","candidate_path":null,"bound_path":null,"runs":[],"next_action":"prepare_candidate","next_command":null,"blocked_reason":null}],
	"report":{"status":"missing","path":null,"runs":[]},"next_action":"prepare_candidate","next_command":null,"default_scope_complete":false}`
	if _, err := ParseProgress([]byte(valid)); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		input string
		want  string
	}{
		"not json":              {input: ``, want: "one JSON object"},
		"array":                 {input: `[]`, want: "one JSON object"},
		"wrong kind":            {input: `{"kind":"device_preflight.v1","progress_version":2,"workspace":"/w","bundles":[]}`, want: "unsupported progress kind"},
		"wrong version":         {input: `{"kind":"progress","progress_version":1,"workspace":"/w","bundles":[]}`, want: "unsupported progress version"},
		"empty workspace":       {input: `{"kind":"progress","progress_version":2,"workspace":"","bundles":[]}`, want: "workspace is empty"},
		"null bundles":          {input: `{"kind":"progress","progress_version":2,"workspace":"/w","bundles":null}`, want: "bundles must be an array"},
		"missing bundle id":     {input: `{"kind":"progress","progress_version":2,"workspace":"/w","bundles":[{"bundle":"","phase":"run_missing","next_action":"run"}]}`, want: "missing required fields"},
		"missing phase":         {input: `{"kind":"progress","progress_version":2,"workspace":"/w","bundles":[{"bundle":"zh-core","phase":"","next_action":"run"}]}`, want: "missing required fields"},
		"missing action":        {input: `{"kind":"progress","progress_version":2,"workspace":"/w","bundles":[{"bundle":"zh-core","phase":"run_missing","next_action":""}]}`, want: "missing required fields"},
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
