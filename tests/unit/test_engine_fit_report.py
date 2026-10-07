"""Offline provenance, semantic tampering and escaped portable diagnostic reports."""

import hashlib
import json
import shutil
from collections import Counter
from copy import deepcopy
from html.parser import HTMLParser

import pytest

from inferyard.config.engine_fit import digest, request_rows
from inferyard.contracts.validation import ContractError
from inferyard.evidence.storage import EvidenceError, json_bytes
from inferyard.reporting.engine_fit import compare, seal_run, verify
from inferyard.reporting.engine_fit_metrics import observations


@pytest.fixture
def plan():
    files = [{"path": "weights.safetensors", "sha256": "1" * 64, "size": 1024}]
    value = {
        "schema_version": 3,
        "definition": "engine_fit_plan.v1",
        "model": {"path": "/offline/model", "sha256": digest(files), "files": files},
        "host": {"sha256": "2" * 64, "system": "Linux"},
        "engines": ["vllm", "sglang"],
        "cases": [{"id": "case", "prompt": "<script>alert('prompt')</script> & 测试"}],
        "parameters": {
            "max_tokens": 32,
            "temperature": 0,
            "repetitions": 2,
            "request_timeout_seconds": 30,
            "min_free_memory_bytes": 128 * 2**20,
            "min_free_disk_bytes": 256 * 2**20,
            "max_temperature_celsius": 85,
        },
        "request_count": 2,
        "max_request_wall_seconds": 60,
    }
    value["plan_id"] = digest(value)
    return value


def _idle(engine):
    names = {
        "vllm": ["num_requests_running", "num_requests_waiting"],
        "sglang": ["num_running_reqs", "num_queue_reqs"],
    }[engine]
    return {
        "idle": True,
        "source": "/metrics",
        "values": [
            {"metric": engine + ":" + name, "labels": {"model": "offline"}, "value": 0.0}
            for name in names
        ],
    }


def _data(plan, engine="vllm"):
    rows = request_rows(plan)
    for index, row in enumerate(rows):
        row.update(
            status="completed",
            reason=None,
            response={
                "text": "<img src=x onerror=alert('answer')> 中文",
                "finish_reason": "stop",
                "elapsed_ms": 100.0 + index * 100,
                "first_content_ms": 20.0 + index * 10,
                "prompt_tokens": 10,
                "completion_tokens": 4,
                "usage_missing_reason": None,
            },
        )
    run = {
        "schema_version": 3,
        "definition": "engine_fit_run.v1",
        "run_id": "run-" + engine,
        "plan_id": plan["plan_id"],
        "engine": engine,
        "diagnostic": True,
        "performance_comparison_qualified": False,
        "completeness": "complete",
        "stop_reason": None,
        "binding": {
            "pid": 123,
            "start_ticks": 456,
            "origin": "http://127.0.0.1:8000",
            "address": "127.0.0.1",
            "port": 8000,
            "executable_sha256": "3" * 64,
            "argv_sha256": "4" * 64,
            "listener_inode": "56",
            "model_binding": {
                "engine": engine,
                "path": plan["model"]["path"],
                "device": 1,
                "inode": 10,
                "source": "verified_startup_directory_identity",
            },
        },
        "service": {
            "engine": engine,
            "version": "1.0.0",
            "served_model": "offline",
            "version_source": "/version" if engine == "vllm" else "/get_server_info",
            "measurement_source_sha256": "5" * 64,
            "idle_before": _idle(engine),
        },
        "resources": [
            {
                "phase": "before_run",
                "memory_available_bytes": 8 * 2**30,
                "process_tree_rss_bytes": 2 * 2**30,
                "process_tree_cpu_seconds": 1.2,
                "process_count": 2,
                "scope": {"rss": "process tree; shared pages repeated"},
                "missing_reasons": {},
                "temperature_samples": [],
                "temperature_missing_reason": "no_verified_temperature_source",
            }
        ],
        "counts": {
            "planned": len(rows),
            "completed": len(rows),
            "failed": 0,
            "cancelled": 0,
            "invalid": 0,
            "not_executed": 0,
        },
        "limitations": ["diagnostic_only"],
    }
    return run, rows


def _seal(path, plan, engine="vllm"):
    run, rows = _data(plan, engine)
    path.mkdir()
    seal_run(path, plan, run, rows)
    return path


