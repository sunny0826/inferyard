"""Explicit device presentation covers errors, cancellation and application boundaries."""

import json
from copy import deepcopy

import pytest

from inferyard.application.types import CommandRequest, CommandResult
from inferyard.cli import main
from inferyard.platforms import device_preflight
from tests.unit.test_device_preflight import GIB, gguf, hardware, models


@pytest.fixture
def host(monkeypatch):
    value = hardware()
    value.update(
        platform="Linux",
        architecture="x86_64",
        cpu_model="Test CPU",
        memory_total_bytes=16 * GIB,
        disk_path="/output-volume",
    )
    monkeypatch.setattr(device_preflight, "hardware_snapshot", lambda path: deepcopy(value))
    return value


def test_default_human_no_model_is_successful_inspection(host, tmp_path, monkeypatch, capsys):
    monkeypatch.chdir(tmp_path)
    assert main(["device-check"]) == 0
    output = capsys.readouterr()
    assert output.err == "" and output.out.startswith("设备检测：完成")
    assert "Test CPU" in output.out and "16.00 GiB" in output.out
    assert "未发现本地 GGUF" in output.out and "本次未发送模型请求" in output.out
    assert list(tmp_path.iterdir()) == []


@pytest.mark.parametrize("flags", [["--json"], ["--format", "json"], ["--format=json"]])
def test_agent_json_keeps_envelope_and_separate_inspection_status(
    host, tmp_path, monkeypatch, capsys, flags
):
    monkeypatch.chdir(tmp_path)
    assert main(["device-check", *flags]) == 0
    result = json.loads(capsys.readouterr().out)
    assert result["command"] == "device-check" and result["status"] == "inspected"
    report = result["details"]
    assert report["kind"] == "device_preflight.v1" and report["model_requests_sent"] == 0
    assert report["ready_to_run"] is False and report["recommendation"]["selected"] is None
    assert report["recommendation"]["reason"] == "no_local_model"
    assert report["next_steps"][0]["action"] == "provide_local_gguf"


@pytest.mark.parametrize("flags", [[], ["--json"]])
def test_explicit_model_requirement_blocks_and_saves_json(host, tmp_path, capsys, flags):
    destination = tmp_path / "new"
    assert (
        main(
            [
                "device-check",
                "--model",
                str(tmp_path / "missing.gguf"),
                "--out",
                str(destination),
                *flags,
            ]
        )
        == 2
    )
    output = capsys.readouterr()
    report = json.loads((destination / "device-preflight.json").read_text())
    assert report["recommendation"]["reason"] == "invalid_or_unreadable_gguf"
    if flags:
        assert json.loads(output.out)["status"] == "blocked"
    else:
        assert "阻断" in output.out and "GGUF 元数据无效" in output.out


def test_explicit_empty_model_root_blocks(host, tmp_path, capsys):
    assert main(["device-check", "--models-root", str(tmp_path), "--json"]) == 2
    assert json.loads(capsys.readouterr().out)["limitations"] == ["no_local_model"]


def test_output_existing_directory_preserves_original(host, tmp_path, capsys):
    marker = tmp_path / "keep"
    marker.write_bytes(b"original")
    assert main(["device-check", "--out", str(tmp_path), "--json"]) == 4
    output = capsys.readouterr()
    assert json.loads(output.out)["limitations"] == ["io_error"]
    assert marker.read_bytes() == b"original" and list(tmp_path.iterdir()) == [marker]


def test_output_disk_uses_nearest_existing_ancestor_without_precreating(
    host, tmp_path, monkeypatch, capsys
):
    observed = []
    model = gguf(tmp_path / "model.gguf")
    destination = tmp_path / "nested" / "output"

    def snapshot(path):
        observed.append(path)
        assert not destination.parent.exists()
        value = deepcopy(host)
        value["disk_free_bytes"] = GIB
        return value

    monkeypatch.setattr(device_preflight, "hardware_snapshot", snapshot)
    assert main(["device-check", "--model", str(model), "--out", str(destination), "--json"]) == 2
    assert observed == [tmp_path.resolve()]
    report = json.loads(capsys.readouterr().out)["details"]
    assert report["recommendation"]["reason"] == "insufficient_output_disk"
    assert (destination / "device-preflight.json").is_file()


