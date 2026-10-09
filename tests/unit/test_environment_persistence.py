"""ADR 041 legal old/new records and unchanged rejection/qualification boundaries."""

import asyncio
from copy import deepcopy
from types import SimpleNamespace

import pytest

from inferyard.analysis.environment import (
    assess_environment,
    assess_schedule,
    environment_qualification,
)
from inferyard.analysis.environment_identity import LINUX_FIELDS, MACOS_FIELDS
from inferyard.evidence.environment_projection import periodic_environment
from inferyard.evidence.storage import EvidenceError
from inferyard.platforms import telemetry
from inferyard.platforms.power_macos import parse
from inferyard.runtime import environment_schedule
from tests.unit.test_environment import state

OMITTED = {"cpu_flags", "os_release", "gpu", "mem_available_bytes", "page_size_bytes"}
SCHEDULE = {"scheduled_ns": 0, "actual_ns": 0, "late_ns": 0, "collector_work_ns": 100}
REQUESTS = [{"execution_state": "completed", "t_send_ns": 0, "t_terminal_ns": 1_000_000_000}]


def full_snapshot(platform):
    snapshot = {
        **state(),
        "platform": platform,
        "cpu_flags": ["synthetic_flag"],
        "os_release": {"VERSION_ID": "synthetic"},
        "gpu": {"devices": [{"name": "synthetic"}]},
        "mem_available_bytes": 1024,
        "page_size_bytes": 4096,
        "read_started_ns": 1,
        "read_finished_ns": 2,
        "boot_id": "synthetic-boot",
        "swap_source": "synthetic-source",
    }
    if platform == "Darwin":
        snapshot["macos_power_policy"] = parse("AC Power:\n lowpowermode 0\n", True)
    return snapshot


@pytest.mark.parametrize("platform", ["Linux", "Darwin", "Windows"])
@pytest.mark.parametrize("change", [None, "kernel", "swap", "policy", "missing_identity"])
def test_old_and_projected_records_have_identical_assessments(platform, change):
    start = full_snapshot(platform)
    middle = deepcopy(start)
    if change == "kernel":
        middle["kernel"] = "different"
    elif change == "swap":
        middle["swap_pages"]["pswpin"] = 1
    elif change == "policy":
        if platform == "Darwin":
            middle["macos_power_policy"]["low_power_mode"] = 1
        else:
            middle["cpu_policies"]["policies"][0]["scaling_governor"] = "different"
    elif change == "missing_identity":
        del middle["cpu_model"]
    original = deepcopy(middle)
    projected = periodic_environment(middle)
    assert middle == original
    assert projected == {key: value for key, value in middle.items() if key not in OMITTED}
    assert set(LINUX_FIELDS + MACOS_FIELDS).intersection(middle) <= projected.keys()
    conditions = {"macos_power_policy": start.get("macos_power_policy")}
    old = assess_environment(
        start, start, [{"monotonic_ns": 500_000_000, "snapshot": middle}], conditions, REQUESTS
    )
    new = assess_environment(
        start, start, [{"monotonic_ns": 500_000_000, "snapshot": projected}], conditions, REQUESTS
    )
    old_schedule = assess_schedule([{**SCHEDULE, "queue_depth": 0}], 1000)
    new_schedule = assess_schedule([SCHEDULE], 1000)
    assert new == old
    assert new_schedule == old_schedule
    external = {"all_requests_eligible": True, "requests": []}
    old_qualification = environment_qualification(old, external, old_schedule, REQUESTS, 1000)
    new_qualification = environment_qualification(new, external, new_schedule, REQUESTS, 1000)
    assert new_qualification == old_qualification
    assert new_qualification["eligible"] == (change is None)


@pytest.mark.parametrize("key", list(SCHEDULE))
@pytest.mark.parametrize("value", [None, True, -1, 1.5])
@pytest.mark.parametrize("legacy", [False, True])
def test_schedule_still_rejects_invalid_retained_fields(key, value, legacy):
    row = {**SCHEDULE, key: value}
    if value is None:
        del row[key]
    if legacy:
        row["queue_depth"] = 0
    with pytest.raises(EvidenceError, match="invalid_collector_schedule"):
        assess_schedule([row], 1000)


@pytest.mark.parametrize(
    "row",
    [
        {"monotonic_ns": True, "snapshot": {}},
        {"monotonic_ns": -1, "snapshot": {}},
        {"monotonic_ns": 0, "snapshot": []},
    ],
)
def test_environment_still_rejects_invalid_observation(row):
    with pytest.raises(EvidenceError, match="invalid_environment_observation"):
        assess_environment(state(), state(), [row], {}, [])


