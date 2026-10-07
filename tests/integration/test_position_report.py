import asyncio
from dataclasses import replace

from inferyard.config.planning import compile_plan
from inferyard.contracts.validation import Document
from inferyard.reporting.report import verify_report, write_report
from inferyard.runtime.trial_runner import run_trial
from tests.integration.test_runner import scenario as runner_scenario
from tests.integration.test_trial_runner import inputs
from tests.unit.test_position import fixture

scenario = runner_scenario


def test_position_report_projects_diagnostic_ledger_without_pooling(scenario, tmp_path):
    plan, loaded, deps, output = inputs(scenario)
    position, corpus, _ = fixture()
    bundle = loaded.bundle.to_dict()
    bundle.update(schema_version=3, task_protocol="quality", review_records=[])
    bundle["cases"] = [{**case, "reference_answer": "甲"} for case in corpus["cases"]]
    loaded = replace(loaded, bundle=Document.parse("bundle", bundle))
    source = plan["experiment"]
    workload = source["workloads"][0]
    workload.update(
        purpose="position", position_cases=position["position_cases"], protocol=position["protocol"]
    )
    plan = compile_plan(source)
    code, data, root = asyncio.run(
        run_trial(
            plan, plan["trials"][0]["trial_id"], loaded, output, diagnostic=True, dependencies=deps
        )
    )
    assert code == 0
    out = tmp_path / "position-report"
    index = write_report([root], out)
    view = index["runs"][0]["scan_view"]["positions"]
    assert view == data["summary"]["positions"]
    assert view["family_count"] == 1 and view["variant_count"] == 3
    assert {tuple(row["positions"]) for row in view["bins"]} == {("front",), ("middle",), ("back",)}
    assert "材料位置 · 题族内变体" in (out / "report.html").read_text()
    assert index["runs"][0]["diagnostic"]
    assert verify_report(out)["semantic_verified"]