def _replace(path, name, value):
    content = value if isinstance(value, bytes) else json_bytes(value)
    (path / name).write_bytes(content)
    manifest = json.loads((path / "manifest.json").read_text())
    manifest["files"][name] = hashlib.sha256(content).hexdigest()
    (path / "manifest.json").write_bytes(json_bytes(manifest))


def test_seal_overwrites_checkpoints_but_never_a_sealed_run(tmp_path, plan):
    run, rows = _data(plan)
    (tmp_path / "checkpoint.json").write_text("ignored in-flight identity")
    for name in ("plan.json", "run.json", "requests.json"):
        (tmp_path / name).write_text("{}")
    assert seal_run(tmp_path, plan, run, rows) == run
    saved = verify(tmp_path)
    assert saved["run"]["counts"]["completed"] == 2
    assert saved["plan"] == plan
    assert set(saved["manifest"]["files"]) == {
        "plan.json",
        "run.json",
        "requests.json",
        "report.html",
    }
    assert (tmp_path / "checkpoint.json").read_text() == "ignored in-flight identity"
    (tmp_path / "checkpoint.json").unlink()
    assert verify(tmp_path)["run"] == run
    before = (tmp_path / "manifest.json").read_bytes()
    with pytest.raises(EvidenceError, match="already_sealed"):
        seal_run(tmp_path, plan, run, rows)
    assert (tmp_path / "manifest.json").read_bytes() == before
    (tmp_path / "checkpoint.json").write_text("{}")
    (tmp_path / "requests.json").unlink()
    with pytest.raises(EvidenceError):
        verify(tmp_path)


def test_comparison_survives_removing_originals_and_keeps_nulls(tmp_path, plan):
    left = _seal(tmp_path / "left", plan)
    right = tmp_path / "right"
    right.mkdir()
    run, rows = _data(plan, "sglang")
    rows[0]["response"].update(
        completion_tokens=None, usage_missing_reason="endpoint_usage_incomplete"
    )
    run["resources"][0]["process_tree_rss_bytes"] = None
    run["resources"][0]["missing_reasons"]["process_tree_rss_bytes"] = "proc_unreadable"
    seal_run(right, plan, run, rows)
    out = tmp_path / "comparison"
    result = compare([left, right], out)
    shutil.rmtree(left)
    shutil.rmtree(right)
    assert verify(out)["comparison"] == result
    assert result["performance_comparison_qualified"] is False
    assert result["sides"][0]["observations"]["client_elapsed_median_ms"]["value"] == 150
    values = result["sides"][1]["observations"]
    assert values["endpoint_completion_tokens_total"]["value"] is None
    assert values["endpoint_completion_tokens_total"]["reason"] == "incomplete_endpoint_usage"
    assert values["boundary_process_tree_rss_max_bytes"]["value"] is None


@pytest.mark.parametrize(
    "key,value",
    [
        ("text", 5),
        ("finish_reason", "tool_calls"),
        ("elapsed_ms", True),
        ("elapsed_ms", float("inf")),
        ("elapsed_ms", float("nan")),
        ("elapsed_ms", 10**400),
        ("first_content_ms", -1),
        ("first_content_ms", 999),
        ("first_content_ms", None),
        ("prompt_tokens", False),
        ("completion_tokens", 1.5),
        ("completion_tokens", None),
        ("usage_missing_reason", "unexpected"),
    ],
)
def test_completed_response_rejects_invalid_values(tmp_path, plan, key, value):
    run, rows = _data(plan)
    rows[0]["response"][key] = value
    with pytest.raises(EvidenceError):
        seal_run(tmp_path, plan, run, rows)
    assert not (tmp_path / "manifest.json").exists()


@pytest.mark.parametrize(
    "mutation",
    [
        lambda r: r["counts"].update(completed=1),
        lambda r: r["counts"].update(failed=False),
        lambda r: r.update(performance_comparison_qualified=True),
        lambda r: r.update(completeness="complete", stop_reason="cancelled"),
        lambda r: r["binding"].update(pid=True),
        lambda r: r["binding"]["model_binding"].update(path="/different/model"),
        lambda r: r["resources"][0].update(process_tree_rss_bytes=None),
        lambda r: r["resources"][0].update(process_count=False),
        lambda r: r["service"]["idle_before"]["values"][0].update(value=1),
    ],
)
def test_semantic_tampering_rejected_even_with_updated_hashes(tmp_path, plan, mutation):
    path = _seal(tmp_path / "run", plan)
    run = json.loads((path / "run.json").read_text())
    mutation(run)
    _replace(path, "run.json", run)
    with pytest.raises(EvidenceError):
        verify(path)


