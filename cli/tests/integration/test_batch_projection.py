"""Real batch/resume orchestration consumes the same fields through a light read."""

import asyncio
import shutil
from pathlib import Path

import pytest

from inferyard.application.types import CommandRequest
from inferyard.application.verification import execute as verify
from inferyard.config.plan_inputs import read_frozen_plan
from inferyard.errors import EvidenceError
from inferyard.contracts.validation import json_bytes
from inferyard.evidence.storage import read_json, read_jsonl
from inferyard.evidence.trial_evidence import TrialEvidence
from inferyard.reporting.repetition_report import write_repetition_summary
from inferyard.reporting.studio_report import write_studio_report
from inferyard.runtime import batch_state
from inferyard.application.execution import execute_async
from tests.integration.test_batch_runner import batch
from tests.integration.test_batch_runner import scenario as batch_scenario
from tests.layout_mutations import change_input, change_observation, digest, reseal
from tests.unit.run_projection_helpers import reseal_file

scenario = batch_scenario


async def cancel_batch(request, deps, calls):
    task = asyncio.create_task(execute_async(request, deps))
    while len(calls) < 6 and not task.done():
        await asyncio.sleep(0.001)
    task.cancel()
    return await task


@pytest.mark.parametrize("name", ["memory.jsonl", "environment.jsonl", "schedule.jsonl"])
@pytest.mark.parametrize("repair_seal", [False, True])
def test_skipped_timeline_validation_remains_in_verify_and_report(
    tmp_path, scenario, name, repair_seal
):
    request, deps = batch(tmp_path, scenario)
    _, result = asyncio.run(execute_async(request, deps))
    root = Path(result.details["runs"][0])
    path = root / name
    original = path.read_bytes()
    assert original
    # Same byte count isolates the intentionally deferred hash/semantic checks.
    path.write_bytes(b"{}" + b" " * (len(original) - 3) + b"\n")
    if repair_seal:
        reseal_file(root, name)
    assert asyncio.run(execute_async(request, deps))[0] == 0
    reason = (
        {
            "memory.jsonl": "invalid_artifact_version",
            "environment.jsonl": "invalid_environment_observation",
            "schedule.jsonl": "invalid_collector_schedule",
        }[name]
        if repair_seal
        else "original_evidence_hash_mismatch"
    )
    for action in (
        lambda: verify(CommandRequest("verify", run=root)),
        lambda: verify(CommandRequest("verify", run=request.output_root)),
        lambda: write_studio_report([root], tmp_path / "bad-report"),
        lambda: write_repetition_summary(request.output_root, tmp_path / "bad-repeat"),
    ):
        with pytest.raises(EvidenceError, match=reason):
            action()
