"""Verify custom --port propagation and startup condition fields in config create."""

import json
from dataclasses import replace
from pathlib import Path

import pytest

from inferyard.application.types import CommandRequest
from inferyard.cli import main
from inferyard.config import community_candidates
from inferyard.config.loader import load_config
from inferyard.config.local_engines import candidate as local_candidate
from inferyard.provisioning import preparation_io
from inferyard.provisioning.preparation_io import PreparationError
from tests.unit.test_community_candidates import candidate_request
from tests.unit.test_device_preflight import gguf

ROOT = Path(__file__).parents[2]


def _pair(args: list[str], flag: str) -> str:
    index = args.index(flag)
    return args[index + 1]


def _setup_prism_macos(tmp_path, monkeypatch, *, context_length=8192, mode="metal"):
    monkeypatch.setattr(preparation_io, "native_platform", lambda: ("Darwin", "arm64"))
    monkeypatch.setattr(
        "inferyard.platforms.identity.environment_snapshot",
        lambda: {"platform": "Darwin", "profile": "balanced"},
    )
    monkeypatch.setattr(community_candidates, "fresh_macos", lambda selected, _: selected)
    monkeypatch.setattr(community_candidates, "query_version", lambda *a, **k: {})
    model = gguf(tmp_path / "model.gguf", "qwen3")
    engine = tmp_path / "llama-server"
    engine.write_bytes(b"synthetic, never executed")
    (tmp_path / "libggml-metal.dylib").write_bytes(b"library")
    request = candidate_request(tmp_path, model, engine=engine, system="Darwin")
    preflight = json.loads(request.preflight.read_text())
    preflight["recommendation"]["selected"]["mode"] = mode
    preflight["recommendation"]["selected"]["context_size"] = min(4096, context_length)
    request.preflight.write_text(json.dumps(preflight))
    return request


def _setup_local_installation(tmp_path, monkeypatch, family: str):
    monkeypatch.setattr(preparation_io, "native_platform", lambda: ("Darwin", "arm64"))
    monkeypatch.setattr(
        local_candidate,
        "environment_snapshot",
        lambda: {"platform": "Darwin", "ac_online": True, "profile": "balanced"},
    )
    runtime_dir = tmp_path / "runtime"
    runtime_dir.mkdir()
    executable = runtime_dir / ("python" if family == "mlx-lm" else "llama-server")
    executable.write_bytes(b"synthetic")
    installation = {
        "schema_version": 3,
        "definition": "local-engine-installation.v1",
        "family": family,
        "executable": str(executable),
        "working_directory": str(tmp_path),
        "runtime_roots": [str(runtime_dir)],
        "environment": {
            "PYTHONDONTWRITEBYTECODE": "1",
            "HF_HUB_OFFLINE": "1",
            "TRANSFORMERS_OFFLINE": "1",
        },
    }
    inst_path = tmp_path / "installation.json"
    inst_path.write_text(json.dumps(installation))
    snapshot = {
        "schema_version": 3,
        "definition": "local-engine-runtime.v1",
        "installation": installation,
        "python": None,
        "roots": [],
        "unresolved_libraries": [],
    }
    monkeypatch.setattr(local_candidate, "read_installation", lambda _: installation)
    monkeypatch.setattr(local_candidate, "freeze_installation", lambda _: snapshot)
    monkeypatch.setattr(local_candidate, "verify_installation", lambda _: ())
    if family == "mlx-lm":
        model = tmp_path / "model-dir"
        model.mkdir()
        (model / "config.json").write_text("{}")
        (model / "weights.safetensors").write_bytes(b"weights")
        (model / "tokenizer_config.json").write_text(
            json.dumps({"chat_template": "{{ messages }}"})
        )
    else:
        model = gguf(tmp_path / "local.gguf", "local")
    return inst_path, model


@pytest.mark.parametrize(
    ("port_arg", "expected_port", "mode", "context"),
    [(None, 48857, "metal", 8192), (49123, 49123, "cpu", 2048)],
)
def test_prism_macos_config_create_port_and_conditions(
    tmp_path, monkeypatch, capsys, port_arg, expected_port, mode, context
):
    request = _setup_prism_macos(tmp_path, monkeypatch, context_length=context, mode=mode)
    if port_arg is None:
        details = community_candidates.create(replace(request, port=None))
    else:
        argv = [
            "config",
            "create",
            "--preflight",
            str(request.preflight),
            "--bundle",
            str(request.bundle_path),
            "--results",
            str(request.output_root),
            "--engine",
            str(request.engine_path),
            "--out",
            str(request.out),
            "--port",
            str(port_arg),
        ]
        code = main(argv)
        assert code == 0
        details = json.loads(capsys.readouterr().out)["details"]

    config = load_config(Path(details["candidate"])).config.to_dict()
    assert config["endpoint"]["url"] == f"http://127.0.0.1:{expected_port}"
    args = config["engine"]["startup_args"]
    assert _pair(args, "--host") == "127.0.0.1"
    assert _pair(args, "--port") == str(expected_port)
    assert config["engine"]["backend"] == mode
    assert config["conditions"]["context_size"] == min(4096, context)
    assert config["generation"]["reasoning_mode"] == "off"
    assert _pair(args, "--reasoning") == "off"
    assert _pair(args, "--chat-template-kwargs") == '{"enable_thinking":false}'


@pytest.mark.parametrize(
    ("family", "port_arg", "expected_port"),
    [("mlx-lm", 49234, 49234), ("llama-cpp", None, 48857)],
)
def test_local_engine_config_create_port_and_conditions(
    tmp_path, monkeypatch, capsys, family, port_arg, expected_port
):
    inst_path, model = _setup_local_installation(tmp_path, monkeypatch, family)
    out = tmp_path / "candidate"
    results = tmp_path / "results"
    bundle = ROOT / "bundles/zh-smoke.json"
    if port_arg is None:
        details = local_candidate.create(
            CommandRequest(
                "config create",
                installation=inst_path,
                model_path=model,
                bundle_path=bundle,
                output_root=results,
                out=out,
                port=None,
            )
        )
    else:
        code = main(
            [
                "config",
                "create",
                "--installation",
                str(inst_path),
                "--model",
                str(model),
                "--bundle",
                str(bundle),
                "--results",
                str(results),
                "--out",
                str(out),
                "--port",
                str(port_arg),
            ]
        )
        assert code == 0
        details = json.loads(capsys.readouterr().out)["details"]

    config = load_config(Path(details["candidate"])).config.to_dict()
    assert config["endpoint"]["url"] == f"http://127.0.0.1:{expected_port}"
    args = config["engine"]["startup_args"]
    assert _pair(args, "--host") == "127.0.0.1"
    assert _pair(args, "--port") == str(expected_port)
    assert config["engine"]["family"] == family
    assert config["engine"]["backend"] == "metal"
    assert config["conditions"]["context_size"] == 8192
    assert config["generation"]["temperature"] == 0


def test_local_engine_rejects_invalid_port(tmp_path, monkeypatch):
    inst_path, model = _setup_local_installation(tmp_path, monkeypatch, "llama-cpp")
    with pytest.raises(PreparationError, match="invalid_input"):
        local_candidate.create(
            CommandRequest(
                "config create",
                installation=inst_path,
                model_path=model,
                bundle_path=ROOT / "bundles/zh-smoke.json",
                output_root=tmp_path / "results",
                out=tmp_path / "candidate",
                port=70000,
            )
        )
