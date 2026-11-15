"""Studio cards reduce saved evidence only; missing and multiple sources stay explicit."""

from copy import deepcopy

from inferyard.analysis.comparison_input import comparison_input
from inferyard.reporting.studio_dashboard import dashboard_view
from inferyard.reporting.studio_profile import profile_view
from inferyard.reporting.studio_resources import memory_view
from tests.helpers import fixture_run


def loaded(root, **kwargs):
    return comparison_input(fixture_run(root, **kwargs))[0]


def test_profile_title_uses_display_name_unless_it_is_generic(tmp_path):
    data = loaded(tmp_path / "run")
    model = data["config"]["model"]
    model.update(display_name="Hf", local_path=r"C:\missing\Example-27B-Q4.gguf", packing="Q4")
    assert profile_view(data)["title"] == "Example 27B"
    model.update(display_name="synthetic-A")
    assert profile_view(data)["title"] == "synthetic-A"


def test_missing_environment_is_null_and_never_zero(tmp_path):
    data = loaded(tmp_path / "run")
    data.pop("environment_start")
    profile = profile_view(data)
    assert profile["machine"] == {"cpu": None, "logical_cpus": None, "memory_total_bytes": None}
    assert profile["engine"]["adapter"] == "prism_llama_server_v1"
    assert profile["engine"]["backend"] == "cpu"


def test_invalid_frozen_counters_are_not_numeric_readings(tmp_path):
    data = loaded(tmp_path / "run")
    data["environment_start"].update(
        memory_total_bytes=16.0, logical_cpus=True, cpu_model=""
    )
    profile = profile_view(data)
    assert profile["machine"] == {"cpu": None, "logical_cpus": None, "memory_total_bytes": None}


def test_latency_samples_completed_requests_and_needs_twenty_for_p95(tmp_path):
    data = loaded(
        tmp_path / "run",
        states=["completed"] * 20 + ["failed"],
        durations=list(range(1, 22)),
    )
    latency = dashboard_view(data, memory_view(data["samples"]))["latency"]
    assert latency == {
        "sample_count": 20,
        "excluded": 1,
        "min": 1000.0,
        "p50": 10000.0,
        "p95": 19000.0,
        "max": 20000.0,
    }

    data = loaded(tmp_path / "short", states=["completed", "failed"], durations=[1, 2])
    latency = dashboard_view(data, memory_view(data["samples"]))["latency"]
    assert latency["p95"] is None
    assert latency["sample_count"] == 1 and latency["excluded"] == 1


def test_five_execution_states_do_not_shrink_the_latency_denominator(tmp_path):
    states = ["completed", "failed", "cancelled", "invalid", "not_executed"]
    data = loaded(tmp_path / "run", states=states)
    view = dashboard_view(data, memory_view(data["samples"]))
    assert [row["execution_state"] for row in data["requests"]] == states
    assert view["latency"]["sample_count"] == 1
    assert view["latency"]["excluded"] == 4
    assert view["quality"]["denominator"] == 2
    assert view["quality"]["excluded"] == 3
    assert view["quality"]["complete"] is False
    assert view["quality"]["reason"] == "quality_incomplete_or_not_applicable"


def test_single_source_memory_is_mib_converted_to_float_bytes(tmp_path):
    data = loaded(tmp_path / "run")
    memory = memory_view(data["samples"])
    assert memory == {
        "service_rss": {"synthetic_fixture": 7168.0},
        "system_mem_available": {"synthetic_fixture": 8192.0},
        "reason": None,
    }
    view = dashboard_view(data, memory)
    assert view["sampled_peak_rss_bytes"] == 7 * 1024**3
    assert type(view["sampled_peak_rss_bytes"]) is float
    assert view["sampled_peak_rss_reason"] is None
    assert view["sampled_minimum_available_bytes"] == 8 * 1024**3
    assert type(view["sampled_minimum_available_bytes"]) is float
    assert view["minimum_available_reason"] is None


def test_zero_is_a_measurement_not_a_missing_value(tmp_path):
    data = loaded(tmp_path / "run")
    for sample in data["samples"]:
        if sample["metric_name"] == "service_rss":
            sample["value"] = 0
    view = dashboard_view(data, memory_view(data["samples"]))
    assert view["sampled_peak_rss_bytes"] == 0.0
    assert view["sampled_peak_rss_reason"] is None


def test_multiple_sources_are_never_pooled(tmp_path):
    data = loaded(tmp_path / "run")
    extra = [
        {**sample, "source": "observer", "seq": sample["seq"] + 1000}
        for sample in data["samples"]
    ]
    view = dashboard_view(data, memory_view(data["samples"] + extra))
    assert view["sampled_peak_rss_bytes"] is None
    assert view["sampled_peak_rss_reason"] == "multiple_sources"
    assert view["sampled_minimum_available_bytes"] is None
    assert view["minimum_available_reason"] == "multiple_sources"


def test_missing_samples_and_mixed_clocks_are_explicit(tmp_path):
    data = loaded(tmp_path / "run")
    assert memory_view([]) == {
        "service_rss": {},
        "system_mem_available": {},
        "reason": "resource_samples_missing",
    }
    view = dashboard_view(data, memory_view([]))
    assert view["sampled_peak_rss_bytes"] is None
    assert view["sampled_peak_rss_reason"] == "resource_samples_missing"

    samples = deepcopy(data["samples"])
    samples[0]["clock_id"] = "different"
    view = dashboard_view(data, memory_view(samples))
    assert view["sampled_peak_rss_bytes"] is None
    assert view["sampled_peak_rss_reason"] == "resource_clock_mismatch"


def test_missing_rss_reason_does_not_reuse_available_memory(tmp_path):
    data = loaded(tmp_path / "run")
    samples = [s for s in data["samples"] if s["metric_name"] != "service_rss"]
    view = dashboard_view(data, memory_view(samples))
    assert view["sampled_peak_rss_bytes"] is None
    assert view["sampled_peak_rss_reason"] == "resource_samples_missing"
    assert view["sampled_minimum_available_bytes"] == 8 * 1024**3
