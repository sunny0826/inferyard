"""Real OS termination of the experiment controller, followed by explicit bounded resume."""

import asyncio
import json
import signal
import subprocess
import sys
from pathlib import Path

import pytest

import inferyard.runtime.lock as locking
from inferyard.analysis.scoring import score_case
from inferyard.application.types import CommandRequest
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import sha256_file
from inferyard.runtime.batch_runner import execute_async
from tests.helpers import readline_timeout
from tests.integration.test_runner import scenario

pytestmark = pytest.mark.skipif(
    sys.platform not in ("linux", "darwin"), reason="Native Linux/macOS controller"
)

WORKER = r"""
import asyncio, json, sys, time
from dataclasses import asdict
from pathlib import Path
import pytest
import inferyard.runtime.batch_runner as running
from inferyard.evidence.journal import TrialJournal
from tests.integration.test_runner import scenario
from tests.integration.test_batch_runner import batch
root, point = Path(sys.argv[1]), sys.argv[2]
patch = pytest.MonkeyPatch()
fixture = scenario.__wrapped__(root, patch, Path('tests/fixtures/config/valid.toml'))
request, deps = batch(root, fixture)
running.TrialDependencies = lambda **kwargs: deps
running.adapter_factory = lambda identifier: deps.adapter
running.collector_factory = lambda identifier: deps.sampler

def hold():
    print('ready', flush=True)
    time.sleep(30)

if point == 'bundle':
    original = TrialJournal.snapshot
    def snapshot(self, name, value):
        result = original(self, name, value)
        if name == 'bundle.json': hold()
        return result
    TrialJournal.snapshot = snapshot
elif point in ('request_started', 'request_finished', 'score'):
    original = TrialJournal.event
    def event(self, kind, phase, key, data, **kwargs):
        result = original(self, kind, phase, key, data, **kwargs)
        if kind == point and phase in ('formal', 'scoring'): hold()
        return result
    TrialJournal.event = event
else:
    factory = deps.adapter
    def adapter(*args, **kwargs):
        instance = factory(*args, **kwargs)
        original_generate = instance.generate
        async def generate(*args, **kwargs):
            if len(fixture[2]) == 5:
                print('ready', flush=True)
                await asyncio.sleep(30)
            return await original_generate(*args, **kwargs)
        instance.generate = generate
        return instance
    deps.adapter = adapter
code, result = running.execute(request)
print(json.dumps({'code': code, 'result': asdict(result)}), flush=True)
"""


def launch(tmp_path, point):
    child = subprocess.Popen(
        [sys.executable, "-c", WORKER, str(tmp_path), point],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    try:
        line = readline_timeout(child, 10)
        assert line == "ready", line
    except BaseException:
        cleanup(child)
        raise
    return child


def cleanup(child):
    if child.poll() is None:
        child.kill()
        child.wait(timeout=5)
    child.stdout.close()
    child.stderr.close()


@pytest.mark.parametrize(
    "point,state,remaining",
    [
        ("bundle", "not_executed", 3),
        ("request_started", "invalid", 2),
        ("request_finished", "completed", 2),
        ("score", "completed", 2),
    ],
)
def test_killed_controller_rebuilds_and_resumes_without_overwriting(
    tmp_path, monkeypatch, point, state, remaining
):
    child = launch(tmp_path, point)
    try:
        child.kill()
        child.wait(timeout=5)
        parent_path = next((tmp_path / "batch/runs").iterdir())
        before = {p.name: sha256_file(p) for p in parent_path.iterdir()}
        parent = read_trial(parent_path)
        assert parent["requests"][0]["execution_state"] == state
        assert not parent["summary"]["evidence_complete"]
        # Synthetic service replacement is explicit; HostLock verifies the actual old worker died.
        fixture = scenario.__wrapped__(
            tmp_path, monkeypatch, Path("tests/fixtures/config/valid.toml")
        )
        deps = fixture[1]
        deps.scorer = score_case
        endpoint = fixture[0].config.config.to_dict()["endpoint"]
        state = locking.read_json(locking.STATE_PATH) if locking.STATE_PATH.exists() else {}
        request = CommandRequest(
            "resume",
            from_run=parent_path,
            endpoint_url=endpoint["url"],
            server_pid=endpoint["server_pid"],
            recovery_confirm=state.get("dirty_token") if state.get("dirty") else None,
            recovery_note="synthetic stopped worker recovery" if state.get("dirty") else None,
        )
        code, result = asyncio.run(execute_async(request, deps))
        assert code == 0, result
        child_data = read_trial(Path(result.details["runs"][-1]))
        assert child_data["summary"]["counts"]["planned"] == remaining
        assert child_data["summary"]["scope_complete"]
        assert child_data["summary"]["completeness"] == "incomplete"
        assert len(fixture[2]) == 5 + remaining
        assert before == {p.name: sha256_file(p) for p in parent_path.iterdir()}
        assert not locking.read_json(locking.STATE_PATH)["dirty"]
    finally:
        cleanup(child)


@pytest.mark.parametrize("sig", [signal.SIGINT, signal.SIGTERM])
@pytest.mark.skipif(sys.platform == "win32", reason="POSIX interprocess signal semantics")
def test_real_signal_stops_controller_and_all_later_trials(tmp_path, sig):
    child = launch(tmp_path, "signal")
    try:
        child.send_signal(sig)
        stdout, stderr = child.communicate(timeout=10)
        result = json.loads(stdout)
        assert result["code"] == 130, stderr
        runs = list((tmp_path / "batch/runs").iterdir())
        assert len(runs) == 1
        data = read_trial(runs[0])
        assert data["summary"]["counts"]["cancelled"] == 1
        assert data["summary"]["counts"]["not_executed"] == 2
        assert data["summary"]["evidence_complete"]
    finally:
        cleanup(child)
