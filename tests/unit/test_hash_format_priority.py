"""An unsupported marker cannot hide damage to an existing hash binding."""

import json
from copy import deepcopy
from pathlib import Path

import pytest

from inferyard.cli import main
from inferyard.config.plan_math import plan_hash
from inferyard.config.planning import write_plan
from inferyard.evidence.storage import EvidenceError, json_bytes, read_json
from inferyard.reporting.rescore import write_rescore
from tests.helpers import fixture_run
from tests.integration.test_phase2_plan import input_package
from tests.unit.test_verification import digest_tree


def check_unchanged(root, args, code, capsys):
    before = digest_tree(root)
    assert main(args) == code
    result = json.loads(capsys.readouterr().out)
    assert ("unsupported_format" in result["limitations"]) is (code == 2)
    assert digest_tree(root) == before


@pytest.mark.parametrize("marker", ["experiment", "plan", "migrated"])
@pytest.mark.parametrize("rehash", [False, True])
def test_current_plan_hash_precedes_version_rejection(
    tmp_path, config_path, capsys, marker, rehash
):
    source = input_package(tmp_path, config_path)
    out = tmp_path / "frozen"
    plan = write_plan(source, out)
    args = ["verify", "--path", str(out / "plan.json")]
    check_unchanged(out, args, 0, capsys)
    if marker == "experiment":
        plan["experiment"]["schema_version"] = 2
    elif marker == "plan":
        plan["schema_version"] = 2
    else:
        plan["experiment"]["origin"] = "migrated"
    if rehash:
        plan["plan_sha256"] = plan_hash(plan)
    (out / "plan.json").write_bytes(json_bytes(plan))
    # A matching hash cannot reconcile contradictory outer/nested versions.
    check_unchanged(out, args, 2 if rehash and marker == "migrated" else 4, capsys)


@pytest.mark.parametrize("damage", [None, "hash", "hash-type", "version-type"])
def test_complete_old_plan_only_unsupported_when_existing_hash_is_intact(tmp_path, capsys, damage):
    legacy = Path(__file__).parents[1] / "fixtures/legacy/synthetic-v2/plan.json"
    plan = read_json(legacy)
    if damage == "hash":
        plan["plan_sha256"] = "0" * 64
    elif damage == "hash-type":
        plan["plan_sha256"] = True
    elif damage == "version-type":
        plan["schema_version"] = True
    path = tmp_path / "plan.json"
    path.write_bytes(json_bytes(plan))
    check_unchanged(tmp_path, ["verify", "--path", str(path)], 2 if damage is None else 4, capsys)


@pytest.mark.parametrize(
    "damage", ["parent-version", "record-and-analysis-version", "old-analysis"]
)
def test_rescore_hashes_precede_deleted_analysis_versions(tmp_path, capsys, damage):
    root = fixture_run(tmp_path / "source")
    source_before = digest_tree(root)
    out = tmp_path / "rescore"
    write_rescore(root, out, scorer_id="phase2.v2", reason="synthetic revision")
    for command in ("verify",):
        check_unchanged(
            out, [command, "--run" if command == "rescore-check" else "--path", str(out)], 0, capsys
        )
    if damage == "parent-version":
        path = out / "parent-analysis.json"
        value = read_json(path)
        value["schema_version"] = 2
        path.write_bytes(json_bytes(value))
    else:
        path = out / "analysis.json"
        value = deepcopy(read_json(path))
        value["schema_version"] = 2
        path.write_bytes(json_bytes(value))
        if damage == "record-and-analysis-version":
            record_path = out / "rescore.json"
            record = read_json(record_path)
            record["reason"] = "changed without updating stored self-hash"
            record_path.write_bytes(json_bytes(record))
    for command in ("verify",):
        check_unchanged(
            out,
            [command, "--run" if command == "rescore-check" else "--path", str(out)],
            2 if damage == "old-analysis" else 4,
            capsys,
        )
    assert digest_tree(root) == source_before


def test_rescore_writer_checks_parent_lineage_hash_before_version(tmp_path):
    root = fixture_run(tmp_path / "source")
    parent = tmp_path / "parent"
    write_rescore(root, parent, scorer_id="phase2.v2", reason="first revision")
    analysis = read_json(parent / "analysis.json")
    analysis["schema_version"] = 2
    (parent / "analysis.json").write_bytes(json_bytes(analysis))
    record = read_json(parent / "rescore.json")
    record["reason"] = "changed without updating stored self-hash"
    (parent / "rescore.json").write_bytes(json_bytes(record))
    before = digest_tree(tmp_path)
    out = tmp_path / "next"
    with pytest.raises(EvidenceError, match="rescore_recomputation_mismatch"):
        write_rescore(
            root,
            out,
            scorer_id="phase2.v1",
            reason="next revision",
            parent_path=parent / "analysis.json",
        )
    assert not out.exists()
    assert digest_tree(tmp_path) == before