@pytest.mark.parametrize(
    "flags,json_output",
    [
        ([], False),
        (["--format", "human"], False),
        (["--json"], True),
        (["--fo", "json"], True),
        (["--j"], True),
        (["--format", "json", "--format", "human"], False),
    ],
)
def test_invalid_arguments_preserve_presentation_and_never_echo_credentials(
    capsys, flags, json_output
):
    secret = "api-key-private-value"
    assert main(["device-check", *flags, "--invalid", secret]) == 2
    output = capsys.readouterr()
    assert secret not in output.out + output.err
    if json_output:
        assert json.loads(output.out)["command"] == "device-check"
    else:
        assert output.out.startswith("设备检测：阻断")


def test_format_options_are_mutually_exclusive(capsys):
    assert main(["device-check", "--format", "human", "--json"]) == 2
    assert json.loads(capsys.readouterr().out)["limitations"] == ["invalid_input"]


@pytest.mark.parametrize(
    "exception,code,limitation",
    [
        (OSError("secret"), 4, "io_error"),
        (RuntimeError("secret"), 4, "internal_error"),
        (KeyboardInterrupt("secret"), 130, "cancelled"),
    ],
)
@pytest.mark.parametrize("flags", [[], ["--json"]])
def test_errors_and_cancellation_use_selected_format(capsys, exception, code, limitation, flags):
    def handler(request):
        assert isinstance(request, CommandRequest) and not hasattr(request, "device_format")
        raise exception

    assert main(["device-check", *flags], handlers={"device-check": handler}) == code
    output = capsys.readouterr()
    assert "secret" not in output.out + output.err
    if flags:
        assert json.loads(output.out)["limitations"] == [limitation]
    else:
        assert output.out.startswith("设备检测：") and not output.out.startswith("{")


def test_nonfinite_backend_details_map_to_sanitized_error(capsys):
    def handler(request):
        return 0, CommandResult(request.command, "inspected", details={"value": float("nan")})

    assert main(["device-check", "--json"], handlers={"device-check": handler}) == 4
    output = capsys.readouterr()
    assert json.loads(output.out)["limitations"] == ["internal_error"]


def test_mac_capacity_requires_service_binding_and_live_probe(host, tmp_path, capsys):
    host.update(
        platform="Darwin",
        memory_available_is_estimate=True,
        gpu={
            "status": "observed",
            "devices": [
                {"name": "Apple M4", "memory_kind": "shared_host", "metal_supported": True}
            ],
        },
    )
    model = gguf(tmp_path / "model.gguf")
    assert main(["device-check", "--model", str(model)]) == 0
    output = capsys.readouterr().out
    assert "共享主机内存" in output and "可用量为估算" in output
    assert "绑定外部服务，再执行 probe" in output
    assert "检测结果不表示服务已就绪" in output


def test_human_untrusted_model_metadata_and_paths_cannot_inject_terminal_controls(
    host, tmp_path, capsys
):
    name = "unsafe\x1b[2J\n伪造行"
    model = gguf(tmp_path / "model\nfile.gguf", name)
    assert main(["device-check", "--model", str(model)]) == 0
    human = capsys.readouterr().out
    assert "\x1b" not in human and "\n伪造行" not in human
    assert r"unsafe\x1b[2J\n伪造行" in human and r"model\nfile.gguf" in human
    assert main(["device-check", "--model", str(model), "--json"]) == 0
    assert json.loads(capsys.readouterr().out)["details"]["models"][0]["name"] == name


@pytest.mark.parametrize(
    "available,disk,reason",
    [
        (None, 40 * GIB, "host_memory_unavailable"),
        (GIB, 40 * GIB, "insufficient_host_memory"),
        (12 * GIB, None, "output_disk_unavailable"),
        (12 * GIB, GIB, "insufficient_output_disk"),
    ],
)
def test_recommendation_distinguishes_missing_and_insufficient_capacity(available, disk, reason):
    state = hardware()
    state.update(memory_available_bytes=available, disk_free_bytes=disk)
    report = device_preflight.recommend(state, models())
    assert report["status"] == "blocked" and report["reason"] == reason
    assert report["selected"] is None and report["limitations"]
