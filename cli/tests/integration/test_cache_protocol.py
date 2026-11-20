import asyncio
from dataclasses import replace

import pytest

from inferyard.config.planning import compile_plan
from inferyard.contracts.validation import Document
from inferyard.errors import ContractError, EvidenceError
from inferyard.analysis.ledger import read_trial
from inferyard.evidence.storage import sha256_file
from inferyard.evidence.trial_evidence import TrialEvidence
from inferyard.runtime.trial_runner import run_trial
from tests.integration.test_trial_runner import inputs, scenario
from tests.layout_mutations import change_observation, change_request

__all__ = ["scenario"]


def prepare(scenario, policy):
    plan, loaded, deps, output = inputs(scenario)
    config = loaded.config.to_dict()
    config["conditions"]["cache_policy"] = policy
    loaded = replace(loaded, config=Document.parse("config", config))
    experiment = plan["experiment"]
    experiment["workloads"][0]["cache_protocol"] = "prism_prefix_reuse.v1"
    return compile_plan(experiment), loaded, deps, output


@pytest.mark.parametrize("policy", ["enabled", "disabled"])
def test_frozen_prefix_reuse_trial_and_offline_replay(scenario, policy):
    plan, loaded, deps, output = prepare(scenario, policy)
    code, data, root = asyncio.run(
        run_trial(
            plan, plan["trials"][0]["trial_id"], loaded, output, diagnostic=True, dependencies=deps
        )
    )
    assert code == 0
    calls = scenario[2]
    assert len(calls) == 8
    assert calls[0]["cache_prompt"] is False
    assert all(c["cache_prompt"] is (policy == "enabled") for c in calls[1:])
    assert (
        TrialEvidence(root).observation("effective_parameters", "stream")["cache"]["value"]
        == policy
    )
    assert data["summary"]["counts"]["completed"] == 3
    change_request(
        root,
        lambda body: body.update(cache_prompt=not body["cache_prompt"]),
        ordinal=1,
        sealed=True,
    )
    with pytest.raises(EvidenceError, match="cache_protocol_request_policy_mismatch"):
        read_trial(root)
