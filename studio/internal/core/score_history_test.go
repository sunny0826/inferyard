package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFinishedRun(t *testing.T, root, reason string) {
	t.Helper()
	dir := filepath.Join(root, reason)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	inputs := `{"schema_version":3,"evidence_layout_version":2,"run_id":"` + reason + `","documents":{
		"run":{"payload":{"run_id":"` + reason + `","kind":"run","diagnostic":false,"relation":"initial"}},
		"selection":{"payload":{"case_ids":["case-0","case-1"]}},
		"bundle":{"payload":{"cases":[{"case_id":"case-0","category":"qa"},{"case_id":"case-1","category":"qa"}]}}}}`
	manifest := `{"schema_version":3,"evidence_layout_version":2,"run_id":"` + reason + `","sealed":true}`
	var events strings.Builder
	seq := 0
	line := func(kind, request, data string) {
		seq++
		fmt.Fprintf(&events, `{"seq":%d,"phase":"formal","utc":"2026-11-17T00:00:%02dZ","request_id":%s,"event_type":%q,"data":{%s}}`+"\n", seq, seq, request, kind, data)
	}
	for i := range 2 {
		request := fmt.Sprintf(`"req-%d"`, i)
		line("request_started", request, fmt.Sprintf(`"case_id":"case-%d"`, i))
		line("request_finished", request, `"execution_state":"completed"`)
		line("score", request, `"category":"qa","quality_state":"pass"`)
	}
	line("run_stopped", "null", fmt.Sprintf(`"reason":%q`, reason))
	for name, body := range map[string]string{"inputs.json": inputs, "manifest.json": manifest, "events.jsonl": events.String()} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHistoryTreatsAbnormalStopAfterLastCaseAsIncomplete(t *testing.T) {
	s := testService(t, nil)
	root, err := canonical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.settings.ResultsRoot = &root
	want := map[string]historyVerdict{
		"plan_finished":          {"complete", "complete"},
		"adapter_cleanup_failed": {"incomplete", "partial"},
		"sampler_failed":         {"incomplete", "partial"},
		"user_cancelled":         {"incomplete", "cancelled"},
	}
	for reason := range want {
		writeFinishedRun(t, root, reason)
	}
	history, apiErr := s.History()
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if len(history.StandaloneRuns) != len(want) {
		t.Fatalf("history runs = %d, want %d", len(history.StandaloneRuns), len(want))
	}
	for _, run := range history.StandaloneRuns {
		want[str(run.StopReason)].check(t, run)
	}
}

type historyVerdict struct{ completeness, status string }

func (v historyVerdict) check(t *testing.T, run RunSummary) {
	t.Helper()
	reason := str(run.StopReason)
	if str(run.Completeness) != v.completeness || run.Status != v.status {
		t.Errorf("%s: completeness=%s status=%s, want %s/%s", reason, str(run.Completeness), run.Status, v.completeness, v.status)
	}
	if run.Score == nil || run.Score.Passed != 2 || run.Score.Valid != 2 {
		t.Errorf("%s: score = %+v, want 2 of 2 counted", reason, run.Score)
		return
	}
	if hasRate := run.Score.Rate != nil; hasRate != (v.completeness == "complete") {
		t.Errorf("%s: rate = %v, want rate only for a complete run", reason, run.Score.Rate)
	}
}
