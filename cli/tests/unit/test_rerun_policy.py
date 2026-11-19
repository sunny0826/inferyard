"""Rerun entry accepts implementation changes; comparison records differences."""

from copy import deepcopy
from pathlib import Path

import pytest

from inferyard.analysis.comparison import compare_trials
from inferyard.analysis.ledger import read_trial
from inferyard.application.types import CommandRequest
from inferyard.config.loader import load_config
from inferyard.config.single_plan import compile_single_plan
from inferyard.contracts.validation_cache import command_validation, validation_key
from inferyard.errors import ContractError, PreflightError
from inferyard.evidence.journal import trial_inputs
from inferyard.evidence.storage import read_json
from inferyard.implementation_identity import IdentityContext
from inferyard.runtime import runner
from tests.helpers import fixture_run
from tests.legacy_journal import LegacyJournal


@pytest.mark.parametrize("field", ["tool_version", "scorer", "measurement", "scoring", "legacy"])
def test_rerun_accepts_identity_difference_but_comparison_rejects(tmp_path, monkeypatch, field):
    root = fixture_run(tmp_path / "parent", states=["completed"] * 3, comparison_mode="model")
    parent = read_trial(root)
    changed = deepcopy(parent)
    if field == "scorer":
        changed["selection"]["scorer_sha256"] = "0" * 64
        condition = "selection.scorer_sha256"
    elif field in ("measurement", "scoring"):
        from inferyard.implementation_identity import digest

        role = changed["run"]["implementation_identity"][field]
        role["files"][0]["sha256"] = "0" * 64
        role["sha256"] = digest({k: v for k, v in role.items() if k != "sha256"})
        condition = "run.implementation_identity." + field
    elif field == "legacy":
        for data in (parent, changed):
            data["run"].pop("implementation_identity")
        changed["run"]["tool_source_sha256"] = "0" * 64
        condition = "run.tool_source_sha256"
    else:
        changed["run"]["tool_version"] = "older"
        condition = "run.tool_version"
    # Isolate the rerun gate after the normal sealed-evidence reader.
    monkeypatch.setattr(runner, "read_trial", lambda *a, **kw: changed)
    old_pid = parent["config"]["endpoint"]["server_pid"]

    def ticks(pid):
        if pid == old_pid:
            raise PreflightError("service_process_unavailable")
        return 98765

    monkeypatch.setattr(runner, "process_start_ticks", ticks)
    request = CommandRequest(
        "run", from_run=root, server_pid=old_pid + 1, endpoint_url="http://127.0.0.1:9090"
    )
    loaded, ancestor = runner.load_rerun(request)
    journal = LegacyJournal(
        tmp_path / "child",
        parent["plan"],
        parent["run"]["trial_id"],
        loaded.config.to_dict(),
        loaded.bundle.to_dict(),
        rerun_parent=ancestor,
        execution_mode="single",
    )
    try:
        run = read_json(journal.path / "run.json")
        assert run["relation"] == "rerun"
        assert run["parent_run_id"] == parent["run"]["run_id"]
    finally:
        journal.close()
    result = compare_trials(parent, changed)
    assert not any(result["eligibility"].values())
    assert next(c for c in result["conditions"] if c["field"] == condition)["status"] == "different"


def test_default_identity_context_reuses_cache_without_sharing_state() -> None:
    first = IdentityContext()
    first.value["measurement"]["sha256"] = "0" * 64
    second = IdentityContext()
    assert second.source == IdentityContext().source
    assert second.value["measurement"]["sha256"] != "0" * 64
    assert second.value == IdentityContext().value


def test_trial_inputs_revalidates_changed_bundle_inside_command_cache() -> None:
    loaded = load_config(Path(__file__).parents[1] / "fixtures/config/valid.toml")
    config, bundle = loaded.config.to_dict(), loaded.bundle.to_dict()
    plan = compile_single_plan(config, bundle)
    trial_id = plan["trials"][0]["trial_id"]
    with command_validation():
        trial, workload = trial_inputs(plan, trial_id, config, bundle)
        assert trial["trial_id"] == trial_id
        assert workload["workload_id"] == trial["workload_id"]
        # The unchanged documents entered the command-scoped cache.
        cache, key = validation_key("bundle", bundle)
        assert cache is not None and key in cache
        tampered = deepcopy(bundle)
        tampered["cases"][0]["case_id"] = 1
        with pytest.raises(ContractError):
            trial_inputs(plan, trial_id, config, tampered)
