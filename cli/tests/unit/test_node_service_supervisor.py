import asyncio
import json
import sys

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
                },
            }
        )
        await asyncio.sleep(.4)
        saved = supervisor._record("crash")
        assert saved["status"] == "exited"
        assert saved["exit_code"] == 0
        assert "bye" in saved["log_tail"]
        assert supervisor.processes["crash"].pid == record["pid"]
        assert len(supervisor.processes) == 1
        assert ("service.exited", "exited") in events
        receipt = json.loads((tmp_path / "services" / "crash" / "receipt.json").read_text())
        assert receipt["status"] == "exited"
        await supervisor.close()

    run(scenario())


def test_managed_port_and_stop_identity(tmp_path):
    async def scenario():
        supervisor = ServiceSupervisor(tmp_path)
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
        assert supervisor.allows_port(48857, "managed")
        assert not supervisor.allows_port(48857, "other")
        assert not supervisor.allows_port(49999, "managed")
        stopped = await supervisor.stop("managed", timeout=.1)
        assert stopped["status"] == "stopped"
        assert not supervisor.allows_port(48857, "managed")
        await supervisor.close()

    run(scenario())
