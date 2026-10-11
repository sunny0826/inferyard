"""Bounded MockTransport failures inspect received bodies, durable prefixes and host state."""

import errno
import json
import signal
import subprocess
import sys
from pathlib import Path

import pytest

import inferyard.runtime.lock as locking
import inferyard.runtime.runner as running
from inferyard.cli import main
from inferyard.evidence.journal import TrialJournal
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import EvidenceError, json_bytes, read_json, read_jsonl
from inferyard.evidence.trial_evidence import TrialEvidence
from tests.helpers import python_worker, readline_timeout
from tests.integration.test_lab_cli import lab_scenario, write_config  # noqa: F401
from tests.integration.test_native_cli import native_scenario as native_fixture
from tests.integration.test_runner import scenario as runner_fixture

scenario = runner_fixture
native_scenario = native_fixture


class FailOnceStream:
    def __init__(self, stream, operation):
        self.stream, self.operation, self.failed = stream, operation, False

    def __getattr__(self, name):
        return getattr(self.stream, name)

    def fail(self, operation):
        if self.operation == operation and not self.failed:
            self.failed = True
            raise OSError(errno.ENOSPC, "synthetic storage boundary")

    def write(self, value):
        self.fail("write")
        return self.stream.write(value)

    def flush(self):
        self.fail("flush")
        return self.stream.flush()

    def close(self):
        self.stream.close()
        self.fail("close")


def record(tmp_path, label, code, root, received):
    events, tails = read_jsonl(root / "events.jsonl")
    prepared, body_tails = read_jsonl(root / "request-bodies.jsonl")
    assert not tails and not body_tails
    value = {
        "synthetic": True,
        "transport": "httpx.MockTransport; no TCP/model service",
        "preflight": "injected fixture; no native qualification",
        "fault": label,
        "cli_exit": code if code >= 0 else None,
        "subprocess_exit": code if code < 0 else None,
        "received": received,
        "received_count": len(received),
        "prepared": prepared,
        "events": events,
        "host_state": read_json(locking.STATE_PATH),
        "files": sorted(path.name for path in root.iterdir()),
        "original_run": str(root),
    }
    (tmp_path / "fault-receipt.json").write_bytes(json_bytes(value))
    return value


def cli_run(command, deps, tmp_path, monkeypatch, capsys):
    monkeypatch.setattr(running, "Dependencies", lambda **options: deps)
    config = tmp_path / "fault.toml"
    write_config(command, config)
    code = main(["run", "--config", str(config)])
    captured = capsys.readouterr()
    (tmp_path / "cli.stdout.txt").write_text(captured.out)
    (tmp_path / "cli.stderr.txt").write_text(captured.err)
    value = json.loads(captured.out)
    root = value.get("evidence_dir")
    if root is None:
        root = next(Path(command.config.config.to_dict()["output"]["root"]).iterdir())
    return code, value, Path(root)


@pytest.mark.parametrize("operation", ["write", "flush", "fsync"])
def test_prepared_io_failure_does_not_send_formal_request(
    scenario, tmp_path, monkeypatch, capsys, operation
):
    import inferyard.evidence.storage as storage

    command, deps, received, _ = scenario
    original = TrialJournal.prepare_request
    fsync = storage.os.fsync

    def prepare(self, request_id, phase, ordinal, body):
        if phase != "formal":
            return original(self, request_id, phase, ordinal, body)
        stream = self._logs["request-bodies.jsonl"]
        wrapped = FailOnceStream(stream, operation)
        self._logs["request-bodies.jsonl"] = wrapped

        def fail_sync(fd):
            if fd == stream.fileno():
                wrapped.fail("fsync")
            return fsync(fd)

        monkeypatch.setattr(storage.os, "fsync", fail_sync)
        try:
            return original(self, request_id, phase, ordinal, body)
        finally:
            self._logs["request-bodies.jsonl"] = stream
            monkeypatch.setattr(storage.os, "fsync", fsync)

    monkeypatch.setattr(TrialJournal, "prepare_request", prepare)
    code, result, root = cli_run(command, deps, tmp_path, monkeypatch, capsys)
    assert code == 4, result
    receipt = record(tmp_path, "prepared-" + operation, code, root, received)
    assert len(received) == 5
    assert not any(
        event["phase"] == "formal"
        for event in receipt["events"]
        if event["event_type"] == "request_started"
    )
    assert not receipt["host_state"]["dirty"]
    data = read_trial(root)
    assert data["summary"]["counts"]["not_executed"] == data["summary"]["counts"]["planned"] == 3
    assert data["summary"]["counts"]["failed"] == 0


