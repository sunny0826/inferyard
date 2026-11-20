package core

import (
	"fmt"
	"testing"

	"inferyard/studio/internal/evidence"
)

const scoreInputsJSON = `{"documents":{
	"selection":{"payload":{"case_ids":["case-0","case-1","case-2"]}},
	"bundle":{"payload":{"cases":[
		{"case_id":"case-0","category":"qa"},
		{"case_id":"case-1","category":"qa"},
		{"case_id":"case-2","category":"svg"}
	]}}}}`

type scoreEvent struct {
	seq, caseIndex int
	kind, value    string
}

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

func TestAggregateExcludesFailedExemptCaseFromDenominator(t *testing.T) {
	a := newAggregate([]byte(scoreInputsJSON))
	a.events(scoreRecords([][2]string{{"completed", "pass"}, {"completed", "pass"}, {"failed", ""}}))
	score, ok := runScore(a.Progress.Cases, caseCategories([]byte(scoreInputsJSON)), true)
	if !ok || score.Passed != 2 || score.Valid != 2 || score.Failed != 0 {
		t.Fatalf("aggregate score = %+v ok=%v", score, ok)
	}
	if score.Rate == nil || *score.Rate != 1 {
		t.Fatalf("rate = %v", score.Rate)
	}
}

func TestAggregateCountsFailedQualityCaseAndKeepsRate(t *testing.T) {
	a := newAggregate([]byte(scoreInputsJSON))
	a.events(scoreRecords([][2]string{{"completed", "pass"}, {"completed", "pass"}, {"failed", ""}}))
	quality := `{"documents":{
		"selection":{"payload":{"case_ids":["case-0","case-1","case-2"]}},
		"bundle":{"payload":{"cases":[
			{"case_id":"case-0","category":"qa"},
			{"case_id":"case-1","category":"qa"},
			{"case_id":"case-2","category":"qa"}
		]}}}}`
	score, ok := runScore(a.Progress.Cases, caseCategories([]byte(quality)), true)
	if !ok || score.Passed != 2 || score.Valid != 3 || score.Failed != 1 {
		t.Fatalf("aggregate score = %+v ok=%v", score, ok)
	}
	if score.Rate == nil || *score.Rate != 2.0/3.0 {
		t.Fatalf("rate = %v, want 2/3", score.Rate)
	}
}

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