def test_all_five_terminal_states_remain_in_denominator(tmp_path, plan):
    plan["parameters"]["repetitions"] = plan["request_count"] = 5
    plan["max_request_wall_seconds"] = 150
    plan["plan_id"] = digest({key: value for key, value in plan.items() if key != "plan_id"})
    run, rows = _data(plan)
    for row, status in zip(
        rows, ("completed", "failed", "cancelled", "invalid", "not_executed"), strict=True
    ):
        if status != "completed":
            row.update(status=status, reason="stopped", response=None)
    run.update(completeness="incomplete", stop_reason="cancelled")
    run["counts"] = {"planned": 5, **Counter(row["status"] for row in rows)}
    seal_run(tmp_path, plan, run, rows)
    assert verify(tmp_path)["run"]["counts"]["planned"] == 5


def test_plan_hash_and_duplicate_json_keys_rejected(tmp_path, plan):
    path = _seal(tmp_path / "run", plan)
    plan["cases"][0]["prompt"] = "changed"
    _replace(path, "plan.json", plan)
    with pytest.raises(EvidenceError, match="invalid_plan_evidence"):
        verify(path)
    path2 = _seal(tmp_path / "other", deepcopy(verify_fixture_plan(plan)))
    body = (
        (path2 / "run.json")
        .read_bytes()
        .replace(b'"schema_version":3', b'"schema_version":3,"schema_version":3')
    )
    _replace(path2, "run.json", body)
    with pytest.raises(EvidenceError, match="invalid_json"):
        verify(path2)


def verify_fixture_plan(plan):
    plan["plan_id"] = digest({key: value for key, value in plan.items() if key != "plan_id"})
    return plan


def test_offline_plan_semantics_are_evidence_errors_with_valid_hashes(tmp_path, plan):
    path = _seal(tmp_path / "run", plan)
    plan["request_count"] += 1
    verify_fixture_plan(plan)
    _replace(path, "plan.json", plan)
    with pytest.raises(EvidenceError, match="engine_fit_invalid_plan_evidence") as failure:
        verify(path)
    assert isinstance(failure.value.__cause__, ContractError)


@pytest.mark.parametrize("version", [1, 2])
def test_html_is_escaped_and_versioned_rebuild_is_explicit(tmp_path, plan, version):
    path = _seal(tmp_path / "run", plan)
    html = (path / "report.html").read_text()
    assert "&lt;script&gt;" in html and "&lt;img" in html
    assert "<script" not in html and "<img" not in html
    assert "@media(max-width:600px)" in html and 'name="viewport"' in html
    assert "客户端总耗时" in html and "端点输出 token" in html
    assert "RSS 相加可能重复计算共享页" in html
    parser = HTMLParser()
    parser.feed(html)
    manifest = json.loads((path / "manifest.json").read_text())
    manifest["definition"] = f"engine_fit_manifest.v{version}"
    (path / "manifest.json").write_bytes(json_bytes(manifest))
    _replace(path, "report.html", html.replace("引擎适配诊断", "unverified").encode())
    if version == 2:
        assert verify(path)["kind"] == "engine_fit_run"
    with pytest.raises(EvidenceError, match="html_rebuild_mismatch"):
        verify(path, rerender=version == 2)


def test_new_engine_fit_seal_needs_no_renderer_but_checks_saved_bytes(tmp_path, plan, monkeypatch):
    from inferyard.reporting import engine_fit

    path = _seal(tmp_path / "run", plan)
    monkeypatch.setattr(engine_fit, "render", lambda *args, **kwargs: "renderer changed")
    assert verify(path)["manifest"]["definition"] == "engine_fit_manifest.v2"
    with pytest.raises(EvidenceError, match="html_rebuild_mismatch"):
        verify(path, rerender=True)
    (path / "report.html").write_text("changed bytes")
    with pytest.raises(EvidenceError, match="hash_mismatch"):
        verify(path)


def test_manifest_refuses_arbitrary_names_and_symlinks(tmp_path, plan):
    path = _seal(tmp_path / "run", plan)
    manifest = json.loads((path / "manifest.json").read_text())
    manifest["files"]["../outside"] = "0" * 64
    (path / "manifest.json").write_bytes(json_bytes(manifest))
    with pytest.raises(EvidenceError, match="manifest_files"):
        verify(path)
    path2 = _seal(tmp_path / "other", plan)
    (path2 / "run.json").rename(tmp_path / "outside.json")
    (path2 / "run.json").symlink_to(tmp_path / "outside.json")
    with pytest.raises(EvidenceError, match="unsafe_file"):
        verify(path2)


