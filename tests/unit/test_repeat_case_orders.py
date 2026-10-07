from copy import deepcopy

import pytest

from inferyard.config.planning import compile_plan
from inferyard.contracts.validation import ContractError
from tests.unit.test_phase2_contracts import experiment


def test_explicit_orders_survive_new_experiment_identity_and_seed():
    source = experiment()
    workload = source["workloads"][0]
    cases = workload["protocol"]["case_ids"]
    workload["repeat_case_orders"] = [list(reversed(cases)) for _ in range(workload["repeats"])]
    source["execution"].update(order="seeded", seed=123)
    before = compile_plan(source)
    renamed = deepcopy(source)
    renamed["experiment_id"] = "another-machine"
    renamed["execution"]["seed"] = 456
    after = compile_plan(renamed)
    assert [t["case_order"] for t in before["trials"]] == [t["case_order"] for t in after["trials"]]
    assert before["trials"][0]["trial_id"] != after["trials"][0]["trial_id"]


@pytest.mark.parametrize("failure", ["missing_repeat", "missing_case", "duplicate_case"])
def test_explicit_orders_require_one_complete_permutation_per_repeat(failure):
    source = experiment()
    workload = source["workloads"][0]
    cases = workload["protocol"]["case_ids"]
    orders = [list(cases) for _ in range(workload["repeats"])]
    if failure == "missing_repeat":
        orders.append(list(cases))
    elif failure == "missing_case":
        orders[0] = cases[:-1]
    else:
        orders[0] = [cases[0]] * len(cases)
    workload["repeat_case_orders"] = orders
    with pytest.raises(ContractError):
        compile_plan(source)
