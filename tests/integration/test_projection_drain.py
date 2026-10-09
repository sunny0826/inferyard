"""Projected drain proof retains lab identity bindings and native non-proof."""

import asyncio
import json
from pathlib import Path

import pytest

from inferyard.evidence.storage import json_bytes
from inferyard.runtime.runner import execute_async
from tests.integration.test_lab_cli import lab_scenario  # noqa: F401
from tests.integration.test_native_cli import native_scenario  # noqa: F401
from tests.integration.test_runner import scenario  # noqa: F401
from tests.unit.test_run_projection import assert_projection, reseal_file, same_error


def test_lab_drain_is_identical(lab_scenario):  # noqa: F811
    request, deps, _ = lab_scenario
    code, result = asyncio.run(execute_async(request, deps))
    assert code == 0
    data = assert_projection(Path(result.evidence_dir))
    assert data["service_drain"]["source"] == "/lab/v1/lifecycle"


def test_native_never_acquires_engine_drain(native_scenario):  # noqa: F811
    request, deps, _ = native_scenario
    code, result = asyncio.run(execute_async(request, deps))
    assert code == 0
    assert assert_projection(Path(result.evidence_dir))["service_drain"] is None


@pytest.mark.parametrize("damage", ["snapshot", "identity", "request"])
def test_lab_drain_keeps_consumed_proof_validation(lab_scenario, damage):  # noqa: F811
    request, deps, _ = lab_scenario
    code, result = asyncio.run(execute_async(request, deps))
    assert code == 0
    root = Path(result.evidence_dir)
    if damage == "snapshot":
        path = root / "events.jsonl"
        events = [json.loads(line) for line in path.read_bytes().splitlines()]
        last_idle = next(e for e in reversed(events) if e["event_type"] == "idle_observed")
        last_idle["data"]["lab_snapshot"] = {}
        path.write_bytes(b"".join(json_bytes(e) for e in events))
    elif damage == "identity":
        path = root / "service.props.json"
        value = json.loads(path.read_bytes())
        value["build_id"] = "wrong-build"
        path.write_bytes(json_bytes(value))
    else:
        path = next(root.glob("*__formal.request.json"))
        value = json.loads(path.read_bytes())
        value["lab_server_instance_id"] = "0" * 32
        path.write_bytes(json_bytes(value))
    reseal_file(root, path.name)
    same_error(root)


def test_native_capability_manifest_binding_is_preserved(native_scenario):  # noqa: F811
    request, deps, _ = native_scenario
    code, result = asyncio.run(execute_async(request, deps))
    assert code == 0
    root = Path(result.evidence_dir)
    path = root / "manifest.json"
    value = json.loads(path.read_bytes())
    del value["files"]["engine-capabilities.json"]
    path.write_bytes(json_bytes(value))
    assert same_error(root) == "engine_observation_manifest_mismatch"


def test_native_idle_event_is_rejected(native_scenario):  # noqa: F811
    request, deps, _ = native_scenario
    code, result = asyncio.run(execute_async(request, deps))
    assert code == 0
    root = Path(result.evidence_dir)
    path = root / "events.jsonl"
    events = [json.loads(line) for line in path.read_bytes().splitlines()]
    observed = next(e for e in events if e["event_type"] == "native_observed")
    observed.update(
        event_type="idle_observed", data={"state": "idle", "source": "/slots:is_processing"}
    )
    path.write_bytes(b"".join(json_bytes(e) for e in events))
    reseal_file(root, path.name)
    assert same_error(root) == "native_evidence_cannot_prove_engine_drain"
