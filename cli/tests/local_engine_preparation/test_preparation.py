"""Fixture assets and mocked observations; no model services are started."""

import json
from pathlib import Path

import pytest

from inferyard.application.types import CommandRequest
from inferyard.config.loader import load_config
from inferyard.config.local_engines import binding, candidate, inspection, launch
from inferyard.config.preparation_io import PreparationError, sha256
from inferyard.platforms.identity import PreflightError


@pytest.fixture
def prepared(tmp_path, monkeypatch):
    model = tmp_path / "model"
    model.mkdir()
    (model / "config.json").write_text("{}")
    (model / "tokenizer_config.json").write_text('{"chat_template":"{{ messages }}"}')
    (model / "model.safetensors").write_bytes(b"fixture weights")
    declaration = {
        "schema_version": 3,
        "definition": "local-engine-installation.v1",
        "family": "mlx-lm",
        "executable": str(tmp_path / "python"),
        "working_directory": str(tmp_path),
        "runtime_roots": [str(tmp_path)],
        "environment": {
            "PYTHONDONTWRITEBYTECODE": "1",
            "HF_HUB_OFFLINE": "1",
            "TRANSFORMERS_OFFLINE": "1",
        },
    }
    runtime = {"definition": "local-engine-runtime.v1", "installation": declaration}
    monkeypatch.setattr(candidate, "require_platform", lambda *a: None)
    monkeypatch.setattr(candidate, "read_installation", lambda p: declaration)
    monkeypatch.setattr(candidate, "freeze_installation", lambda d: runtime)
    monkeypatch.setattr(candidate, "verify_installation", lambda r: [])
    monkeypatch.setattr(candidate, "environment_snapshot", lambda: {"ac_online": True})
    request = CommandRequest(
        "config create",
        installation=tmp_path / "declaration.json",
        model_path=model,
        bundle_path=Path(__file__).resolve().parents[2] / "bundles/zh-smoke.json",
        output_root=tmp_path / "results",
        out=tmp_path / "candidate",
        port=48857,
    )
    result = candidate.create(request)
    return request, Path(result["candidate"]), runtime


def test_candidate_freezes_assets_and_safe_defaults(prepared):
    request, path, _ = prepared
    config = load_config(path).config.to_dict()
    assert config["generation"] == {"temperature": 0, "max_tokens": 512}
    assert config["conditions"]["context_size"] == 8192
    assert config["execution"]["timeout_seconds"] == 600
    assert config["safety"]["require_temperature"] is True
    assert config["safety"]["check_environment"] is True
    assert config["engine"]["startup_args"][:3] == ["-I", "-S", "-B"]
    assert config["engine"]["startup_args"][3].endswith("/data/local_engine/bootstrap.py")
    assert config["engine"]["startup_args"][4] == "mlx-lm"
    assert config["engine"]["installation_path"] != str(request.installation)
    for section, filekey, hashkey in (
        ("engine", "installation_path", "installation_sha256"),
        ("model", "assets_path", "assets_sha256"),
        ("model", "template_path", "template_sha256"),
    ):
        assert sha256(Path(config[section][filekey]).read_bytes()) == config[section][hashkey]


def test_describe_has_concrete_launch_and_honest_unknowns(prepared, monkeypatch):
    _, path, runtime = prepared
    monkeypatch.setattr(inspection, "assets", lambda config: (runtime, [], []))
    value = inspection.describe(CommandRequest("config describe", candidate=path))
    assert value["launch"]["kind"] == "direct_process"
    assert value["launch"]["argv"][0] == runtime["installation"]["executable"]
    assert value["effective_parameters"] is None
    assert value["effective_template"] is None
    assert value["bound"] is False


def test_bind_rejects_identity_mismatch_without_output(prepared, monkeypatch, tmp_path):
    _, path, _ = prepared
    monkeypatch.setattr(binding, "process_start_ticks", lambda pid: 123)

    def fail(config):
        raise PreflightError("local_engine_startup_mismatch")

    monkeypatch.setattr(binding, "bind_service", fail)
    request = CommandRequest(
        "config bind",
        candidate=path,
        out=tmp_path / "bound",
        server_pid=22,
        endpoint_url="http://127.0.0.1:48857",
    )
    with pytest.raises(PreflightError, match="startup_mismatch"):
        binding.prepare(request)
    assert not request.out.exists()


