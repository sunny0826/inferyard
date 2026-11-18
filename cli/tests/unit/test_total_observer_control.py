import asyncio
import hashlib
from copy import deepcopy

import pytest

from inferyard.errors import EvidenceError
from inferyard.contracts.validation import json_bytes
from inferyard.extensions.total_observer_control import COVERAGE, assess_total, run_total


def packet():
    spec = {
        "schema_version": 3,
        "definition": "total_observer_control.v1",
        "case_ids": ["a", "b"],
        "tolerance_ratio": 0.05,
        "max_wall_seconds": 10,
        "max_guard_gap_ns": 2_000_000_000,
        "tool_source_sha256": "a" * 64,
        "config_sha256": "b" * 64,
        "bundle_sha256": "c" * 64,
        "guardian_policy_sha256": "d" * 64,
    }
    key = hashlib.sha256(json_bytes(spec)).hexdigest()
    arms = []
    for i, mode in enumerate(["off", "on", "on", "off"]):
        begin = (i + 1) * 1_000_000_000
        arms.append(
            {
                "mode": mode,
                "tool_source_sha256": "a" * 64,
                "config_sha256": "b" * 64,
                "bundle_sha256": "c" * 64,
                "protocol_sha256": key,
                "client_pid": 100,
                "clock_id": "clock",
                "begin_ns": begin,
                "after_close_ns": begin + 800_000_000,
                "finalized": True,
                "coverage": list(COVERAGE) if mode == "on" else [],
                "requests": [
                    {
                        "case_id": c,
                        "state": "completed",
                        "duration_ns": 100_000_000,
                        "output_sha256": "e" * 64,
                    }
                    for c in ["a", "b"]
                ],
            }
        )
    guard = {
        "policy_sha256": "d" * 64,
        "clock_id": "clock",
        "pid": 200,
        "samples": [
            {
                "monotonic_ns": i * 1_000_000_000,
                "safe": True,
                "environment_identity": {"host": "stable"},
            }
            for i in range(6)
        ],
    }
    return spec, arms, guard


def test_final_seal_cost_outside_request_latency_is_included_and_can_fail():
    spec, arms, guard = packet()
    arms[1]["after_close_ns"] += 100_000_000
    result = assess_total(spec, arms, guard)
    assert result["valid_control"] and not result["within_tolerance"]
    assert result["pairs"][0]["request_median_change_ratio"] == 0
    assert result["pairs"][0]["full_lifecycle_change_ratio"] == 0.125
