package core

import (
	"encoding/json"
	"testing"

	"inferyard/studio/internal/evidence"
)

func TestAggregatePopulatesCasePromptAndResponse(t *testing.T) {
	inputs, err := json.Marshal(map[string]any{
		"documents": map[string]any{
			"selection": map[string]any{
				"payload": map[string]any{
					"case_ids": []string{"case-answered", "case-pending"},
				},
			},
			"bundle": map[string]any{
				"payload": map[string]any{
					"cases": []map[string]any{
						{"case_id": "case-answered", "prompt": "What is 2 + 2?"},
						{"case_id": "case-pending", "prompt": "Explain KV cache briefly."},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	a := newAggregate(inputs)
	if len(a.Progress.Cases) != 2 {
		t.Fatalf("got %d cases, want 2", len(a.Progress.Cases))
	}
	if str(a.Progress.Cases[0].Prompt) != "What is 2 + 2?" || a.Progress.Cases[0].Response != nil {
		t.Fatalf("unexpected initial answered case: %+v", a.Progress.Cases[0])
	}
	if str(a.Progress.Cases[1].Prompt) != "Explain KV cache briefly." || a.Progress.Cases[1].Response != nil {
		t.Fatalf("unexpected initial pending case: %+v", a.Progress.Cases[1])
	}

	makeRec := func(seq int64, eventType, reqID string, data map[string]any) evidence.Record {
		raw, _ := json.Marshal(map[string]any{
			"seq":        seq,
			"utc":        "2026-10-17T00:00:00Z",
			"phase":      "formal",
			"request_id": reqID,
			"event_type": eventType,
			"data":       data,
		})
		return evidence.Record{Seq: seq, Phase: "formal", EventType: eventType, Raw: raw}
	}

	a.events([]evidence.Record{
		makeRec(1, "request_started", "req-1", map[string]any{"case_id": "case-answered", "plan_index": 0}),
		makeRec(2, "content", "req-1", map[string]any{"text": "4"}),
	})
	if str(a.Progress.Cases[0].Response) != "4" {
		t.Fatalf("streaming content not captured: %+v", a.Progress.Cases[0])
	}

	a.events([]evidence.Record{
		makeRec(3, "request_finished", "req-1", map[string]any{
			"execution_state": "completed",
			"content":         "The answer is 4.",
			"t_send_ns":       1000000,
			"t_terminal_ns":   2000000,
		}),
		makeRec(4, "request_started", "req-2", map[string]any{"case_id": "case-pending", "plan_index": 1}),
		makeRec(5, "request_finished", "req-2", map[string]any{
			"execution_state": "failed",
			"error_category":  "timeout",
			"content":         "",
			"t_send_ns":       3000000,
			"t_terminal_ns":   4000000,
		}),
	})
	if str(a.Progress.Cases[0].Response) != "The answer is 4." {
		t.Fatalf("terminal response mismatch: %+v", a.Progress.Cases[0])
	}
	if a.Progress.Cases[1].Response != nil {
		t.Fatalf("empty terminal content should remain nil, got %q", *a.Progress.Cases[1].Response)
	}
	if str(a.Progress.Cases[1].Prompt) != "Explain KV cache briefly." {
		t.Fatalf("prompt should stay intact on failure: %+v", a.Progress.Cases[1])
	}
}
