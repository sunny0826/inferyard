package inferyard

import (
	_ "embed"
	"encoding/json"
	"strings"
	"testing"
)

//go:embed testdata/progress.v2.json
var contractProgressFixture string

//go:embed testdata/progress.v1.json
var legacyProgressFixture string

func TestProgressContractBoundaries(t *testing.T) {
	var fixture map[string]any
	if err := json.Unmarshal([]byte(contractProgressFixture), &fixture); err != nil {
		t.Fatal(err)
	}
	fixture["future_field"] = "compatible"
	raw, _ := json.Marshal(fixture)
	if _, err := ParseProgress(raw); err != nil {
		t.Fatal("unknown field rejected", err)
	}
	for _, field := range []string{"device_preflight", "report", "next_action", "next_command", "default_scope_complete"} {
		clone := map[string]json.RawMessage{}
		json.Unmarshal(raw, &clone)
		delete(clone, field)
		bad, _ := json.Marshal(clone)
		if _, err := ParseProgress(bad); err == nil {
			t.Fatal("missing field accepted", field)
		}
	}
	for _, bad := range []string{
		strings.Replace(contractProgressFixture, `"progress_version": 2`, `"progress_version": true`, 1),
		strings.Replace(contractProgressFixture, `"default_scope_complete": false`, `"default_scope_complete": null`, 1),
		strings.Replace(contractProgressFixture, `"complete": true`, `"complete": null`, 1),
		strings.Replace(contractProgressFixture, `"kind": "progress"`, `"kind": "progress", "kind": "progress"`, 1),
		strings.Replace(contractProgressFixture, `"phase": "bound_missing"`, `"phase": "unexpected"`, 1),
		strings.Replace(contractProgressFixture, `"report": {"status": "missing"`, `"report": {"status": "verified"`, 1),
		contractProgressFixture + `{}`,
	} {
		if _, err := ParseProgress([]byte(bad)); err == nil {
			t.Fatal("invalid structure accepted", bad)
		}
	}
	verifyAction := `{"kind":"progress","progress_version":2,"workspace":"/w","device_preflight":{"status":"ok","path":null},"bundles":[],"report":{"status":"missing","path":null,"runs":[]},"next_action":"verify","next_command":null,"default_scope_complete":false}`
	if _, err := ParseProgress([]byte(verifyAction)); err == nil {
		t.Fatal("removed verify action accepted")
	}
}

func TestProgressRejectsVersionOne(t *testing.T) {
	_, err := ParseProgress([]byte(legacyProgressFixture))
	if err == nil || !strings.Contains(err.Error(), "unsupported progress version") {
		t.Fatalf("legacy progress accepted: %v", err)
	}
}

func TestProgressDoneBindsPresentReport(t *testing.T) {
	done := `{"kind":"progress","progress_version":2,"workspace":"/w","device_preflight":{"status":"ok","path":null},
	"bundles":[{"bundle":"zh-core","phase":"run_complete","candidate_path":null,"bound_path":null,"runs":[],"next_action":"none","next_command":null,"blocked_reason":null}],
	"report":{"status":"present","path":"/w/reports/summary","runs":["/w/results/run"]},"next_action":"done","next_command":null,"default_scope_complete":true}`
	if _, err := ParseProgress([]byte(done)); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"done without present report": strings.Replace(done, `"status":"present"`, `"status":"missing"`, 1),
		"done with follow-up command": strings.ReplaceAll(done, `"next_command":null`, `"next_command":"inferyard verify --path /w/reports/summary"`),
		"done without report path":    strings.Replace(done, `"path":"/w/reports/summary"`, `"path":null`, 1),
		"done outside default scope":  strings.Replace(done, `"default_scope_complete":true`, `"default_scope_complete":false`, 1),
		"default scope skipped ahead": strings.Replace(done, `"next_action":"done"`, `"next_action":"bind"`, 1),
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseProgress([]byte(input)); err == nil {
				t.Fatal("inconsistent progress accepted")
			}
		})
	}
	missing := strings.Replace(done, `"status":"present"`, `"status":"missing"`, 1)
	missing = strings.Replace(missing, `"path":"/w/reports/summary"`, `"path":null`, 1)
	missing = strings.Replace(missing, `"runs":["/w/results/run"]`, `"runs":[]`, 1)
	missing = strings.Replace(missing, `"next_action":"done"`, `"next_action":"report"`, 1)
	missing = strings.ReplaceAll(missing, `"next_command":null`, `"next_command":"inferyard report --runs /w/results/run --out /w/reports/summary"`)
	missing = strings.Replace(missing, `"default_scope_complete":true`, `"default_scope_complete":false`, 1)
	if _, err := ParseProgress([]byte(missing)); err != nil {
		t.Fatal(err)
	}
}
