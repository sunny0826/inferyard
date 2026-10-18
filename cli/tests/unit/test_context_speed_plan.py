"""Preset freeze and unchanged formal review admission."""

from dataclasses import replace

import pytest

from inferyard.config.context_speed_plan import freeze
from inferyard.config.context_speed_profile import CASES
from inferyard.config.loader import load_config
from inferyard.config.plan_inputs import read_frozen_plan
from inferyard.contracts.validation import ContractError, Document
from tests.unit.test_context_speed_profile import inputs as profile_inputs

inputs = profile_inputs


@pytest.mark.parametrize("context", CASES)
def test_fixed_plan_five_trials_and_budget(inputs, config_path, tmp_path, monkeypatch, context):
    _, config, bundle = inputs
    config["conditions"]["context_size"] = context
    config["engine"]["startup_args"][3] = str(context)
    loaded = replace(
        load_config(config_path),
        config=Document.parse("config", config),
        bundle=Document.parse("bundle", bundle),
    )
    # Controlled review response only; real bundle approval bytes remain empty.
    monkeypatch.setattr("inferyard.config.context_speed_plan.require_review", lambda value: None)
    plan = freeze(loaded, tmp_path / "plan")
    reread, bound = read_frozen_plan(tmp_path / "plan/plan.json")
    assert plan == reread
    assert len(plan["trials"]) == plan["request_limit"] == 5
    assert [t["case_order"] for t in plan["trials"]] == [[CASES[context]]] * 5
    assert [t["repeat_index"] for t in plan["trials"]] == list(range(5))
    assert plan["experiment"]["budget"]["max_requests"] == 5
    assert bound["context-speed"].config.to_dict()["execution"]["repeats"] == 1
    timeout = config["execution"]["timeout_seconds"]
    idle = config["execution"]["idle_wait_seconds"]
    assert plan["total_budget_seconds"] == 5 * (
        4 * timeout + 5 * idle + config["telemetry"]["baseline_seconds"]
    )


def test_formal_draft_refused_before_output(inputs, config_path, tmp_path):
    _, config, bundle = inputs
    loaded = replace(
        load_config(config_path),
        config=Document.parse("config", config),
        bundle=Document.parse("bundle", bundle),
    )
    with pytest.raises(ContractError):
        freeze(loaded, tmp_path / "plan")
    assert not (tmp_path / "plan").exists()
