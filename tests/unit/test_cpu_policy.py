from copy import deepcopy

import pytest

from inferyard.platforms.cpu_policy import assess, cpu_list, snapshot


def inventory():
    return {
        "source": "linux.sysfs.cpufreq.policy.v1",
        "online_cpus": [0, 1],
        "policies": [
            {
                "policy_id": "policy0",
                "affected_cpus": [0, 1],
                "related_cpus": [0, 1],
                "scaling_driver": "driver",
                "scaling_governor": "performance",
                "energy_performance_preference": "performance",
                "scaling_min_freq": 400000,
                "scaling_max_freq": 3000000,
            }
        ],
    }


def test_all_domains_are_read_without_substituting_cpu0(tmp_path):
    root = tmp_path / "devices/system/cpu"
    root.mkdir(parents=True)
    (root / "online").write_text("0-3")
    for n, members, governor in [(0, "0 1", "performance"), (2, "2 3", "powersave")]:
        policy = root / f"cpufreq/policy{n}"
        policy.mkdir(parents=True)
        for key, value in {
            "affected_cpus": members,
            "related_cpus": members,
            "scaling_driver": "driver",
            "scaling_governor": governor,
            "energy_performance_preference": "performance",
            "scaling_min_freq": "400000",
            "scaling_max_freq": "3000000",
        }.items():
            (policy / key).write_text(value)
    result = snapshot(tmp_path)
    assert result["online_cpus"] == [0, 1, 2, 3]
    assert len(result["policies"]) == 2
    assert assess([result, result], {})["complete_and_stable"]
    assert (
        "cpu_policy_frozen_mismatch:governor"
        in assess([result], {"governor": "performance"})["reasons"]
    )
    (root / "cpufreq/policy2/affected_cpus").unlink()
    assert not assess([snapshot(tmp_path)], {})["complete_and_stable"]


@pytest.mark.parametrize("change", ["missing", "overlap", "hotplug", "transient", "unknown"])
def test_incomplete_and_transient_domains_never_qualify(change):
    start = inventory()
    middle = deepcopy(start)
    if change == "missing":
        middle = None
    elif change == "overlap":
        middle["policies"].append({**middle["policies"][0], "policy_id": "policy1"})
    elif change == "hotplug":
        middle["online_cpus"].append(2)
    elif change == "transient":
        middle["policies"][0]["scaling_max_freq"] -= 1
    else:
        middle["policies"][0]["energy_performance_preference"] = None
    assert not assess([start, middle, start], {})["complete_and_stable"]


def test_cpu_list_parsing_and_missing_inventory():
    assert cpu_list("0-2,4 6") == [0, 1, 2, 4, 6]
    for value in (None, "", "4-2", "1-x", "1000000"):
        assert cpu_list(value) is None
    assert assess([None], {})["reasons"] == ["cpu_policy_inventory_missing"]
