import asyncio
from dataclasses import replace

from inferyard.config.planning import compile_plan
from inferyard.contracts.validation import Document
from inferyard.reporting.studio_report import write_studio_report
from inferyard.runtime.trial_runner import run_trial
from tests.integration.test_runner import scenario as runner_scenario
from tests.integration.test_trial_runner import inputs
from tests.unit.test_position import fixture

scenario = runner_scenario


def test_position_ledger_stays_in_summary_and_is_not_republished(scenario, tmp_path):
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
    view = data["summary"]["positions"]
    assert view["family_count"] == 1 and view["variant_count"] == 3
    assert {tuple(row["positions"]) for row in view["bins"]} == {
        ("front",),
        ("middle",),
        ("back",),
    }
    out = tmp_path / "position-report"
    document = write_studio_report([root], out)
    run = document["runs"][0]
    assert run["diagnostic"] is True
    assert "scan_view" not in run and "positions" not in run
    assert [row["case_id"] for row in run["requests"]] == [
        request["case_id"] for request in data["requests"]
    ]
