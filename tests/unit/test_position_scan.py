import runpy
from pathlib import Path

import pytest

from inferyard.config.bundle import require_review
from inferyard.config.plan_inputs import read_frozen_plan
from inferyard.contracts.validation import ContractError
from inferyard.evidence.storage import EvidenceError, json_bytes


def setup(tmp_path):
    scripts = Path(__file__).parents[2] / "scripts"
    bundle, protocol = runpy.run_path(str(scripts / "build_position_bundle.py"))["build"]()
    corpus = tmp_path / "corpus"
    corpus.mkdir()
    (corpus / "bundle.json").write_bytes(json_bytes(bundle))
    (corpus / "position-protocol.json").write_bytes(json_bytes(protocol))
    prepare = runpy.run_path(str(scripts / "prepare_position_scan.py"))["prepare"]
    return corpus, protocol, prepare


def test_position_scan_freezes_exact_shared_text_and_character_cohorts(tmp_path, config_path):
    corpus, _, prepare = setup(tmp_path)
    out = tmp_path / "scan"
    scan = prepare(corpus, [config_path], out)
    plan, configs = read_frozen_plan(out / "model-1/frozen/plan.json")
    assert scan["formal_request_limit"] == 18
    assert scan["independent_families"] == 2
    assert len(plan["trials"]) == 3
    assert [len(t["case_order"]) for t in plan["trials"]] == [6, 6, 6]
    assert all(w["input_target_tokens"] is None for w in plan["experiment"]["workloads"])
    assert (out / "model-1/frozen/bundle.json").read_bytes() == (
        corpus / "bundle.json"
    ).read_bytes()
    with pytest.raises(ContractError, match="human_corpus_review_required"):
        require_review(next(iter(configs.values())).bundle.to_dict())
    with pytest.raises(EvidenceError, match="duplicate_model"):
        prepare(corpus, [config_path, config_path], tmp_path / "duplicates")


@pytest.mark.parametrize("change", ["character_count", "position", "inventory", "family_count"])
def test_position_scan_rejects_misleading_cohort_metadata(tmp_path, config_path, change):
    corpus, protocol, prepare = setup(tmp_path)
    if change == "character_count":
        protocol["cohorts"][0]["body_characters"] += 1
    elif change == "position":
        protocol["cohorts"][0]["position"] = "back"
    elif change == "inventory":
        protocol["cohorts"].pop()
    else:
        protocol["independent_families"] = 18
    (corpus / "position-protocol.json").write_bytes(json_bytes(protocol))
    with pytest.raises(EvidenceError):
        prepare(corpus, [config_path], tmp_path / "invalid")
    assert not (tmp_path / "invalid").exists()
