"""Plan preview freezes supplied evidence without probing or generating requests."""

import hashlib
import json
import socket
import tomllib

import pytest

from inferyard.cli import main
from inferyard.config.loader import load_config
from inferyard.config.plan_inputs import read_frozen_plan
from inferyard.config.planning import prepare_plan, write_plan
from inferyard.evidence.storage import EvidenceError
from tests.unit.test_phase2_contracts import experiment


def input_package(tmp_path, config_path):
    source = tmp_path / "input"
    source.mkdir()
    loaded = load_config(config_path)
    config = loaded.config.to_dict()
    config["bundle"]["path"] = "bundle.json"
    data = experiment()
    case_ids = [c["case_id"] for c in loaded.bundle.to_dict()["cases"]]
    workload = data["workloads"][0]
    workload["protocol"]["case_ids"] = case_ids
    workload["timeout_seconds"] = config["execution"]["timeout_seconds"]
    data["budget"]["max_wall_seconds"] = 100_000
    for kind, value in (("config", config), ("bundle", loaded.bundle.to_dict())):
        raw = (json.dumps(value, ensure_ascii=False) + "\n").encode()
        (source / f"{kind}.json").write_bytes(raw)
        workload[kind] = {"path": f"{kind}.json", "sha256": hashlib.sha256(raw).hexdigest()}
    path = source / "experiment.json"
    path.write_text(json.dumps(data))
    return path


def forbid_network(*args, **kwargs):
    raise AssertionError("offline plan attempted a network request")


def test_dry_run_no_network_no_writes_and_freeze_is_content_addressed(
    tmp_path, config_path, monkeypatch, capsys
):
    path = input_package(tmp_path, config_path)
    before = {p.name: p.read_bytes() for p in path.parent.iterdir()}
    monkeypatch.setattr(socket, "socket", forbid_network)
    assert main(["plan", "--config", str(path), "--dry-run"]) == 0
    preview = json.loads(capsys.readouterr().out)
    assert preview["status"] == "previewed"
    assert preview["evidence_dir"] is None
    assert before == {p.name: p.read_bytes() for p in path.parent.iterdir()}
    output = tmp_path / "frozen"
    assert main(["plan", "--config", str(path), "--out", str(output)]) == 0
    frozen = json.loads(capsys.readouterr().out)
    assert frozen["details"] == preview["details"]
    assert json.loads((output / "plan.json").read_text()) == preview["details"]
    # The frozen package remains loadable after its original input directory disappears.
    path.parent.rename(tmp_path / "moved-input")
    assert read_frozen_plan(output / "plan.json")[0] == frozen["details"]
    assert main(["plan", "--config", str(output / "experiment.json"), "--out", str(output)]) == 4


def test_tampered_source_rejected_before_snapshot(tmp_path, config_path, capsys):
    path = input_package(tmp_path, config_path)
    (path.parent / "bundle.json").write_text("{}")
    out = tmp_path / "out"
    assert main(["plan", "--config", str(path), "--out", str(out)]) == 4
    assert not out.exists()
    assert "evidence_error" in capsys.readouterr().out


def test_symlink_escape_is_not_copied(tmp_path, config_path):
    path = input_package(tmp_path, config_path)
    corpus = path.parent / "bundle.json"
    outside = tmp_path / "outside.json"
    corpus.rename(outside)
    from tests.helpers import symlink_or_skip

    symlink_or_skip(corpus, outside)
    with pytest.raises(EvidenceError, match="escapes"):
        prepare_plan(path)


def test_plaintext_startup_secret_blocked_without_echo(tmp_path, config_path, capsys):
    path = input_package(tmp_path, config_path)
    config_path = path.parent / "config.json"
    config = json.loads(config_path.read_text())
    config["engine"]["startup_args"].extend(["--api-key", "sensitive-new-key"])
    raw = json.dumps(config).encode()
    config_path.write_bytes(raw)
    data = json.loads(path.read_text())
    data["workloads"][0]["config"]["sha256"] = hashlib.sha256(raw).hexdigest()
    path.write_text(json.dumps(data))
    assert main(["plan", "--config", str(path), "--dry-run"]) == 2
    captured = capsys.readouterr()
    assert "sensitive-new-key" not in captured.out + captured.err


