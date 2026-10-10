"""F01 command dispatch tests: injected handlers, not inference acceptance."""

import json
import socket
import subprocess
import sys
from pathlib import Path

import pytest

from inferyard.application.types import CommandResult
from inferyard.cli import main


@pytest.fixture(autouse=True)
def no_network(monkeypatch):
    # Both Windows loops create an internal loopback socket pair. Permit only
    # this stdlib internal operation while prohibiting application connections.
    if sys.platform == "win32":
        pair, connect = socket.socketpair, socket.socket.connect

        def internal_pair(*args, **kwargs):
            with monkeypatch.context() as patch:
                patch.setattr(socket.socket, "connect", connect)
                return pair(*args, **kwargs)

        monkeypatch.setattr(socket, "socketpair", internal_pair)

    def fail(*args, **kwargs):
        raise AssertionError("T01 must not access the network")

    monkeypatch.setattr(socket, "create_connection", fail)
    monkeypatch.setattr(socket, "getaddrinfo", fail)
    monkeypatch.setattr(socket.socket, "connect", fail)


def test_valid_configuration_enters_injected_preflight(config_path, capsys):
    calls = []

    def preflight(request):
        calls.append(request)
        return 2, CommandResult("probe", "blocked", limitations=("fixture_preflight",))

    assert main(["probe", "--config", str(config_path)], handlers={"probe": preflight}) == 2
    output = json.loads(capsys.readouterr().out)
    assert len(calls) == 1
    assert calls[0].config.bundle.to_dict()["bundle_id"] == "t01-fixture"
    assert output["limitations"] == ["fixture_preflight"]


@pytest.mark.parametrize(
    "fixture",
    [
        "unknown-field.toml",
        "concurrency.toml",
        "negative-timeout.toml",
        "plaintext-key.toml",
        "duplicate-case.toml",
    ],
)
def test_invalid_configuration_never_dispatches(config_path, fixture, capsys):
    calls = []
    code = main(
        ["probe", "--config", str(config_path.with_name(fixture))],
        handlers={"probe": lambda request: calls.append(request)},
    )
    output = capsys.readouterr()
    assert code == 2
    assert calls == []
    assert json.loads(output.out)["status"] == "blocked"
    assert "fixture-secret-7f39" not in output.out + output.err
    assert output.err


@pytest.mark.parametrize(
    "arguments",
    [
        [
            "run",
            "--rerun-from",
            "/unused",
            "--endpoint-url",
            "http://127.0.0.1:8080",
            "--server-pid",
            "12345",
        ],
    ],
)
def test_missing_rerun_is_blocked_without_writing(arguments, capsys):
    assert main(arguments) == 2
    result = json.loads(capsys.readouterr().out)
    assert result["completeness"] == "incomplete"
    assert result["evidence_dir"] is None
    assert "rerun_requires_sealed_source" in result["limitations"]


def test_missing_credentials_do_not_claim_benchmark_success(config_path, capsys):
    assert main(["run", "--config", str(config_path)]) == 2
    result = json.loads(capsys.readouterr().out)
    assert result["status"] == "blocked"
    assert result["run_id"] is None


@pytest.mark.parametrize(
    "arguments",
    [
        [],
        ["unknown", "fixture-secret-7f39"],
        ["run", "--rerun-from", "old"],
        ["run", "--rerun-from", "old", "--endpoint-url", "http://localhost", "--server-pid", "0"],
        ["report", "--runs", "old"],
        ["--versions", "report", "--run", "old", "--out", "new"],
        ["probe", "--config", "unused", "--recovery-confirm", "fixture-secret-7f39"],
    ],
)
def test_argument_errors_emit_one_json_and_no_argument_values(arguments, capsys):
    assert main(arguments) == 2
    output = capsys.readouterr()
    assert json.loads(output.out)["status"] == "blocked"
    assert "fixture-secret-7f39" not in output.out + output.err


def test_dependency_versions_export_is_json(capsys):
    assert main(["--versions"]) == 0
    data = json.loads(capsys.readouterr().out)
    assert data["schema_version"] == 3
    assert data["python"] == "3.14.7"
    assert data["dependencies"]["httpx"]
    assert "environment" not in data


def test_schema_export(capsys):
    assert main(["--schema", "sample"]) == 0
    data = json.loads(capsys.readouterr().out)
    assert all(item["additionalProperties"] is False for item in data["oneOf"])


