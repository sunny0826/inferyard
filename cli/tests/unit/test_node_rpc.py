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
                "family": "mlx-lm",
                "release_id": "release-1",
                "build_id": "build-1",
                "target_platform": "darwin/arm64",
                "source_url": "https://example.test/engine.zip",
                "wheel_urls": ["https://example.test/engine.whl"],
                "packages": ["mlx-lm==1.0"],
                "python": "3.12",
            },
        )
    )
    assert result["details"] == {"ok": True}
    assert len(requests) == 1
    request = requests[0]
    assert request.engine_source_url == "https://example.test/engine.zip"
    assert request.out == tmp_path / "state" / "workspace" / "engines" / "mlx-lm-release-1"


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
