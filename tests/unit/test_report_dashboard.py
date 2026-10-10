"""Dashboard display cannot invent hardware, pool sources or shrink failed denominators."""

from copy import deepcopy

import pytest

from inferyard.evidence.storage import EvidenceError, json_bytes
from inferyard.reporting import report_assets
from inferyard.reporting.report import _environment, build_index, verify_report, write_report
from inferyard.reporting.report_dashboard import dashboard_view
from inferyard.reporting.report_profile import profile_view
from tests.helpers import fixture_run


def loaded(tmp_path, **kwargs):
    from inferyard.reporting.comparison_report import comparison_input

    return comparison_input(fixture_run(tmp_path, **kwargs))[0]


def test_frozen_profile_does_not_infer_parameter_count_or_inspect_missing_weights(tmp_path):
    data = loaded(tmp_path)
    model = data["config"]["model"]
    model.update(display_name="Hf", local_path=r"C:\missing\Example-27B-Q4.gguf", packing="Q4")
    data["identity"]["files"] = [
        {"path": model["local_path"], "sha256": "wrong", "size": 42},
        {"path": "different", "sha256": model["sha256"], "size": 99},
    ]
    result = profile_view(data)
    assert result["title"] == "Example 27B"
    assert result["title_source"] == "frozen_model_filename"
    fields = {row["label"]: row for row in result["model_rows"]}
    assert fields["配置显示名"]["value"] == "Hf"
    assert fields["结构参数量"]["value"] is None
    assert fields["结构参数量"]["missing_reason"] == "parameter_count_not_frozen"
    assert fields["权重文件大小"]["value"] is None
    data["identity"]["files"].append(
        {"path": model["local_path"], "sha256": model["sha256"], "size": 0}
    )
    assert (
        next(row for row in profile_view(data)["model_rows"] if row["label"] == "权重文件大小")[
            "value"
        ]
        == 0
    )


def test_missing_hardware_and_effective_parameters_stay_missing(tmp_path):
    data = loaded(tmp_path)
    data.pop("environment_start")
    data["config"]["generation"]["temperature"] = 0
    data["identity"]["effective_parameters"]["parameters"]["temperature"]["effective"] = 0
    del data["identity"]["effective_parameters"]["parameters"]["seed"]
    result = profile_view(data)
    assert result["machine"]["cpu"] is None
    assert result["machine"]["memory_total_bytes"] is None
    assert result["graphics"] == []
    params = {row["name"]: row for row in result["generation"]}
    assert params["seed"]["requested"] == data["config"]["generation"]["seed"]
    assert params["seed"]["effective"] is None
    assert params["seed"]["verification"] == "unknown"
    assert params["temperature"]["effective"] == 0
    assert params["temperature"]["verification"] == "verified"


@pytest.mark.parametrize("invalid", ["known", True, -1, 16.0])
def test_untyped_historical_hardware_values_are_not_numeric_readings(tmp_path, invalid):
    data = loaded(tmp_path)
    data["environment_start"].update(memory_total_bytes=invalid, logical_cpus=invalid)
    profile = profile_view(data)
    assert profile["machine"]["memory_total_bytes"] is None
    assert profile["machine"]["logical_cpus"] is None
    fields = {row["label"]: row for row in profile["machine_rows"]}
    assert fields["总内存"]["missing_reason"] == "invalid_frozen_value"
    assert fields["逻辑 CPU"]["missing_reason"] == "invalid_frozen_value"


def test_five_states_and_unknown_quality_survive_dashboard_and_html(tmp_path):
    states = ["completed", "failed", "cancelled", "invalid", "not_executed"]
    root = fixture_run(tmp_path / "runs", states=states)
    index = write_report([root], tmp_path / "report")
    run = index["runs"][0]
    assert [row["state"] for row in run["requests"]] == states
    assert run["summary"]["counts"]["planned"] == 5
    quality = run["dashboard"]["quality"]
    assert quality["denominator"] == 2 and quality["excluded"] == 3
    assert not quality["complete"] and quality["reason"]
    assert "value" not in quality  # Q01 stays six category rates, never a combined score.
    assert run["dashboard"]["latency"]["excluded"] == 4
    html = (tmp_path / "report/report.html").read_text()
    assert "仅已记录通过数" in html
    assert all(f'data-execution="{state}"' in html for state in states)
    assert verify_report(tmp_path / "report")["verified"]


