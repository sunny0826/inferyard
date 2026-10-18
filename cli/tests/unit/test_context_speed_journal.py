"""The actual Journal admits frozen profiles without losing ordinary plans."""

from dataclasses import replace

import pytest

from inferyard.config.context_speed_plan import freeze
from inferyard.config.loader import load_config
from inferyard.config.single_plan import compile_single_plan
from inferyard.contracts.validation import Document
from inferyard.evidence.journal import TrialJournal
from inferyard.evidence.trial_evidence import TrialEvidence
from tests.unit.test_context_speed_profile import inputs as inputs_fixture

inputs = inputs_fixture


@pytest.mark.parametrize("special", [True, False])
def test_actual_journal_preserves_frozen_profile(
    inputs, config_path, tmp_path, monkeypatch, special
):
    loaded = load_config(config_path)
    if special:
        _, config, bundle = inputs
        loaded = replace(
            loaded, config=Document.parse("config", config), bundle=Document.parse("bundle", bundle)
        )
        monkeypatch.setattr("inferyard.config.context_speed_plan.require_review", lambda _: None)
        plan = freeze(loaded)
    else:
        config, bundle = loaded.config.to_dict(), loaded.bundle.to_dict()
        plan = compile_single_plan(config, bundle)
    store = TrialJournal(tmp_path / "runs", plan, plan["trials"][0]["trial_id"], config, bundle)
    try:
        store.seal()
        saved = TrialEvidence(store.path).input("plan")
        assert saved == plan
        assert saved["experiment"].get("profile") == ("context-speed.v1" if special else None)
    finally:
        store.close()