def test_schema_cli_has_one_revision_and_rejects_version_selector(capsys):
    assert main(["--schema", "plan"]) == 0
    assert json.loads(capsys.readouterr().out)["$id"].endswith("v3:plan")
    assert main(["--schema", "plan", "--schema-version", "2"]) == 2
    capsys.readouterr()
    assert main(["--schema-version", "2", "--versions"]) == 2


def test_relative_toml_paths_survive_relocation(tmp_path, config_path):
    path = input_package(tmp_path, config_path)
    config_text = config_path.read_text()
    # The fixture contains a relative corpus path; bind it to the copied local bundle.

    old_literal = tomllib.loads(config_text)["bundle"]["path"]
    config_text = config_text.replace(old_literal, "bundle.json")
    raw = config_text.encode()
    (path.parent / "config.toml").write_bytes(raw)
    data = json.loads(path.read_text())
    data["workloads"][0]["config"] = {
        "path": "config.toml",
        "sha256": hashlib.sha256(raw).hexdigest(),
    }
    path.write_text(json.dumps(data))
    original_model = load_config(path.parent / "config.toml").config.to_dict()["model"][
        "local_path"
    ]
    frozen = tmp_path / "frozen"
    expected = write_plan(path, frozen)
    path.parent.rename(tmp_path / "removed-inputs")
    moved = tmp_path / "relocated"
    frozen.rename(moved)
    plan, configs = read_frozen_plan(moved / "plan.json")
    assert plan == expected
    actual = next(iter(configs.values()))
    assert actual.config.to_dict()["model"]["local_path"] == original_model
    assert actual.config.to_dict()["bundle"]["path"] == str(moved / "bundle.json")
    assert actual.bundle.to_dict()["cases"]


def test_runtime_snapshot_tampering_is_rejected_even_with_rehashed_plan(tmp_path, config_path):
    from inferyard.config.plan_math import plan_hash

    path = input_package(tmp_path, config_path)
    frozen = tmp_path / "frozen"
    plan = write_plan(path, frozen)
    binding = plan["runtime_bindings"][0]
    snapshot_path = frozen / binding["config"]["path"]
    snapshot = json.loads(snapshot_path.read_text())
    snapshot["generation"]["max_tokens"] = 42
    raw = json.dumps(snapshot).encode()
    snapshot_path.write_bytes(raw)
    binding["config"]["sha256"] = hashlib.sha256(raw).hexdigest()
    plan["plan_sha256"] = plan_hash(plan)
    (frozen / "plan.json").write_text(json.dumps(plan))
    with pytest.raises(EvidenceError, match="snapshot_differs_from_source"):
        read_frozen_plan(frozen / "plan.json")


def test_frozen_reader_rechecks_budget_bindings_and_seed_order(tmp_path, config_path):
    from inferyard.config.plan_math import plan_hash
    from inferyard.contracts.validation import ContractError

    path = input_package(tmp_path, config_path)
    source = json.loads(path.read_text())
    source["execution"].update(order="seeded", seed=7)
    path.write_text(json.dumps(source))
    output = tmp_path / "frozen"
    plan = write_plan(path, output)
    plan["experiment"]["workloads"][0]["output_budget_tokens"] += 1
    plan["plan_sha256"] = plan_hash(plan)
    (output / "plan.json").write_text(json.dumps(plan))
    with pytest.raises(ContractError, match="differs from config"):
        read_frozen_plan(output / "plan.json")
    plan["experiment"]["workloads"][0]["output_budget_tokens"] -= 1
    plan["trials"][0]["case_order"].reverse()
    plan["plan_sha256"] = plan_hash(plan)
    (output / "plan.json").write_text(json.dumps(plan))
    with pytest.raises(EvidenceError, match="trials_differ"):
        read_frozen_plan(output / "plan.json")
