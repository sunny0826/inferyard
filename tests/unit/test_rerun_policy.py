"""Rerun entry accepts implementation changes; comparison records differences."""

from copy import deepcopy

import pytest

from inferyard.analysis.comparison import compare_trials
from inferyard.application.types import CommandRequest
from inferyard.evidence.journal import TrialJournal
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import read_json
from inferyard.platforms.identity import PreflightError
from inferyard.runtime import runner
from tests.helpers import fixture_run


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
    journal = TrialJournal(
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


def test_mixed_identity_schemes_are_different_not_missing(tmp_path):
    parent = read_trial(fixture_run(tmp_path, comparison_mode="model"))
    other = deepcopy(parent)
    other["run"].pop("implementation_identity")
    result = compare_trials(parent, other)
    assert not any(result["eligibility"].values())
    assert (
        next(c for c in result["conditions"] if c["field"] == "run.identity_scheme")["status"]
        == "different"
    )
