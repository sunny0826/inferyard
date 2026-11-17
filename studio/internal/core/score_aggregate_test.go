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
	a.events(scoreRecords([]string{"pass", "unscorable"}))
	score, ok := runScore(a.Progress.Cases)
	if !ok || score.Valid != 2 || score.Passed != 1 || score.Unscorable != 1 {
		t.Fatalf("aggregate score = %+v ok=%v", score, ok)
	}
	if score.Rate != nil {
		t.Fatalf("aggregate produced rate %v for an unscorable case", *score.Rate)
	}
}

// scoreRecords builds the started/finished/score sequence, one triple per case.
func scoreRecords(qualities []string) []evidence.Record {
	records := make([]evidence.Record, 0, len(qualities)*3)
	seq := 0
	for i, quality := range qualities {
		for _, event := range []scoreEvent{
			{caseIndex: i, kind: "request_started"},
			{caseIndex: i, kind: "request_finished", value: "completed"},
			{caseIndex: i, kind: "score", value: quality},
		} {
			seq++
			event.seq = seq
			records = append(records, evidence.Record{
				Seq: int64(seq), Phase: "formal", EventType: event.kind, Raw: []byte(event.scoreLine()),
			})
		}
	}
	return records
}
