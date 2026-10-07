"""Kill actual worker processes at durable boundaries, then rebuild offline."""

import json
import signal
import subprocess
import sys
from pathlib import Path

import pytest

from inferyard.evidence.ledger import read_trial as rebuild
from inferyard.reporting.report import write_report
from tests.helpers import python_worker, readline_timeout

WORKER = r"""
import asyncio, json, sys, time
from dataclasses import asdict
from pathlib import Path
import pytest
from tests.integration.test_runner import scenario
from inferyard.runtime.runner import execute_async
from inferyard.evidence.journal import TrialJournal as EvidenceStore

root, point = Path(sys.argv[1]), sys.argv[2]
patch = pytest.MonkeyPatch()
request, deps, calls, settings = scenario.__wrapped__(
    root, patch, Path('tests/fixtures/config/valid.toml'))

def hold():
    print('ready', flush=True)
    time.sleep(30)

if point == 'plan':
    original = EvidenceStore.snapshot
    def snapshot(self, name, data):
        result = original(self, name, data)
        if name == 'bundle.json': hold()
        return result
    EvidenceStore.snapshot = snapshot
elif point in ('request_started','request_finished','score'):
    original = EvidenceStore.event
    def event(self, kind, phase, key, data, **kwargs):
        result = original(self, kind, phase, key, data, **kwargs)
        if kind == point and ((phase == 'formal') or (kind == 'score')): hold()
        return result
    EvidenceStore.event = event
else:
    factory = deps.adapter
    def adapter(*args, **kwargs):
        instance = factory(*args, **kwargs)
        original_generate = instance.generate
        async def generate(*args, **kwargs):
            if len(calls) == 5:
                print('ready', flush=True)
                await asyncio.sleep(30)
            return await original_generate(*args, **kwargs)
        instance.generate = generate
        return instance
    deps.adapter = adapter
code, result = asyncio.run(execute_async(request, deps))
print(json.dumps({'code': code, 'result': asdict(result)}), flush=True)
"""


@pytest.mark.parametrize(
    "point,state,quality",
    [
        ("plan", "not_executed", "not_scored"),
        ("request_started", "invalid", "not_scored"),
        ("request_finished", "completed", "unscorable"),
        ("score", "completed", "pass"),
    ],
)
def test_sigkill_at_persisted_boundaries_rebuilds_without_service(tmp_path, point, state, quality):
    child = subprocess.Popen(
        python_worker(WORKER, str(tmp_path), point),
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    try:
        assert readline_timeout(child) == "ready"
        child.kill()
        child.wait(timeout=5)
        run = next((tmp_path / "runs").iterdir())
        before = {p.name: p.read_bytes() for p in run.iterdir() if p.is_file()}
        write_report([run], tmp_path / "offline-report")
        data = rebuild(run)
        assert data["requests"][0]["execution_state"] == state
        assert data["requests"][0]["quality_state"] == quality
        assert data["summary"]["completeness"] == "incomplete"
        assert data["summary"]["counts"]["not_executed"] == (3 if point == "plan" else 2)
        assert before == {p.name: p.read_bytes() for p in run.iterdir() if p.is_file()}
    finally:
        if child.poll() is None:
            child.kill()
            child.wait(timeout=5)
        child.stdout.close()
        child.stderr.close()


@pytest.mark.parametrize("sig", [signal.SIGINT, signal.SIGTERM])
@pytest.mark.skipif(sys.platform == "win32", reason="POSIX interprocess signal semantics")
def test_real_signal_returns_130_without_starting_later_cases(tmp_path, sig):
    child = subprocess.Popen(
        python_worker(WORKER, str(tmp_path), "signal"),
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    try:
        assert readline_timeout(child) == "ready"
        child.send_signal(sig)
        stdout, stderr = child.communicate(timeout=8)
        result = json.loads(stdout)
        assert result["code"] == 130, stderr
        data = rebuild(Path(result["result"]["evidence_dir"]))
        assert data["summary"]["counts"]["not_executed"] == 2
        assert data["requests"][0]["execution_state"] == "cancelled"
        assert data["requests"][0]["content"] == ""
        assert data["summary"]["completeness"] == "incomplete"
    finally:
        if child.poll() is None:
            child.kill()
            child.wait(timeout=5)
        child.stdout.close()
        child.stderr.close()
