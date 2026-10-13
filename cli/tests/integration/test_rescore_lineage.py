import asyncio
from copy import deepcopy
from pathlib import Path

import pytest

from inferyard.evidence.storage import EvidenceError, json_bytes, read_json, sha256_file
from inferyard.reporting.rescore import build_rescore, verify_rescore, write_rescore
from inferyard.runtime.batch_runner import execute_async
from tests.integration.test_batch_runner import batch
from tests.integration.test_runner import scenario as runner_scenario

scenario = runner_scenario


def test_multigeneration_replay_uses_immediate_parent_and_rejects_forgery(tmp_path, scenario):
    request, deps = batch(tmp_path, scenario)
    _, result = asyncio.run(execute_async(request, deps))
    root = Path(result.details["runs"][0])
    before = {p.name: sha256_file(p) for p in root.iterdir() if p.is_file()}
    calls = len(scenario[2])
    first, second, third = [tmp_path / name for name in ("first", "second", "third")]
    write_rescore(root, first, scorer_id="phase2.v2", reason="first revision")
    write_rescore(
        root, second, scorer_id="phase2.v1", reason="revert", parent_path=first / "analysis.json"
    )
    write_rescore(
        root, third, scorer_id="phase2.v2", reason="reapply", parent_path=second / "analysis.json"
    )
    assert verify_rescore(third)["verified"]
    changes = read_json(second / "rescore.json")["changes"]
    for row in changes:
        assert row["old_score"]["scorer_version"] == "phase2.v2"
        assert row["new_score"]["scorer_version"] == "phase2.v1"
    lineage = read_json(third / "parent-lineage.json")
    forged = deepcopy(lineage)
    forged["parent-lineage.json"]["analysis.json"]["metrics"][0]["value"] = 999
    (third / "parent-lineage.json").write_bytes(json_bytes(forged))
    with pytest.raises(EvidenceError, match="parent_recomputation"):
        verify_rescore(third)
    (third / "parent-lineage.json").write_bytes(json_bytes(lineage))
    # Copied lineage is sufficient even after the previous derived directories move.
    first.rename(tmp_path / "moved-first")
    second.rename(tmp_path / "moved-second")
    assert verify_rescore(third)["verified"]
    parent = read_json(third / "parent-analysis.json")
    with pytest.raises(EvidenceError, match="parent_evidence_required"):
        build_rescore(root, scorer_id="phase2.v2", reason="forged parent", parent=parent)
    assert len(scenario[2]) == calls
    assert before == {p.name: sha256_file(p) for p in root.iterdir() if p.is_file()}