def test_io_errors_and_internal_exception_messages_are_sanitized(config_path, tmp_path, capsys):
    assert main(["probe", "--config", str(tmp_path / "fixture-secret-7f39")]) == 4
    first = capsys.readouterr()
    assert "fixture-secret-7f39" not in first.out + first.err

    def broken(_):
        raise RuntimeError("fixture-secret-7f39")

    assert main(["probe", "--config", str(config_path)], handlers={"probe": broken}) == 4
    second = capsys.readouterr()
    assert "fixture-secret-7f39" not in second.out + second.err
    assert json.loads(second.out)["limitations"] == ["internal_error"]


def test_installed_module_command_discovery():
    completed = subprocess.run(
        [sys.executable, "-m", "inferyard", "--help"],
        capture_output=True,
        text=True,
        check=True,
    )
    for name in ("probe", "run", "report", "compare"):
        assert name in completed.stdout
    assert completed.stderr == ""


def test_exported_schema_files_match_source():
    root = Path(__file__).resolve().parents[2]
    subprocess.run([sys.executable, str(root / "scripts/export_schemas.py"), "--check"], check=True)


def test_real_offline_commands_need_no_network(tmp_path, capsys):
    from tests.helpers import fixture_run

    a = fixture_run(tmp_path / "runs")
    b = fixture_run(tmp_path / "runs", model="synthetic-B")
    assert main(["report", "--runs", str(a), "--out", str(tmp_path / "single")]) == 0
    assert json.loads(capsys.readouterr().out)["status"] == "rendered"
    assert (
        main(["compare", "--left", str(a), "--right", str(b), "--out", str(tmp_path / "pair")]) == 0
    )
    assert json.loads(capsys.readouterr().out)["status"] == "compared"
    assert (tmp_path / "pair/report.html").is_file()


def test_report_recovers_tail_and_rejects_middle_corruption(tmp_path, capsys):
    from tests.helpers import fixture_run

    root = fixture_run(tmp_path / "runs")
    (root / "manifest.json").unlink()
    events = root / "events.jsonl"
    with events.open("ab") as stream:
        stream.write(b'{"seq":')
    assert main(["report", "--runs", str(root), "--out", str(tmp_path / "partial")]) == 0
    output = json.loads(capsys.readouterr().out)
    index = json.loads((tmp_path / "partial/index.json").read_text())
    assert output["status"] == "rendered"
    summary = index["runs"][0]["summary"]
    assert summary["completeness"] == "incomplete"
    assert any("truncated_tail" in reason for reason in summary["limitations"])
    events.write_bytes(events.read_bytes() + b"\nbroken\n")
    assert main(["report", "--runs", str(root), "--out", str(tmp_path / "corrupt")]) == 4
    assert json.loads(capsys.readouterr().out)["limitations"] == ["corrupt_jsonl_evidence"]
    assert not (tmp_path / "corrupt").exists()


def test_readonly_evidence_with_damaged_derived_files_rebuilds(tmp_path, capsys):
    from tests.helpers import fixture_run

    # Add derived files before sealing; the fixture helper intentionally omits them.
    root = fixture_run(tmp_path / "runs")
    manifest = json.loads((root / "manifest.json").read_text())
    import hashlib

    for name in ("summary.json", "report.html"):
        original = b"original derived placeholder"
        manifest["files"][name] = {
            "sha256": hashlib.sha256(original).hexdigest(),
            "bytes": len(original),
            "derived": True,
        }
        (root / name).write_bytes(b"damaged")
    (root / "manifest.json").write_text(json.dumps(manifest))
    before = {p.name: p.read_bytes() for p in root.iterdir()}
    for path in root.iterdir():
        path.chmod(0o400)
    root.chmod(0o500)
    try:
        assert main(["report", "--runs", str(root), "--out", str(tmp_path / "new-report")]) == 0
        result = json.loads(capsys.readouterr().out)
        index = json.loads((tmp_path / "new-report/index.json").read_text())
        assert "derived_evidence_damaged:summary.json" in index["runs"][0]["summary"]["limitations"]
        assert result["status"] == "rendered"
        assert before == {p.name: p.read_bytes() for p in root.iterdir()}
    finally:
        root.chmod(0o700)
        for path in root.iterdir():
            path.chmod(0o600)
