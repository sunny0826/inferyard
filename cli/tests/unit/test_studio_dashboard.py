"""Studio cards reduce saved evidence only; missing and multiple sources stay explicit."""

from inferyard.analysis.comparison_input import comparison_input
from inferyard.reporting.studio_dashboard import dashboard_view
from inferyard.reporting.studio_resources import memory_view
from tests.helpers import fixture_run


def loaded(root, **kwargs):
    return comparison_input(fixture_run(root, **kwargs))[0]


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


def test_missing_rss_reason_does_not_reuse_available_memory(tmp_path):
    data = loaded(tmp_path / "run")
    samples = [s for s in data["samples"] if s["metric_name"] != "service_rss"]
    view = dashboard_view(data, memory_view(samples))
    assert view["sampled_peak_rss_bytes"] is None
    assert view["sampled_peak_rss_reason"] == "resource_samples_missing"
    assert view["sampled_minimum_available_bytes"] == 8 * 1024**3
