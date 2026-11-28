"""Offline provenance, semantic tampering and comparison integrity boundaries."""

import hashlib
import json
from copy import deepcopy

import pytest

from inferyard.analysis.engine_fit_seal import seal_run
from inferyard.config.engine_fit import request_rows
from inferyard.contracts.json_digest import digest
from inferyard.contracts.validation import json_bytes
from inferyard.errors import EvidenceError
from inferyard.reporting.engine_fit import compare, verify


@pytest.fixture
def plan():
    files = [{"path": "weights.safetensors", "sha256": "1" * 64, "size": 1024}]
    value = {
        "schema_version": 3,
        "definition": "engine_fit_plan.v1",
        "model": {"path": "/offline/model", "sha256": digest(files), "files": files},
        "host": {"sha256": "2" * 64, "system": "Linux"},
        "engines": ["vllm"],
        "cases": [{"id": "case", "prompt": "<script>alert('prompt')</script> & 测试"}],
        "parameters": {
            "max_tokens": 32,
            "temperature": 0,
            "repetitions": 2,
            "request_timeout_seconds": 30,
            "min_free_memory_bytes": 128 * 2**20,
            "min_free_disk_bytes": 256 * 2**20,
        },
        "request_count": 2,
        "max_request_wall_seconds": 60,
    }
    value["plan_id"] = digest(value)
    return value


def _idle(engine):
    if engine == "mlx-lm":
        names = [
            "engine_fit:num_requests_running",
            "engine_fit:num_requests_waiting",
            "engine_fit:poisoned",
        ]
        prefix = ""
    else:
        names = ["num_requests_running", "num_requests_waiting"]
        prefix = engine + ":"
    return {
        "idle": True,
        "source": "/metrics",
        "values": [
            {"metric": prefix + name, "labels": {"model": "offline"}, "value": 0.0}
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
            "version_source": "/version",
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


def verify_fixture_plan(plan):
    plan["plan_id"] = digest({key: value for key, value in plan.items() if key != "plan_id"})
    return plan


def _seal_modern_darwin(path, plan, engine):
    run, rows = _data(plan, engine)
    run["definition"] = "engine_fit_run.v3"
    run["platform"] = "Darwin"
    run["binding"].update(
        listener_inode=None,
        listener_identity="macos:tcp:127.0.0.1:8000:pid:123",
        listener_source="lsof:TCP:LISTEN:pid",
        cwd_sha256="6" * 64,
    )
    run["service"]["version_missing_reason"] = None
    path.mkdir()
    seal_run(path, plan, run, rows)
    return path


def test_compare_rejects_duplicates_plans_sources_and_existing_out(tmp_path, plan):
    modern = deepcopy(plan)
    modern["definition"] = "engine_fit_plan.v2"
    modern["model"].update(definition="model-assets.v2", kind="directory")
    modern["host"] = {"sha256": "2" * 64, "platform": "Darwin"}
    modern["engines"] = ["vllm", "mlx-lm"]
    verify_fixture_plan(modern)

    left = _seal_modern_darwin(tmp_path / "left", modern, "vllm")
    right = _seal_modern_darwin(tmp_path / "right", modern, "mlx-lm")
    with pytest.raises(EvidenceError, match="duplicate_engine"):
        compare([left, left], tmp_path / "dup")
    with pytest.raises(EvidenceError, match="output_exists"):
        compare([left, right], tmp_path)
    changed = deepcopy(modern)
    changed["host"]["sha256"] = "9" * 64
    verify_fixture_plan(changed)
    other = _seal_modern_darwin(tmp_path / "other", changed, "mlx-lm")
    with pytest.raises(EvidenceError, match="comparison_plan_mismatch"):
        compare([left, other], tmp_path / "mismatch")
    out = tmp_path / "comparison"
    result = compare([left, right], out)
    result["source_runs"][0]["path"] = str(left)
    _replace(out, "comparison.json", result)
    with pytest.raises(EvidenceError, match="unsafe_source_reference"):
        verify(out)