@pytest.mark.parametrize("module", [telemetry, environment_schedule])
def test_both_writers_project_each_fresh_read_without_mutating_snapshot(monkeypatch, module):
    clock, reads, observations = [0.0], [], []
    snapshot = full_snapshot("Darwin")
    original = deepcopy(snapshot)
    sampler = telemetry.Sampler(
        SimpleNamespace(
            sample=lambda row: None,
            observation=lambda name, row: observations.append((name, row)),
            flush_due=lambda: None,
        ),
        {
            "telemetry": {"interval_ms": 1000},
            "endpoint": {"server_pid": 1, "process_start_ticks": 2},
        },
    )
    sampler.collect = lambda *args: []

    def read():
        reads.append(clock[0])
        return {**snapshot, "read_finished_ns": int(clock[0] * 1e9) + 2}

    async def sleep(seconds):
        clock[0] += seconds
        sampler.stopped = clock[0] >= 2

    monkeypatch.setattr(module, "environment_snapshot", read)
    monkeypatch.setattr(
        module.asyncio, "get_running_loop", lambda: SimpleNamespace(time=lambda: clock[0])
    )
    monkeypatch.setattr(sampler, "wait", sleep)
    monkeypatch.setattr(module.time, "monotonic_ns", lambda: int(clock[0] * 1e9))
    asyncio.run(sampler.run() if module is telemetry else module.run_sampler(sampler))
    assert reads == [0, 1]
    assert snapshot == original
    environments = [row for name, row in observations if name == "environment.jsonl"]
    schedules = [row for name, row in observations if name == "schedule.jsonl"]
    assert len(environments) == len(schedules) == 2
    for index, row in enumerate(environments):
        assert row["snapshot"] == {
            **periodic_environment(snapshot),
            "read_finished_ns": index * 1_000_000_000 + 2,
        }
        assert row["monotonic_ns"] == index * 1_000_000_000
        assert row["phase"] == "probe"
    assert all(set(row) == set(SCHEDULE) for row in schedules)


def test_sealed_old_and_new_runs_rebuild_the_same_context_and_report_profile(tmp_path):
    from shutil import copytree

    from inferyard.evidence.storage import json_bytes, read_json
    from inferyard.reporting.comparison_report import comparison_input
    from inferyard.reporting.report_profile import profile_view
    from tests.helpers import fixture_run
    from tests.unit.test_run_projection import reseal_file

    old_root = fixture_run(tmp_path / "old")
    snapshot = read_json(old_root / "environment.start.json")
    snapshot.update(
        mem_available_bytes=1024,
        page_size_bytes=4096,
        gpu={"devices": [{"name": "fixture-gpu"}]},
    )
    for name in ("environment.start.json", "environment.end.json"):
        (old_root / name).write_bytes(json_bytes(snapshot))
        reseal_file(old_root, name)
    observation = {"monotonic_ns": 1_500_000_000, "phase": "formal", "snapshot": snapshot}
    (old_root / "environment.jsonl").write_bytes(json_bytes(observation))
    (old_root / "schedule.jsonl").write_bytes(json_bytes({**SCHEDULE, "queue_depth": 0}))
    for name in ("environment.jsonl", "schedule.jsonl"):
        reseal_file(old_root, name)
    old_bytes = {p.name: p.read_bytes() for p in old_root.iterdir() if p.is_file()}
    new_root = copytree(old_root, tmp_path / "projected")
    (new_root / "environment.jsonl").write_bytes(
        json_bytes({**observation, "snapshot": periodic_environment(snapshot)})
    )
    (new_root / "schedule.jsonl").write_bytes(json_bytes(SCHEDULE))
    for name in ("environment.jsonl", "schedule.jsonl"):
        reseal_file(new_root, name)
    old, _ = comparison_input(old_root)
    new, _ = comparison_input(new_root)
    for key in ("environment", "collector_schedule", "environment_qualification"):
        assert (
            old["summary"]["measurement_context"][key] == new["summary"]["measurement_context"][key]
        )
    assert old["summary"]["resources"] == new["summary"]["resources"]
    assert profile_view(old) == profile_view(new)
    for name in ("environment.start.json", "environment.end.json", "memory.jsonl"):
        assert (new_root / name).read_bytes() == old_bytes[name]
    assert all((old_root / name).read_bytes() == raw for name, raw in old_bytes.items())
