"""The standalone observer keeps v2 streams and binds layout-specific source hashes."""

from copy import deepcopy

import pytest

from inferyard.evidence.trial_evidence import TrialEvidence
from tests.evidence_layout_helpers import build_current_original, pack_layout2
from tests.unit.test_observer_analysis import bridge as observer_bridge
from tests.unit.test_observer_analysis import records, save

bridge = observer_bridge


@pytest.fixture
def bound(tmp_path, bridge):
    old = build_current_original(tmp_path / "original")
    root = pack_layout2(old, tmp_path / "container")
    evidence = TrialEvidence(root)
    run, config = evidence.input("run"), evidence.input("config")
    values = records()
    for item in values:
        item["definition"] = "lab_observer.v2"
    values[0]["disk_scope"] = "benchmark_run_filesystem"
    values[0]["benchmark_binding"] = {
        "definition": "benchmark-inputs-binding.v1",
        "run_id": run["run_id"],
        "inputs_file_sha256": evidence.input_reference("run")["sha256"],
        "run_member_sha256": evidence.input_digest("run"),
        "config_member_sha256": evidence.input_digest("config"),
        "model_label_declared": config["model"]["display_name"],
        "backend_declared": config["engine"]["backend"],
    }
    for target in (values[0]["targets"][0], values[1]["processes"][0], values[2]["processes"][0]):
        target["pid"] = config["endpoint"]["server_pid"]
        target["process_start_ticks"] = config["endpoint"]["process_start_ticks"]
    return root, values


def test_layout2_observer_link_is_readonly_and_not_performance_qualification(
    bound, bridge, tmp_path
):
    root, values = bound
    before = {p.name: p.read_bytes() for p in root.iterdir()}
    summary = bridge.analyze(save(tmp_path, values))
    assert summary["benchmark_binding"] == values[0]["benchmark_binding"]
    linked = bridge.link_benchmark(summary, root)
    assert linked["source_binding_verified"]
    assert linked["whole_run_coverage_verified"] is False
    assert linked["performance_comparison_qualified"] is False
    assert before == {p.name: p.read_bytes() for p in root.iterdir()}


@pytest.mark.parametrize(
    "field", ["inputs_file_sha256", "run_member_sha256", "config_member_sha256"]
)
def test_layout2_observer_checks_each_hash(bound, bridge, tmp_path, field):
    root, values = bound
    summary = bridge.analyze(save(tmp_path, values))
    summary["benchmark_binding"][field] = "0" * 64
    with pytest.raises(bridge.ObserverError):
        bridge.link_benchmark(summary, root)


@pytest.mark.parametrize(
    "damage", ["legacy_field", "unknown", "missing_definition", "null_hash", "missing_hash"]
)
def test_layout2_observer_binding_shape_is_strict(bound, bridge, tmp_path, damage):
    _, values = bound
    binding = values[0]["benchmark_binding"]
    if damage == "legacy_field":
        binding["run_file_sha256"] = "0" * 64
    elif damage == "unknown":
        binding["definition"] = "benchmark-inputs-binding.v99"
    elif damage == "missing_definition":
        binding.pop("definition")
    elif damage == "null_hash":
        binding["inputs_file_sha256"] = None
    else:
        binding.pop("run_member_sha256")
    with pytest.raises(bridge.ObserverError):
        bridge.analyze(save(tmp_path, values))


def test_layout1_hashes_cannot_bind_layout2(bound, bridge, tmp_path):
    root, values = bound
    summary = bridge.analyze(save(tmp_path, values))
    binding = deepcopy(summary["benchmark_binding"])
    summary["benchmark_binding"] = {
        key: binding[key] for key in ("run_id", "model_label_declared", "backend_declared")
    } | {
        "run_file_sha256": binding["run_member_sha256"],
        "config_file_sha256": binding["config_member_sha256"],
    }
    with pytest.raises(bridge.ObserverError):
        bridge.link_benchmark(summary, root)
