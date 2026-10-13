import asyncio
from dataclasses import replace
from pathlib import Path

import pytest

from inferyard.config.public_config_check import check_config
from inferyard.config.public_plan import materialize
from inferyard.contracts.validation import Document
from inferyard.evidence.storage import EvidenceError, json_bytes, read_json, sha256_file
from inferyard.reporting.comparison_report import comparison_input
from inferyard.reporting.public_package import projection, verify_public, write_public
from inferyard.runtime.batch_runner import execute_async
from tests.integration.test_batch_runner import batch
from tests.integration.test_runner import scenario as runner_scenario

scenario = runner_scenario


def test_public_summary_omits_private_text_and_preserves_denominators(tmp_path, scenario):
    original_request = scenario[0]
    config = original_request.config.config.to_dict()
    config["engine"]["startup_args"] += ["-m", config["model"]["local_path"]]
    loaded = replace(original_request.config, config=Document.parse("config", config))
    scenario = (replace(original_request, config=loaded), *scenario[1:])
    policy = {"max_external_cpu_percent": 10, "max_external_interval_seconds": 1}
    request, deps = batch(tmp_path, scenario, performance_policy=policy)
    _, result = asyncio.run(execute_async(request, deps))
    root = Path(result.details["runs"][0])
    before = {p.name: sha256_file(p) for p in root.iterdir() if p.is_file()}
    calls = len(scenario[2])
    data, source = comparison_input(root)
    data["config"]["conditions"]["background_load"] = "SECRET-background"
    data["config"]["endpoint"]["url"] = "http://SECRET-endpoint"
    data["config"]["model"]["repo"] = "SECRET-repo"
    data["config"]["conditions"]["epp"] = "SECRET-custom-epp"
    data["requests"][0]["content"] = "SECRET-answer"
    public = projection(data, source)
    assert b"SECRET" not in json_bytes(public)
    metrics = public["metrics"]
    assert metrics["observation_count"] == len(data["summary"]["metric_observations"])
    for actual, original in zip(
        metrics["observations"], data["summary"]["metric_observations"], strict=True
    ):
        for key in ("value", "sample_count", "numerator", "denominator", "excluded"):
            assert actual[key] == original[key]
        assert actual["status"]["value"] == original["status"]
        assert (actual["missing_reason"] is None) == (original["missing_reason"] is None)
        assert not actual["public_comparison_authorized"]
    recipe = public["reproduction"]
    assert recipe["conditions"]["epp"] is None
    assert "conditions.epp" in recipe["locally_required_sha256"]
    assert recipe["conditions"]["cache_policy"] == data["config"]["conditions"]["cache_policy"]
    assert recipe["execution"]["timeout_seconds"] == data["config"]["execution"]["timeout_seconds"]
    assert recipe["budget"] == data["plan"]["experiment"]["budget"]
    assert len(recipe["workload"]["case_order"]) == 3
    assert recipe["safety"] is None
    assert recipe["performance_environment"] == policy
    assert public["counts"]["completed"] == data["summary"]["counts"]["completed"]
    for category, row in public["quality_Q01"].items():
        assert (
            row["denominator"] == data["summary"]["quality"]["Q01"][category]["rate"]["denominator"]
        )
    out = tmp_path / "public"
    write_public(root, out)
    assert verify_public(out)["integrity_verified"]
    assert verify_public(out, root)["source_projection_verified"]
    assert check_config(out, loaded)["declared_configuration_matches"]
    changed = loaded.config.to_dict()
    changed["conditions"]["threads"] += 1
    mismatch = check_config(out, replace(loaded, config=Document.parse("config", changed)))
    assert not mismatch["declared_configuration_matches"]
    assert "conditions.threads" in mismatch["mismatched_fields"]
    assert not mismatch["ready_to_run"]
    rebuilt = tmp_path.with_name(tmp_path.name + "-reproduced")
    receipt = materialize(out, loaded, rebuilt)
    assert receipt["generation_requests"] == 0
    rebuilt_plan = read_json(rebuilt / "frozen/plan.json")
    assert [t["case_order"] for t in rebuilt_plan["trials"]] == [
        t["case_order"] for t in data["plan"]["trials"]
    ]
    assert rebuilt_plan["request_limit"] == data["plan"]["request_limit"]
    assert rebuilt_plan["experiment"]["performance_environment"] == policy
    combined = b"".join(p.read_bytes() for p in out.iterdir())
    assert str(root).encode() not in combined
    assert b"127.0.0.1" not in combined
    saved = read_json(out / "manifest.json")
    assert set(saved["files"]) == {"candidate.json", "redactions.json", "REPRODUCE.md"}
    (out / "private.txt").write_text("unexpected")
    with pytest.raises(EvidenceError, match="unlisted"):
        verify_public(out)
    (out / "private.txt").unlink()
    (out / "candidate.json").write_text("{}")
    with pytest.raises(EvidenceError, match="hash"):
        verify_public(out)
    assert len(scenario[2]) == calls
    assert before == {p.name: sha256_file(p) for p in root.iterdir() if p.is_file()}
