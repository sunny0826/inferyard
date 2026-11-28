import asyncio
from types import SimpleNamespace

from inferyard.application.types import CommandResult
from inferyard.node.rpc import NodeRPC


def test_go_engine_install_aliases_and_default_output(tmp_path, monkeypatch):
    requests = []

    def execute(request):
        requests.append(request)
        return 0, CommandResult("engine prepare", "prepared", "complete", details={"ok": True})

    monkeypatch.setattr("inferyard.application.engine_prepare.execute", execute)
    rpc = NodeRPC(tmp_path / "state", None, None)
    result = asyncio.run(
        rpc.call(
            "node.engine_install",
            {
                "family": "llama-cpp",
                "release_id": "release-1",
                "build_id": "build-1",
                "install_mode": "direct_download",
                "target_platform": "darwin/arm64",
                "source_url": "https://example.test/engine.zip",
            },
        )
    )
    assert result["details"] == {"ok": True}
    assert len(requests) == 1
    request = requests[0]
    assert request.engine_source_url == "https://example.test/engine.zip"
    assert request.out == tmp_path / "state" / "workspace" / "engines" / "llama-cpp-release-1"


def test_go_engine_install_managed_venv_uses_uv_and_init(tmp_path, monkeypatch):
    calls = []
    init_requests = []

    monkeypatch.setattr("shutil.which", lambda name: "/usr/bin/uv" if name == "uv" else None)
    monkeypatch.setattr(
        "subprocess.run", lambda argv, **kwargs: calls.append(argv) or SimpleNamespace(returncode=0)
    )
    monkeypatch.setattr(
        "inferyard.application.engine_detect.execute",
        lambda req: init_requests.append(req)
        or (0, CommandResult("engine init-installation", "prepared", "complete")),
    )
    rpc = NodeRPC(tmp_path / "state", None, None)
    result = asyncio.run(
        rpc.call(
            "node.engine_install",
            {
                "family": "mlx-lm",
                "release_id": "release-1",
                "build_id": "build-1",
                "install_mode": "managed_venv",
                "target_platform": "darwin/arm64",
                "wheel_urls": ["https://example.test/mlx.whl"],
                "packages": ["mlx-lm==1.0"],
                "python": "3.12",
            },
        )
    )
    assert result["install_mode"] == "managed_venv"
    assert len(calls) == 2 and calls[0][1] == "venv" and calls[1][1:3] == ["pip", "install"]
    assert "https://example.test/mlx.whl" in calls[1]
    assert len(init_requests) == 1 and init_requests[0].family == "mlx-lm"


def test_report_export_by_job_id_resolves_raw_run(tmp_path):
    from tests.helpers import fixture_run

    state = tmp_path / "state"
    run = fixture_run(tmp_path / "raw-run")
    jobs = SimpleNamespace(jobs={"job-42": {"job_id": "job-42", "evidence_dir": str(run)}})
    rpc = NodeRPC(state, None, jobs)
    result = asyncio.run(rpc.call("report.export", {"remote_report_id": "job-42"}))
    assert result["verified_from_raw"] is True
    assert result["report"]["schema_version"] == 3


def test_report_export_by_batch_job_id_resolves_nested_runs(tmp_path):
    from tests.helpers import fixture_run

    state = tmp_path / "state"
    batch_root = tmp_path / "batch-1"
    run_a = fixture_run(batch_root / "runs" / "run-a")
    run_b = fixture_run(batch_root / "runs" / "run-b")
    jobs = SimpleNamespace(
        jobs={
            "batch-job": {
                "job_id": "batch-job",
                "evidence_dir": str(batch_root),
                "result": {"details": {"runs": [str(run_a), str(run_b)]}},
            }
        }
    )
    rpc = NodeRPC(state, None, jobs)
    result = asyncio.run(rpc.call("report.export", {"remote_report_id": "batch-job"}))
    assert result["verified_from_raw"] is True
    assert len(result["report"]["runs"]) == 2


def test_find_uv_discovers_private_inferyard_install_dir(tmp_path, monkeypatch):
    import os

    uv_dir = tmp_path / "private-uv"
    uv_dir.mkdir()
    uv_bin = uv_dir / ("uv.exe" if os.name == "nt" else "uv")
    uv_bin.write_text("#!/bin/sh\n")
    uv_bin.chmod(0o755)
    monkeypatch.setenv("INFERYARD_UV_DIR", str(uv_dir))
    monkeypatch.setattr("shutil.which", lambda name: None)
    rpc = NodeRPC(tmp_path / "state", None, None)
    assert rpc._find_uv() == str(uv_bin)


def test_empty_workspace_clean_preserves_assets_and_active_jobs(tmp_path):
    state = tmp_path / "state"
    jobs = SimpleNamespace(
        list=lambda: [
            {"job_id": "done", "state": "done"},
            {"job_id": "active", "state": "running"},
        ]
    )
    rpc = NodeRPC(state, None, jobs)

    def put(path):
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("artifact")

    put(state / "jobs" / "done" / "results" / "run" / "manifest.json")
    put(state / "jobs" / "done" / "job.json")
    put(state / "jobs" / "active" / "results" / "run" / "manifest.json")
    put(state / "reports" / "export-one" / "studio.json")
    put(state / "reports" / "curated" / "studio.json")
    for name in ("candidates", "bound", "results", "reports"):
        put(rpc.workspace / name / "artifact.txt")
    put(rpc.workspace / "models" / "model.gguf")
    put(rpc.workspace / "engines" / "engine" / "installation.json")

    result = asyncio.run(rpc.call("workspace.clean", {}))
    assert result["removed"]
    assert not (state / "jobs" / "done" / "results").exists()
    assert (state / "jobs" / "done" / "job.json").is_file()
    assert (state / "jobs" / "active" / "results" / "run" / "manifest.json").is_file()
    assert not (state / "reports" / "export-one").exists()
    assert (state / "reports" / "curated" / "studio.json").is_file()
    for name in ("candidates", "bound", "results", "reports"):
        assert (rpc.workspace / name).is_dir()
        assert list((rpc.workspace / name).iterdir()) == []
    assert (rpc.workspace / "models" / "model.gguf").is_file()
    assert (rpc.workspace / "engines" / "engine" / "installation.json").is_file()


def test_service_stop_is_blocked_while_benchmark_job_is_active(tmp_path):
    import pytest
    from inferyard.node.rpc import RPCError

    stopped = []
    supervisor = SimpleNamespace(stop=lambda sid, timeout=5: stopped.append(sid))
    jobs = SimpleNamespace(has_active_jobs=lambda: True)
    rpc = NodeRPC(tmp_path / "state", supervisor, jobs)
    with pytest.raises(RPCError) as caught:
        asyncio.run(rpc.call("service.stop", {"service_id": "svc-1"}))
    assert caught.value.code == "run_active"
    assert stopped == []
