package inferyard

import (
	_ "embed"
	"encoding/json"
	"strings"
	"testing"
)

//go:embed testdata/progress.v1.json
var contractProgressFixture string

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
		strings.Replace(contractProgressFixture, `"progress_version": 1`, `"progress_version": true`, 1),
		strings.Replace(contractProgressFixture, `"default_scope_complete": false`, `"default_scope_complete": null`, 1),
		strings.Replace(contractProgressFixture, `"complete": true`, `"complete": null`, 1),
		strings.Replace(contractProgressFixture, `"kind": "progress"`, `"kind": "progress", "kind": "progress"`, 1),
		strings.Replace(contractProgressFixture, `"phase": "bound_missing"`, `"phase": "unexpected"`, 1),
		contractProgressFixture + `{}`,
	} {
		if _, err := ParseProgress([]byte(bad)); err == nil {
			t.Fatal("invalid structure accepted", bad)
		}
	}
}
func TestProgressAllPhases(t *testing.T) {
	for phase, action := range map[string]string{"preflight_missing": "device_check", "candidate_missing": "prepare_candidate", "bound_missing": "bind", "run_missing": "run", "run_incomplete": "resume", "run_complete": "none"} {
		raw := strings.Replace(contractProgressFixture, `"phase": "bound_missing"`, `"phase": "`+phase+`"`, 1)
		raw = strings.Replace(raw, `"next_action": "bind"`, `"next_action": "`+action+`"`, 1)
		p, err := ParseProgress([]byte(raw))
		if err != nil || p.Bundles[0].Phase != phase || p.Bundles[0].NextAction != action {
			t.Fatalf("%s %v", phase, err)
		}
	}
}