def test_started_write_failure_keeps_dirty_without_delivery(
    scenario, tmp_path, monkeypatch, capsys
):
    command, deps, received, _ = scenario
    original = TrialJournal.event

    def event(self, kind, phase, key, data, **options):
        if kind == "request_started" and phase == "formal":
            raise EvidenceError("synthetic_started_write_failed")
        return original(self, kind, phase, key, data, **options)

    monkeypatch.setattr(TrialJournal, "event", event)
    code, result, root = cli_run(command, deps, tmp_path, monkeypatch, capsys)
    assert code == 4, result
    receipt = record(tmp_path, "started-write", code, root, received)
    assert len(received) == 5 and receipt["host_state"]["dirty"]
    assert len(receipt["prepared"]) == 6
    assert read_trial(root)["summary"]["counts"]["not_executed"] == 3


def test_native_receipt_failure_keeps_client_dirty(native_scenario, tmp_path, monkeypatch, capsys):
    command, deps, service = native_scenario
    original = TrialJournal.observe

    def observe(self, kind, payload, **options):
        if kind == "native_clean_receipt":
            raise EvidenceError("synthetic_receipt_write_failed")
        return original(self, kind, payload, **options)

    monkeypatch.setattr(TrialJournal, "observe", observe)
    code, result, root = cli_run(command, deps, tmp_path, monkeypatch, capsys)
    assert code == 4, result
    receipt = record(tmp_path, "native-clean-receipt", code, root, service.requests)
    assert len(service.requests) == 1 and receipt["host_state"]["dirty"]
    assert TrialEvidence(root).observation_keys("native_clean_receipt") == []
    # Missing required layout2 receipts never earn full verification.
    with pytest.raises(EvidenceError, match="native_clean_receipt"):
        read_trial(root)


@pytest.mark.parametrize("boundary", ["sampling", "close", "seal"])
def test_finalization_failures_keep_real_received_and_readable_prefix(
    scenario, tmp_path, monkeypatch, capsys, boundary
):
    import inferyard.evidence.storage as storage

    command, deps, received, settings = scenario
    if boundary == "sampling":
        original = TrialJournal.sample

        def sample(self, value):
            if value["phase"] == "formal":
                raise EvidenceError("synthetic_sampling_io_failed")
            return original(self, value)

        settings["stream_delay"] = 0.06
        monkeypatch.setattr(TrialJournal, "sample", sample)
    elif boundary == "seal":
        publish = storage.publish

        def fail_publish(source, destination, overwrite):
            if destination.name == "manifest.json":
                raise OSError(errno.ENOSPC, "synthetic manifest publication failure")
            return publish(source, destination, overwrite)

        monkeypatch.setattr(storage, "publish", fail_publish)
    else:
        seal = TrialJournal.seal

        def seal_then_close_failure(self, *args, **kwargs):
            result = seal(self, *args, **kwargs)
            stream = self._logs["memory.jsonl"]
            self._logs["memory.jsonl"] = FailOnceStream(stream, "close")
            return result

        monkeypatch.setattr(TrialJournal, "seal", seal_then_close_failure)
    code, result, root = cli_run(command, deps, tmp_path, monkeypatch, capsys)
    assert code == 4, result
    receipt = record(tmp_path, boundary, code, root, received)
    data = read_trial(root)
    assert data["summary"]["counts"]["planned"] == 3
    assert receipt["host_state"]["dirty"] == (boundary == "sampling")
    assert len(receipt["prepared"]) == len(received)
    if boundary == "sampling":
        assert 5 < len(received) < 8
        assert data["summary"]["counts"]["not_executed"] > 0
        assert data["summary"]["stop_reason"] == "sampler_failed"
        assert read_jsonl(root / "memory.jsonl")[0]
    else:
        assert len(received) == 8
        assert data["summary"]["counts"]["completed"] == 3
        assert (root / "manifest.json").is_file() == (boundary == "close")


