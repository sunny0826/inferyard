"""Position family invariants apply both before freeze and during offline loading."""

from copy import deepcopy

import pytest

from inferyard.config.loader import load_config
from inferyard.config.plan_inputs import read_frozen_plan
from inferyard.config.plan_math import plan_hash
from inferyard.config.planning import prepare_plan, write_plan
from inferyard.contracts.validation import ContractError
from inferyard.evidence.storage import json_bytes, read_json, sha256_file
from tests.unit.test_phase2_contracts import experiment
from tests.unit.test_position import fixture


def package(root, config_path, *, change=False, separate=False):
    root.mkdir()
    loaded = load_config(config_path)
    data = experiment()
    data["budget"]["max_wall_seconds"] = 100000
    template = data["workloads"][0]
    data["workloads"] = []
    work, cases, _ = fixture()
    for i in range(2):
        bundle = loaded.bundle.to_dict()
        bundle.update(schema_version=3, task_protocol="quality", review_records=[])
        bundle["cases"] = [deepcopy(cases["cases"][i])]
        bundle["cases"][0]["reference_answer"] = "different" if change and i else "甲"
        config = loaded.config.to_dict()
        config["bundle"]["path"] = f"bundle-{i}.json"
        workload = deepcopy(template)
        workload.update(
            workload_id=f"position-{i}",
            purpose="position",
            protocol={"kind": "fixed", "case_ids": [bundle["cases"][0]["case_id"]]},
            position_cases=[deepcopy(work["position_cases"][i])],
            timeout_seconds=config["execution"]["timeout_seconds"],
            output_budget_tokens=config["generation"]["max_tokens"],
        )
        if separate and i:
            workload["position_cases"][0]["family_id"] = "separate-family"
        for kind, value in (("config", config), ("bundle", bundle)):
            path = root / f"{kind}-{i}.json"
            path.write_bytes(json_bytes(value))
            workload[kind] = {"path": path.name, "sha256": sha256_file(path)}
        data["workloads"].append(workload)
    path = root / "experiment.json"
    path.write_bytes(json_bytes(data))
    return path


def test_freeze_accepts_shared_family_and_rejects_changed_reference(tmp_path, config_path):
    source = package(tmp_path / "good", config_path)
    write_plan(source, tmp_path / "frozen")
    assert read_frozen_plan(tmp_path / "frozen/plan.json")[0]["request_limit"] == 6
    source = package(tmp_path / "bad", config_path, change=True)
    with pytest.raises(ContractError, match="across workloads"):
        prepare_plan(source)


def test_frozen_loader_rejects_rehashed_cross_workload_family_collision(tmp_path, config_path):
    source = package(tmp_path / "input", config_path, change=True, separate=True)
    root = tmp_path / "frozen"
    write_plan(source, root)
    read_frozen_plan(root / "plan.json")
    plan = read_json(root / "plan.json")
    plan["experiment"]["workloads"][1]["position_cases"][0]["family_id"] = "family-1"
    plan["plan_sha256"] = plan_hash(plan)
    (root / "plan.json").write_bytes(json_bytes(plan))
    with pytest.raises(ContractError, match="across workloads"):
        read_frozen_plan(root / "plan.json")