def test_compare_rejects_duplicates_plans_sources_and_existing_out(tmp_path, plan):
    left = _seal(tmp_path / "left", plan)
    right = _seal(tmp_path / "right", plan, "sglang")
    with pytest.raises(EvidenceError, match="duplicate_engine"):
        compare([left, left], tmp_path / "dup")
    with pytest.raises(EvidenceError, match="output_exists"):
        compare([left, right], tmp_path)
    changed = deepcopy(plan)
    changed["host"]["sha256"] = "9" * 64
    verify_fixture_plan(changed)
    other = _seal(tmp_path / "other", changed, "sglang")
    with pytest.raises(EvidenceError, match="comparison_plan_mismatch"):
        compare([left, other], tmp_path / "mismatch")
    out = tmp_path / "comparison"
    result = compare([left, right], out)
    result["source_runs"][0]["path"] = str(left)
    _replace(out, "comparison.json", result)
    with pytest.raises(EvidenceError, match="unsafe_source_reference"):
        verify(out)


def test_copied_source_directory_symlink_rejected(tmp_path, plan):
    left = _seal(tmp_path / "left", plan)
    right = _seal(tmp_path / "right", plan, "sglang")
    out = tmp_path / "comparison"
    compare([left, right], out)
    shutil.rmtree(out / "sources" / "0")
    (out / "sources" / "0").symlink_to(left, target_is_directory=True)
    with pytest.raises(EvidenceError, match="unsafe_directory"):
        verify(out)


def test_compare_requires_same_measurement_source(tmp_path, plan):
    left = _seal(tmp_path / "left", plan)
    right = tmp_path / "right"
    right.mkdir()
    run, rows = _data(plan, "sglang")
    run["service"]["measurement_source_sha256"] = "a" * 64
    seal_run(right, plan, run, rows)
    with pytest.raises(EvidenceError, match="comparison_measurement_source_mismatch"):
        compare([left, right], tmp_path / "comparison")


def test_empty_completed_subset_does_not_become_zero(plan):
    run, rows = _data(plan)
    for row in rows:
        row.update(status="not_executed", reason="cancelled", response=None)
    result = observations({"run": run, "requests": rows})
    assert result["client_elapsed_median_ms"] == {
        "value": None,
        "reason": "no_completed_requests",
        "sample_count": 0,
        "expected_count": 0,
    }


def test_comparison_bool_cannot_be_replaced_by_integer(tmp_path, plan):
    left = _seal(tmp_path / "left", plan)
    right = _seal(tmp_path / "right", plan, "sglang")
    out = tmp_path / "comparison"
    result = compare([left, right], out)
    result["diagnostic"] = 1
    _replace(out, "comparison.json", result)
    with pytest.raises(EvidenceError, match="comparison_rebuild_mismatch"):
        verify(out)


def test_stop_snapshot_retains_temperature_and_disk_observations(tmp_path, plan):
    run, rows = _data(plan)
    sample = run["resources"][0]
    sample.update(
        phase="stop:r000002",
        disk_free_bytes=1234,
        temperature_samples=[
            {
                "collector": "linux-sensors.v2",
                "server_pid": None,
                "process_start_ticks": None,
                "phase": "engine_fit",
                "request_id": None,
                "metric_name": "temperature",
                "source": "class/thermal/thermal_zone0/temp",
                "unit": "celsius",
                "semantics": "thermal_zone_reported",
                "raw_value": 87000,
                "value": 87.0,
                "missing_reason": None,
                "read_started_ns": 100,
                "read_finished_ns": 200,
            }
        ],
        temperature_missing_reason=None,
    )
    run.update(completeness="incomplete", stop_reason="temperature_safety_threshold_reached")
    seal_run(tmp_path, plan, run, rows)
    assert verify(tmp_path)["run"]["resources"][0]["temperature_samples"][0]["value"] == 87


def test_unicode_surrogates_are_invalid_evidence(tmp_path, plan):
    path = _seal(tmp_path / "run", plan)
    body = (path / "run.json").read_bytes().replace(b'"run-vllm"', b'"\\ud800"')
    _replace(path, "run.json", body)
    with pytest.raises(EvidenceError, match="invalid_json"):
        verify(path)
