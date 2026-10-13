"""New native bindings and missing versions remain portable, explicit evidence."""

from copy import deepcopy

import pytest

from inferyard.config.engine_fit import digest
from inferyard.evidence.storage import EvidenceError
from inferyard.reporting.engine_fit import compare, seal_run, verify
from inferyard.reporting.engine_fit_validation import validate_run
from tests.unit.test_engine_fit_report import _data
from tests.unit.test_engine_fit_report import plan as _plan_fixture


def common_data(engine="lmstudio"):
    plan = _plan_fixture.__wrapped__()
    plan.update(definition="engine_fit_plan.v2", engines=["llama-cpp", "lmstudio"])
    plan["host"]["platform"] = "Darwin"
    plan["model"].update(kind="gguf", path="/offline/model.gguf")
    plan["model"]["files"][0]["path"] = "model.gguf"
    plan["model"]["sha256"] = digest(plan["model"]["files"])
    plan["plan_id"] = digest({key: value for key, value in plan.items() if key != "plan_id"})
    run, rows = _data(plan)
    run.update(definition="engine_fit_run.v3", platform="Darwin", engine=engine, run_id=engine)
    run["binding"].update(
        listener_inode=None,
        listener_source="lsof:TCP:LISTEN:pid",
        listener_identity="macos:tcp:127.0.0.1:8000:pid:123",
        cwd_sha256="6" * 64,
    )
    run["binding"]["model_binding"].update(
        engine=engine,
        source="lms_loaded_instance_path"
        if engine == "lmstudio"
        else "verified_startup_file_identity",
    )
    if engine == "lmstudio":
        run["binding"]["observer"] = {
            "kind": "lms",
            "path": "/tools/lms",
            "sha256": "7" * 64,
            "models_root": "/offline",
            "instance_id": "offline",
        }
    metrics = (
        ("lmstudio:queued", "lmstudio:active")
        if engine == "lmstudio"
        else ("llamacpp:requests_processing", "llamacpp:requests_deferred")
    )
    idle = {
        "idle": True,
        "source": "lms:ps" if engine == "lmstudio" else "/metrics",
        "values": [
            {
                "metric": metric,
                "labels": {"instance": "offline"} if engine == "lmstudio" else {},
                "value": 0,
            }
            for metric in metrics
        ],
    }
    run["service"].update(
        engine=engine,
        version=None if engine == "lmstudio" else "b12345-abcdef0",
        version_source="not_exposed" if engine == "lmstudio" else "/props",
        version_missing_reason="lmstudio_service_version_not_exposed"
        if engine == "lmstudio"
        else None,
        idle_before=idle,
    )
    return plan, run, rows


def test_new_gguf_engines_compare_with_missing_version_and_no_live_backend(tmp_path, monkeypatch):
    monkeypatch.setattr("platform.system", lambda: "Windows")
    paths = []
    for engine in ("llama-cpp", "lmstudio"):
        plan, run, rows = common_data(engine)
        path = tmp_path / engine
        path.mkdir()
        seal_run(path, plan, run, rows)
        assert verify(path)["run"] == run
        paths.append(path)
    out = tmp_path / "comparison"
    result = compare(paths, out)
    assert verify(out)["comparison"] == result
    html = (out / "report.html").read_text()
    assert "模型文件" in html and "模型目录" not in html
    assert "缺测 · lmstudio_service_version_not_exposed" in html
    assert "<td>None</td>" not in html


@pytest.mark.parametrize(
    "change", ["version", "reason", "source", "root", "instance", "platform", "legacy", "idle"]
)
def test_rejects_new_evidence_tampering(change):
    plan, run, rows = common_data()
    if change == "version":
        run["service"]["version"] = "cli-commit-is-not-server-version"
    elif change == "reason":
        run["service"]["version_missing_reason"] = None
    elif change == "source":
        run["binding"]["model_binding"]["source"] = "verified_startup_directory_identity"
    elif change == "root":
        run["binding"]["observer"]["models_root"] = "relative/root"
    elif change == "instance":
        run["binding"]["observer"]["instance_id"] = "other-model"
    elif change == "platform":
        run["platform"] = "Linux"
    elif change == "legacy":
        run["definition"] = "engine_fit_run.v2"
        run.pop("platform")
    elif change == "idle":
        run["service"]["idle_before"]["values"][0]["value"] = 1
    with pytest.raises(EvidenceError):
        validate_run(plan, run, rows)


def test_lms_status_for_other_instance_never_becomes_idle():
    plan, run, rows = common_data()
    run["service"]["idle_before"]["values"][0]["labels"]["instance"] = "other"
    with pytest.raises(EvidenceError, match="idle_instance"):
        validate_run(plan, run, rows)


def test_new_asset_cannot_be_compared_under_different_plan(tmp_path):
    paths = []
    for engine in ("llama-cpp", "lmstudio"):
        plan, run, rows = common_data(engine)
        if engine == "lmstudio":
            plan = deepcopy(plan)
            plan["model"]["files"][0]["sha256"] = "8" * 64
            plan["model"]["sha256"] = digest(plan["model"]["files"])
            plan["plan_id"] = digest(
                {key: value for key, value in plan.items() if key != "plan_id"}
            )
            run["plan_id"] = plan["plan_id"]
        path = tmp_path / engine
        path.mkdir()
        seal_run(path, plan, run, rows)
        paths.append(path)
    with pytest.raises(EvidenceError, match="plan_mismatch"):
        compare(paths, tmp_path / "comparison")
