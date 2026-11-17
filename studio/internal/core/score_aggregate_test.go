package core

import (
	"encoding/json"
	"testing"

	"inferyard/studio/internal/evidence"
)

// scoreSelectionDocument is the minimal selection document the aggregate reads.
type scoreSelectionDocument struct {
	Documents struct {
		Selection struct {
			Payload struct {
				CaseIDs []string `json:"case_ids"`
			} `json:"payload"`
		} `json:"selection"`
	} `json:"documents"`
}

// scoreRecord is one events.jsonl record with the fields the aggregate reads.
type scoreRecord struct {
	Seq       int64  `json:"seq"`
	Phase     string `json:"phase"`
	EventType string `json:"event_type"`
	Data      struct {
		CaseID         string `json:"case_id"`
		ExecutionState string `json:"execution_state,omitempty"`
		Category       string `json:"category,omitempty"`
		QualityState   string `json:"quality_state,omitempty"`
	} `json:"data"`
}

// The event path feeds the same reduction: an unscorable case keeps its slot in
// the denominator, so one pass plus one unscorable case is 1 / 2 with no rate.
func TestAggregateKeepsUnscorableCaseInDenominator(t *testing.T) {
	inputs := scoreSelectionDocument{}
	inputs.Documents.Selection.Payload.CaseIDs = []string{"case-0", "case-1"}
	raw, err := json.Marshal(inputs)
	if err != nil {
		t.Fatal(err)
	}
	a := newAggregate(raw)
	qualities := []string{"pass", "unscorable"}
	records := make([]evidence.Record, 0, len(qualities)*3)
	seq := 0
	for i, quality := range qualities {
		id := inputs.Documents.Selection.Payload.CaseIDs[i]
		for _, event := range []struct{ kind, state string }{
			{"request_started", ""},
			{"request_finished", "completed"},
			{"score", quality},
		} {
			seq++
			record := scoreRecord{Seq: int64(seq), Phase: "formal", EventType: event.kind}
			record.Data.CaseID = id
			if event.kind == "request_finished" {
				record.Data.ExecutionState = event.state
			}
			if event.kind == "score" {
				record.Data.Category, record.Data.QualityState = "qa", event.state
			}
			line, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			records = append(records, evidence.Record{Seq: int64(seq), Phase: "formal", EventType: event.kind, Raw: line})
		}
	}
	a.events(records)
	score, ok := runScore(a.Progress.Cases)
	if !ok || score.Valid != 2 || score.Passed != 1 || score.Unscorable != 1 {
		t.Fatalf("aggregate score = %+v ok=%v", score, ok)
	}
	if score.Rate != nil {
		t.Fatalf("aggregate produced rate %v for an unscorable case", *score.Rate)
	}
	if got := len(inputs.Documents.Selection.Payload.CaseIDs); got != 2 {
		t.Fatalf("fixture declared %d cases", got)
	}
}