INTERRUPTED_CLI = r"""
import json, sys, time, asyncio
from pathlib import Path
import pytest
import inferyard.runtime.runner as running
from inferyard.cli import main
from inferyard.evidence.journal import TrialJournal
from tests.integration.test_runner import scenario
from tests.integration.test_lab_cli import write_config
root, boundary = Path(sys.argv[1]), sys.argv[2]
patch = pytest.MonkeyPatch()
command, deps, calls, settings = scenario.__wrapped__(
    root, patch, Path('tests/fixtures/config/valid.toml'))
running.Dependencies = lambda **options: deps
config = root / 'fault.toml'
write_config(command, config)
def hold():
    (root / 'received.json').write_text(json.dumps(calls))
    print('ready', flush=True)
    time.sleep(30)
if boundary == 'crash':
    original = TrialJournal.event
    def event(self, kind, phase, key, data, **options):
        value = original(self, kind, phase, key, data, **options)
        if kind == 'request_started' and phase == 'formal': hold()
        return value
    TrialJournal.event = event
else:
    factory = deps.adapter
    def adapter(*args, **kwargs):
        instance = factory(*args, **kwargs)
        generate = instance.generate
        async def stopped(*args, **kwargs):
            if len(calls) == 5:
                (root / 'received.json').write_text(json.dumps(calls))
                print('ready', flush=True)
                await asyncio.sleep(30)
            return await generate(*args, **kwargs)
        instance.generate = stopped
        return instance
    deps.adapter = adapter
code = main(['run', '--config', str(config)])
print(json.dumps({'actual_cli_exit': code}), flush=True)
"""


@pytest.mark.parametrize("boundary", ["crash", "cancel"])
@pytest.mark.skipif(sys.platform == "win32", reason="POSIX process signals")
def test_actual_cli_interruption_keeps_dirty_and_durable_prefix(tmp_path, monkeypatch, boundary):
    monkeypatch.setattr(locking, "STATE_PATH", tmp_path / "host.state.json")
    child = subprocess.Popen(
        python_worker(INTERRUPTED_CLI, str(tmp_path), boundary),
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    try:
        assert readline_timeout(child) == "ready"
        if boundary == "crash":
            child.kill()
        else:
            child.send_signal(signal.SIGINT)
        stdout, stderr = child.communicate(timeout=8)
        (tmp_path / "cli.stdout.txt").write_text(stdout)
        (tmp_path / "cli.stderr.txt").write_text(stderr)
        assert child.returncode == (-signal.SIGKILL if boundary == "crash" else 0), stderr
        if boundary == "cancel":
            assert json.loads(stdout.splitlines()[-1])["actual_cli_exit"] == 130
        root = next((tmp_path / "runs").iterdir())
        received = read_json(tmp_path / "received.json")
        code = child.returncode if boundary == "crash" else 130
        receipt = record(tmp_path, boundary, code, root, received)
        assert receipt["host_state"]["dirty"] == (boundary == "crash")
        assert len(received) == 5
        assert len(receipt["prepared"]) == 6
        data = read_trial(root)
        assert data["summary"]["counts"]["not_executed"] == 2
        assert data["requests"][0]["execution_state"] == (
            "invalid" if boundary == "crash" else "cancelled"
        )
        assert data["summary"]["completeness"] == "incomplete"
    finally:
        if child.poll() is None:
            child.kill()
            child.wait(timeout=5)
        child.stdout.close()
        child.stderr.close()