def test_bind_writes_new_verified_binding(prepared, monkeypatch, tmp_path):
    _, path, _ = prepared
    original = path.read_bytes()
    monkeypatch.setattr(binding, "process_start_ticks", lambda pid: 123)
    value = {"definition": "local-engine-binding.v1", "service": {"pid": 22}}
    monkeypatch.setattr(binding, "bind_service", lambda config: value)
    verified = []
    monkeypatch.setattr(binding, "verify_bound_service", lambda config: verified.append(config))
    result = binding.prepare(
        CommandRequest(
            "config bind",
            candidate=path,
            out=tmp_path / "bound",
            server_pid=22,
            endpoint_url="http://127.0.0.1:48857",
        )
    )
    config = load_config(Path(result["config"])).config.to_dict()
    assert config["endpoint"]["server_pid"] == 22
    assert json.loads(Path(config["endpoint"]["binding_path"]).read_text()) == value
    assert path.read_bytes() == original
    assert len(verified) == 1


def test_missing_template_is_rejected(tmp_path):
    (tmp_path / "tokenizer_config.json").write_text("{}")
    with pytest.raises(PreparationError, match="chat_template_unavailable"):
        candidate.template(tmp_path)


def test_lmstudio_requires_observed_background_process():
    with pytest.raises(PreparationError, match="background_pid_required"):
        launch.startup({"family": "lmstudio", "working_directory": "/"}, "/m", "m", 8192, 1234)


@pytest.mark.parametrize(
    "reason,public",
    [
        ("local_engine_environment_override", "local_engine_environment_override"),
        ("local_engine_mapping_outside_runtime", "local_engine_mapping_outside_runtime"),
        ("local_engine_private_token=secret", "preflight_blocked"),
    ],
)
def test_bind_cli_reports_public_reason_without_exception_details(tmp_path, capsys, reason, public):
    from inferyard.cli import main

    def blocked(request):
        raise PreflightError(reason)

    code = main(
        [
            "config",
            "bind",
            "--candidate",
            str(tmp_path / "candidate.toml"),
            "--pid",
            "123",
            "--endpoint",
            "http://127.0.0.1:48857",
            "--out",
            str(tmp_path / "bound"),
        ],
        handlers={"config bind": blocked},
    )
    captured = capsys.readouterr()
    assert code == 2
    assert json.loads(captured.out)["limitations"] == [public]
    assert "secret" not in captured.out + captured.err


def test_engine_check_rejects_incomplete_runtime(monkeypatch, tmp_path):
    from inferyard.application.engine_check import execute
    from inferyard.platforms import local_engines

    monkeypatch.setattr(local_engines, "read_installation", lambda p: {"family": "vllm"})

    def fail(value):
        raise PreflightError("local_engine_runtime_root_invalid")

    monkeypatch.setattr(local_engines, "freeze_installation", fail)
    code, result = execute(CommandRequest("engine check", installation=tmp_path / "i.json"))
    assert code == 0
    assert result.details["eligible"] is False
    assert result.details["ready_to_run"] is False
    assert result.details["reason"] == "local_engine_runtime_root_invalid"
    assert result.details["definition"] == "local-engine-check.v1"


@pytest.mark.parametrize("family", ["llama-cpp", "mlx-lm", "vllm", "sglang"])
def test_launch_matches_identity_parser(family, tmp_path):
    from inferyard.platforms.local_engines.startup import validate_startup

    installation = {
        "family": family,
        "executable": str(tmp_path / "exe"),
        "working_directory": str(tmp_path),
        "environment": {},
    }
    model = tmp_path / "model"
    args, cwd = launch.startup(installation, model, "bench", 8192, 48857)
    config = {
        "engine": {
            "family": family,
            "startup_args": args,
            "working_directory": cwd,
            "environment": {},
        },
        "model": {"local_path": str(model)},
        "conditions": {"context_size": 8192},
    }
    validate_startup(
        config,
        installation,
        [installation["executable"], *args],
        Path(installation["executable"]),
        tmp_path,
    )


@pytest.mark.parametrize(
    "option,value",
    [
        ("--max-tokens", "0"),
        ("--context-size", "0"),
        ("--timeout-seconds", "nan"),
        ("--service-pid", "0"),
    ],
)
def test_cli_rejects_invalid_local_options(option, value, capsys):
    from inferyard import cli

    assert (
        cli.main(
            [
                "config",
                "create",
                "--installation",
                "/i.json",
                "--model",
                "/m",
                "--results",
                "/r",
                "--bundle",
                "/b.json",
                "--out",
                "/o",
                option,
                value,
            ]
        )
        == 2
    )
    assert json.loads(capsys.readouterr().out)["status"] == "blocked"


def test_pelican_default_uses_bundle_id_not_filename(prepared, tmp_path):
    from dataclasses import replace

    request, _, _ = prepared
    bundle = request.bundle_path.parent / "zh-svg-pelican.json"
    renamed = tmp_path / "renamed.json"
    renamed.write_bytes(bundle.read_bytes())
    result = candidate.create(replace(request, bundle_path=renamed, out=tmp_path / "pelican"))
    config = load_config(Path(result["candidate"])).config.to_dict()
    assert config["generation"]["max_tokens"] == 4096
