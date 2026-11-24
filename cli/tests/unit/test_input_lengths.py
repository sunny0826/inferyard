from inferyard.analysis.input_lengths import input_length_summary

RUN = {
    "origin": "measured",
    "run_id": "r1",
    "trial_id": "t1",
    "definition_versions": {
        "measurement": "phase2.v1",
        "scoring": "phase2.v1",
        "comparison": "phase2.v1",
    },
}
EVIDENCE = [{"path": "events.jsonl", "sha256": "a" * 64}]


def workload(kind="fixed"):
    return {
        "workload_id": "w",
        "protocol": {"kind": kind},
        "input_target_tokens": 1024,
        "output_budget_tokens": 512,
    }


def test_missing_counts_never_produce_measured_length_cohort():
    rows = [
        {
            "case_id": f"c{i}",
            "category": "qa",
            "request_id": f"request-{i}",
            "execution_state": "completed" if i < 3 else "failed",
            "error_category": None if i < 3 else "total_timeout",
            "budget_exhausted": i == 2,
            "score": {"passed": i != 1},
            "t_send_ns": 0,
            "t_first_content_ns": 200_000_000,
            "t_first_answer_ns": 300_000_000,
            "t_terminal_ns": (i + 1) * 1_000_000_000,
            "completion_tokens": 12,
            "token_source": "endpoint.usage",
            "token_scope": "completion_tokens",
        }
        for i in range(4)
    ]
    summary, observations = input_length_summary(RUN, workload(), rows, {}, EVIDENCE, complete=True)
    assert summary["bins"][0]["actual_input_tokens"] is None
    assert all(m["value"] is None for m in observations)
