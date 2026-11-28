import asyncio
import json
import sys
from datetime import datetime

from inferyard.node.service_supervisor import ServiceSupervisor


def run(coroutine):
    return asyncio.run(coroutine)


def test_crash_records_exit_and_never_restarts(tmp_path):
    async def scenario():
        events = []

        async def event(name, data):
            events.append((name, data["status"]))

        supervisor = ServiceSupervisor(tmp_path, event_sink=event)
        record = await supervisor.start(
            {
                "service_id": "crash",
                "plan": {
                    "argv": [sys.executable, "-c", "print('bye')"],
                    "environment": {},
                    "working_directory": str(tmp_path),
                    "endpoint": "http://127.0.0.1:48857",
                    "model_path": "model.gguf",
                    "family": "llama-cpp",
                    "context_size": 8192,
                },
            }
        )
        datetime.fromisoformat(record["started_at"])
        assert record["state"] == record["status"] == "running"
        assert record["endpoint_url"] == record["endpoint"] == "http://127.0.0.1:48857"
        assert record["model"] == "model.gguf"
        assert record["engine"] == "llama-cpp"
        for _ in range(100):
            if supervisor._record("crash")["status"] == "exited":
                break
            await asyncio.sleep(0.05)
        saved = supervisor._record("crash")
        assert saved["state"] == saved["status"] == "exited"
        assert saved["exit_code"] == 0
        assert saved["log_tail"] == ["bye"]
        assert saved["crashed"] is True
        assert supervisor.logs("crash") == {"service_id": "crash", "logs": "bye\n"}
        assert supervisor.processes["crash"].pid == record["pid"]
        assert len(supervisor.processes) == 1
        assert ("service.exited", "exited") in events
        receipt = json.loads((tmp_path / "services" / "crash" / "receipt.json").read_text())
        assert receipt["status"] == "exited"
        await supervisor.close()

    run(scenario())


def test_managed_port_and_stop_identity(tmp_path):
    async def scenario():
        events = []

        async def sink(name, data):
            events.append((name, data["status"]))

        supervisor = ServiceSupervisor(tmp_path, event_sink=sink)
        record = await supervisor.start(
            {
                "service_id": "managed",
                "plan": {
                    "argv": [sys.executable, "-c", "import time; time.sleep(60)"],
                    "environment": {},
                    "working_directory": str(tmp_path),
                    "endpoint": "http://127.0.0.1:48857",
                },
            }
        )
        assert record["status"] == "running"
        assert supervisor.allows_port(48857, "managed")
        assert not supervisor.allows_port(48857, "other")
        assert not supervisor.allows_port(49999, "managed")
        stopped = await supervisor.stop("managed", timeout=.1)
        assert stopped["status"] == "stopped"
        assert not supervisor.allows_port(48857, "managed")
        assert all(name != "service.exited" for name, _ in events)
        await supervisor.close()

    run(scenario())


def test_go_service_aliases_and_default_context(tmp_path, monkeypatch):
    requests = []

    def prepare(request):
        requests.append(request)
        return {"endpoint": "http://127.0.0.1:48857"}

    monkeypatch.setattr("inferyard.config.serve_plan.prepare", prepare)
    supervisor = ServiceSupervisor(tmp_path)
    service_dir = tmp_path / "services" / "aliases"
    service_dir.mkdir()
    installation = tmp_path / "engine-installation"
    installation.mkdir()
    supervisor._prepare_plan(
        {"model": "model.gguf", "engine": str(installation)}, service_dir, 48857
    )
    supervisor._prepare_plan(
        {"model_path": "model.gguf", "engine_path": "llama-server", "context_size": 4096},
        service_dir,
        48857,
    )
    assert requests[0].model_path.name == "model.gguf"
    assert requests[0].installation == installation
    assert requests[0].engine_path is None
    assert requests[0].context_size == 8192
    assert requests[1].installation is None
    assert requests[1].engine_path.name == "llama-server"
    assert requests[1].context_size == 4096


def test_stale_running_receipt_is_sealed_and_rejects_tunnel(tmp_path, monkeypatch):
    service_dir = tmp_path / "services" / "stale"
    service_dir.mkdir(parents=True)
    record = {
        "service_id": "stale",
        "pid": 4242,
        "start_ticks": 111,
        "port": 48857,
        "log_path": str(service_dir / "engine.log"),
        "status": "running",
    }
    (service_dir / "receipt.json").write_text(json.dumps(record))
    monkeypatch.setattr("inferyard.node.service_supervisor.process_start_ticks", lambda pid: 222)
    supervisor = ServiceSupervisor(tmp_path)
    assert supervisor._record("stale")["status"] == "exited"
    assert supervisor._record("stale")["state"] == "exited"
    assert not supervisor.allows_port(48857, "stale")
    saved = json.loads((service_dir / "receipt.json").read_text())
    assert saved["status"] == "exited"