def test_completed_latency_excludes_failures_and_uses_nearest_rank(tmp_path):
    data = loaded(
        tmp_path, states=["completed"] * 20 + ["failed"], durations=list(range(1, 21)) + [999]
    )
    view = dashboard_view(data, {"charts": []})
    assert view["latency"]["p50"] == 10_000
    assert view["latency"]["p95"] == 19_000
    assert view["latency"]["sample_count"] == 20
    assert view["latency"]["excluded"] == 1
    assert view["latency"]["p95_exploratory"]
    data["requests"] = data["requests"][:2]
    latency = dashboard_view(data, {"charts": []})["latency"]
    assert latency["p95"] is None and latency["p95_reason"] == "insufficient_samples"


def test_resource_sources_are_not_pooled_and_zero_is_not_missing(tmp_path):
    data = loaded(tmp_path)
    chart = {"metric": "service_rss", "source": "native", "max": 0, "min": 0}
    view = dashboard_view(data, {"charts": [chart]})
    assert view["sampled_peak_rss_bytes"] == 0
    assert view["sampled_peak_rss_reason"] is None
    assert view["sampled_minimum_available_bytes"] is None
    mixed = [chart, {**chart, "source": "observer", "max": 999}]
    view = dashboard_view(data, {"charts": mixed})
    assert view["sampled_peak_rss_bytes"] is None
    assert view["sampled_peak_rss_reason"] == "multiple_sources"
    assert view["rss_sources"] == ["native", "observer"]


def test_new_detail_fields_are_escaped_and_requested_values_do_not_become_effective(tmp_path):
    root = fixture_run(tmp_path / "runs")
    index = build_index([root], tmp_path / "report")
    run = index["runs"][0]
    poison = '<img src=x onerror="window.pwned=true">'
    run["profile"]["title"] = poison
    run["profile"]["model_rows"][0]["value"] = poison
    from inferyard.reporting.report_content import text_ref

    run["requests"][0]["reference_answer_ref"] = text_ref(index["contents"], poison)
    run["requests"][0]["rule_results"] = [
        {"rule": poison, "expected": poison, "observed": poison, "passed": None, "reason": poison}
    ]
    run["profile"]["generation"][0].update(effective=None, verification="unknown")
    html = _environment().get_template("report.html").render(index=index)
    assert "<img src=x" not in html and "&lt;img src=x" in html
    assert "未知 ·" in html
    assert "实际值缺失表示没有对应核验记录" in html
    assert "<script src=" not in html and '<link rel="stylesheet"' not in html


def test_current_template_identity_binds_all_partial_assets(tmp_path, monkeypatch):
    resources = tmp_path / "package"
    templates = resources / "templates"
    templates.mkdir(parents=True)
    (templates / "report.html").write_text("entry")
    style = templates / "report_styles.html"
    style.write_text("first")
    monkeypatch.setattr(report_assets, "files", lambda _: resources)
    original = report_assets.template_hash(8)
    style.write_text("tampered")
    assert report_assets.template_hash(8) != original


@pytest.mark.parametrize("version", [True, False, 0, 7, 9, "2", None])
def test_unknown_report_format_is_rejected_before_loading_sources(tmp_path, version):
    from inferyard.evidence.formats import UnsupportedFormat

    error = UnsupportedFormat if type(version) is int else EvidenceError
    with pytest.raises(error):
        build_index([tmp_path / "missing-source"], tmp_path / "out", format_version=version)


def test_verifier_rejects_boolean_report_version(tmp_path):
    root = fixture_run(tmp_path / "runs")
    out = tmp_path / "report"
    index = write_report([root], out)
    bad = deepcopy(index)
    bad["report_format_version"] = True
    (out / "index.json").write_bytes(json_bytes(bad))
    with pytest.raises(EvidenceError, match="presentation_bytes_changed"):
        verify_report(out)
