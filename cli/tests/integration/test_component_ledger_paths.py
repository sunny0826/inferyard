"""Single and relocated frozen plans bind ledger paths to the same source origin."""

import hashlib

import pytest

from inferyard.config.plan_inputs import read_frozen_plan
from inferyard.config.plan_math import plan_hash
from inferyard.config.planning import write_plan
from inferyard.evidence.storage import EvidenceError, json_bytes, read_json
from tests.integration.test_phase2_plan import input_package


@pytest.mark.parametrize("legacy", [False, True])
@pytest.mark.parametrize("tamper", ["ledger", "number_type", "bool_type"])
def test_ledger_path_survives_relocation_and_old_snapshot_remains_readable(
    tmp_path, config_path, legacy, tamper
):
    source = input_package(tmp_path, config_path)
    config_path = source.parent / "config.json"
    config = read_json(config_path)
    config["model"]["component_ledger_path"] = "lineage.json"
    raw = json_bytes(config)
    config_path.write_bytes(raw)
    experiment = read_json(source)
    experiment["workloads"][0]["config"]["sha256"] = hashlib.sha256(raw).hexdigest()
    source.write_bytes(json_bytes(experiment))
    frozen = tmp_path / "frozen"
    plan = write_plan(source, frozen)
    expected_path = str(source.parent / "lineage.json")
    binding = plan["runtime_bindings"][0]
    snapshot_path = frozen / binding["config"]["path"]
    if legacy:
        # Recreate the exact pre-bugfix projection, with valid source/hash bindings.
        snapshot = read_json(snapshot_path)
        snapshot["model"]["component_ledger_path"] = "lineage.json"
        raw = json_bytes(snapshot)
        snapshot_path.write_bytes(raw)
        binding["config"]["sha256"] = hashlib.sha256(raw).hexdigest()
        plan["plan_sha256"] = plan_hash(plan)
        (frozen / "plan.json").write_bytes(json_bytes(plan))
    source.parent.rename(tmp_path / "removed-source")
    moved = tmp_path / "moved"
    frozen.rename(moved)
    before = {str(p.relative_to(moved)): p.read_bytes() for p in moved.rglob("*") if p.is_file()}
    _, loaded = read_frozen_plan(moved / "plan.json")
    assert (
        next(iter(loaded.values())).config.to_dict()["model"]["component_ledger_path"]
        == expected_path
    )
    assert before == {
        str(p.relative_to(moved)): p.read_bytes() for p in moved.rglob("*") if p.is_file()
    }

    snapshot_path = moved / binding["config"]["path"]
    snapshot = read_json(snapshot_path)
    if tamper == "ledger":
        snapshot["model"]["component_ledger_path"] = "unbound-other.json"
    elif tamper == "number_type":
        value = snapshot["generation"]["max_tokens"]
        snapshot["generation"]["max_tokens"] = float(value)
        assert value == snapshot["generation"]["max_tokens"]
    else:
        assert snapshot["generation"]["repeat_penalty"] == 1.0
        snapshot["generation"]["repeat_penalty"] = True
    raw = json_bytes(snapshot)
    snapshot_path.write_bytes(raw)
    binding["config"]["sha256"] = hashlib.sha256(raw).hexdigest()
    plan["plan_sha256"] = plan_hash(plan)
    (moved / "plan.json").write_bytes(json_bytes(plan))
    with pytest.raises(EvidenceError, match="snapshot_differs_from_source"):
        read_frozen_plan(moved / "plan.json")
