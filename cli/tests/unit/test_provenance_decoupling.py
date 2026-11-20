"""Verify Wave 3 decoupling: version-based identity, direct resources, and semantic history."""

import hashlib
import json
from copy import deepcopy
from pathlib import Path

import pytest

from inferyard import SCHEMA_VERSION, __version__
from inferyard.analysis.scoring import scorer_hash
from inferyard.config.loader import load_config
from inferyard.contracts.scoring_version import SCORER_VERSION
from inferyard.contracts.validation import json_bytes
from inferyard.errors import EvidenceError
from inferyard.extensions.workflow import digest, freeze, read_plan
from inferyard.provenance import tool_source_hash
from inferyard.provisioning.community_resources import resource_bytes
from inferyard.provisioning.preparation_io import PreparationError
from inferyard.runtime.history_read import HistoryContext, _identity_code


def test_tool_and_scorer_hashes_derive_from_versions() -> None:
    expected_tool = hashlib.sha256(f"inferyard:{__version__}".encode("utf-8")).hexdigest()
    expected_scorer = hashlib.sha256(
        f"inferyard.scorer:{SCORER_VERSION}".encode("utf-8")
    ).hexdigest()
    assert tool_source_hash() == expected_tool
    assert scorer_hash() == expected_scorer


@pytest.mark.parametrize(
    "bad_name",
    [
        "",
        ".",
        "..",
        "../methods.json",
        "bundles/../methods.json",
        "/etc/passwd",
        "bundles\\zh-smoke.json",
        "bundles/zh-smoke.json\x00",
        "C:bundles/zh-smoke.json",
        "bundles/missing.json",
    ],
)
def test_community_resource_bytes_rejects_invalid_paths(bad_name: str) -> None:
    with pytest.raises(PreparationError):
        resource_bytes(bad_name)


def test_community_resource_bytes_reads_packaged_bundle() -> None:
    raw = resource_bytes("bundles/zh-smoke.json")
    data = json.loads(raw.decode("utf-8"))
    assert data["bundle_id"] == "zh-smoke-v1"


def test_history_identity_allows_tool_source_drift_but_blocks_definition_version_drift(
    tmp_path: Path,
) -> None:
    plan_sha = "a" * 64
    plan = {
        "plan_sha256": plan_sha,
        "experiment": {
            "experiment_id": "exp-1",
            "definition_versions": {"measurement": "phase2.v1", "scoring": "phase2.v1"},
        },
        "trials": [{"trial_id": "trial-1", "workload_id": "w-1"}],
    }
    context = HistoryContext(
        root=tmp_path,
        plan=plan,
        loaded={},
        metadata={
            "plan_sha256": plan_sha,
            "diagnostic": False,
            "tool_source_sha256": "b" * 64,
        },
        full_verification=False,
        retain_binding=False,
    )
    run_data = {
        "run": {
            "plan_sha256": plan_sha,
            "experiment_id": "exp-1",
            "trial_id": "trial-1",
            "diagnostic": False,
            "tool_source_sha256": "c" * 64,
            "definition_versions": {"measurement": "phase2.v1", "scoring": "phase2.v1"},
        }
    }
    assert _identity_code(run_data, context) is None

    drifted = deepcopy(run_data)
    drifted["run"]["definition_versions"]["measurement"] = "phase2.v2"
    assert _identity_code(drifted, context) == "batch_run_identity_mismatch"


def test_extension_read_plan_ignores_tool_source_drift_but_validates_frozen_inputs(
    tmp_path: Path,
) -> None:
    loaded = load_config(Path(__file__).parents[1] / "fixtures/config/valid.toml")
    spec = {
        "schema_version": SCHEMA_VERSION,
        "definition": "closed_concurrency.v1",
        "server_slots": 2,
        "concurrency": 2,
        "case_ids": [loaded.bundle.to_dict()["cases"][0]["case_id"]],
        "timeout_seconds": 10,
        "drain_timeout_seconds": 5,
        "max_wall_seconds": 30,
        "latency_limit_ms": 5000,
        "require_quality": False,
        "max_dispatch_delay_ns": 5_000_000,
        "max_client_cpu_fraction": 0.5,
    }
    plan_dir = tmp_path / "ext"
    plan = freeze(spec, loaded, plan_dir)
    changed = deepcopy(plan)
    changed["tool_source_sha256"] = "0" * 64
    changed.pop("plan_sha256")
    changed["plan_sha256"] = digest(changed)
    plan_path = plan_dir / "plan.json"
    plan_path.write_bytes(json_bytes(changed))
    assert read_plan(plan_path, loaded)["tool_source_sha256"] == "0" * 64

    bad_config = deepcopy(changed)
    bad_config["config_sha256"] = "1" * 64
    bad_config.pop("plan_sha256")
    bad_config["plan_sha256"] = digest(bad_config)
    plan_path.write_bytes(json_bytes(bad_config))
    with pytest.raises(EvidenceError, match="extension_frozen_input_or_source_mismatch"):
        read_plan(plan_path, loaded)
