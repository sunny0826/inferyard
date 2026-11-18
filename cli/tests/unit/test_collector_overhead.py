from copy import deepcopy

import pytest

from inferyard.errors import EvidenceError
from inferyard.runtime.collector_overhead import ORDER, assess_overhead, freeze_protocol


def protocol(**kwargs):
    return freeze_protocol(
        **{
            "case_ids": ["a", "b"],
            "interval_ms": 100,
            "tolerance_ratio": 0.05,
            "max_wall_seconds": 60,
            "config_sha256": "a" * 64,
            "bundle_sha256": "b" * 64,
            "tool_source_sha256": "c" * 64,
            **kwargs,
        }
    )


def trials(p, durations=(100, 104, 104, 100)):
    return [
        {
            "mode": mode,
            "completed": True,
            "protocol_sha256": p["protocol_sha256"],
            "requests": [
                {
                    "case_id": case,
                    "duration_ns": duration,
                    "execution_state": "completed",
                    "output_identity": "same",
                }
                for case in p["case_ids"]
            ],
        }
        for mode, duration in zip(ORDER, durations, strict=True)
    ]


def test_exact_tolerance_and_independent_pairs():
    p = protocol()
    assert assess_overhead(p, trials(p, (100, 105, 105, 100)))["passed"]
    result = assess_overhead(p, trials(p, (100, 110, 90, 100)))
    assert not result["passed"]
    assert result["max_absolute_pair_median_change"] == 0.1
