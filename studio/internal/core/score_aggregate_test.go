package core

import (
	"fmt"
	"testing"

	"inferyard/studio/internal/evidence"
)

// scoreInputsJSON declares two cases; the aggregate only needs the selection
// document to seed its progress list.
const scoreInputsJSON = `{"documents":{"selection":{"payload":{"case_ids":["case-0","case-1"]}}}}`

// scoreEvent is one events.jsonl line the aggregate consumes.
type scoreEvent struct {
	seq, caseIndex int
	kind, value    string
}

// scoreLine renders one event exactly as the CLI writer lays it out.
func (e scoreEvent) scoreLine() string {
	base := fmt.Sprintf(`{"seq":%d,"phase":"formal","event_type":%q,"data":{"case_id":"case-%d"`, e.seq, e.kind, e.caseIndex)
	switch e.kind {
	case "request_finished":
		return base + fmt.Sprintf(`,"execution_state":%q}}`, e.value)
	case "score":
		return base + fmt.Sprintf(`,"category":"qa","quality_state":%q}}`, e.value)
	}
	return base + "}}"
}

// The event path feeds the same reduction: an unscorable case keeps its slot in
// the denominator, so one pass plus one unscorable case is 1 / 2 with no rate.
func TestAggregateKeepsUnscorableCaseInDenominator(t *testing.T) {
	a := newAggregate([]byte(scoreInputsJSON))
	a.events(scoreRecords([][2]string{{"completed", "pass"}, {"completed", "unscorable"}}))
	assertAggregateScore(t, a, RunScore{Passed: 1, Valid: 2, Unscorable: 1})
}

// A failed execution still occupies its slot in the denominator end to end.
func TestAggregateKeepsFailedExecutionInDenominator(t *testing.T) {
	a := newAggregate([]byte(scoreInputsJSON))
	a.events(scoreRecords([][2]string{{"completed", "pass"}, {"failed", ""}, {"completed", "pass"}}))
	assertAggregateScore(t, a, RunScore{Passed: 2, Valid: 3, Failed: 1})
}

func assertAggregateScore(t *testing.T, a *aggregate, want RunScore) {
	t.Helper()
	score, ok := runScore(a.Progress.Cases)
	if !ok || score != want {
		t.Fatalf("aggregate score = %+v ok=%v want %+v", score, ok, want)
	}
	if want.Failed == 0 && want.Unscorable == 0 && want.Missing == 0 {
		if score.Rate == nil {
			t.Fatalf("fully judged score has no rate: %+v", score)
		}
	} else if score.Rate != nil {
		t.Fatalf("gapped score still produced rate %v", *score.Rate)
	}
}

// scoreRecords builds the started/finished/score sequence, one triple per case;
// an empty state value emits no score event, and "failed" marks execution.
func scoreRecords(states [][2]string) []evidence.Record {
	records := make([]evidence.Record, 0, len(states)*3)
	seq := 0
	for i, pair := range states {
		state, quality := pair[0], pair[1]
		records = append(records, scoreRecordLine(&seq, i, "request_started", "", ""))
		records = append(records, scoreRecordLine(&seq, i, "request_finished", state, ""))
		if quality != "" {
			records = append(records, scoreRecordLine(&seq, i, "score", "", quality))
		}
	}
	return records
}

func scoreRecordLine(seq *int, caseIndex int, kind, state, quality string) evidence.Record {
	*seq++
	event := scoreEvent{seq: *seq, caseIndex: caseIndex, kind: kind, value: state}
	if kind == "score" {
		event.value = quality
	}
	return evidence.Record{Seq: int64(*seq), Phase: "formal", EventType: kind, Raw: []byte(event.scoreLine())}
}
