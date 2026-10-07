"""Synthetic startup identity evidence for additional diagnostic engines."""

import hashlib
from pathlib import Path

import pytest

from inferyard.config import engine_fit_assets
from inferyard.platforms import engine_fit as fit
from inferyard.platforms import engine_fit_macos as macos
from inferyard.platforms.engine_fit_entrypoints import startup_binding
from inferyard.platforms.identity import PreflightError
from tests.unit import test_engine_fit_identity as linux_fixtures
from tests.unit import test_engine_fit_macos_identity as mac_fixtures

native = mac_fixtures.native
service = linux_fixtures.service


def _llama_native(native):
    state, _, _, directory = native
    model = directory.parent / "model.gguf"
    model.write_bytes(b"synthetic single GGUF")
    old = Path(state["exe"])
    renamed = old.with_name("llama-server")
    old.rename(renamed)
    state.update(exe=str(renamed), args=[str(renamed), "-m", model.name, "--metrics"])
    state["environment"] = {}
    native[1].environ = lambda: dict(state["environment"])
    return model


def test_llama_macos_binding_is_native_single_file_and_revalidates(native, monkeypatch):
    model = _llama_native(native)
    monkeypatch.setattr(fit.platform, "system", lambda: "Darwin")
    binding = fit.bind_service("llama-cpp", model, 321, "http://127.0.0.1:8080")
    assert binding["model_binding"]["source"] == "verified_startup_file_identity"
    assert binding["listener_inode"] is None
    fit.check_service(binding)
    model.rename(model.with_suffix(".old"))
    model.write_bytes(b"replacement")
    with pytest.raises(PreflightError, match="identity_changed"):
        fit.check_service(binding)


@pytest.mark.parametrize("basename", ["llama-server", "renamed-engine"])
def test_llama_linux_binding_reuses_pid_and_listener_checks(service, basename):
    proc, process, directory = service
    model = directory.parent / "model.gguf"
    model.write_bytes(b"synthetic single GGUF")
    executable = directory.parent / basename
    executable.write_bytes(b"fixture executable")
    (process / "exe").unlink()
    (process / "exe").symlink_to(executable)
    (process / "environ").write_bytes(b"OTHER_TOKEN=private-credential\0")
    linux_fixtures._cmdline(
        process, basename, "--model=model.gguf", "--metrics", "--log-prefix", "--log-verbosity", "0"
    )
    binding = fit.bind_service("llama-cpp", model, 123, "http://127.0.0.1:8080", proc_root=proc)
    assert binding["model_binding"]["source"] == "verified_startup_file_identity"
    assert binding["listener_inode"] == "456"
    fit.check_service(binding, proc_root=proc)
    (process / "environ").write_bytes(b"LLAMA_ARG_RPC=private-host\0")
    with pytest.raises(PreflightError, match="requires_explicit_args"):
        fit.check_service(binding, proc_root=proc)


@pytest.mark.parametrize(
    "args",
    [
        ["--model", "other.gguf"],
        ["--model-draft", "draft.gguf"],
        ["-md", "draft.gguf"],
        ["--lora", "adapter.gguf"],
        ["--mmproj", "vision.gguf"],
        ["--models-dir", "models"],
        ["-hf", "remote/model"],
        ["--hf-repo", "remote/model"],
        ["--model-url", "https://example.org"],
        ["--rpc", "127.0.0.1:1234"],
        ["--config", "config.json"],
        ["--unknown-model-setting"],
        ["--metrics=true"],
        ["--alias"],
        ["--model="],
        ["--", "file.gguf"],
    ],
)
def test_llama_alternate_assets_downloads_and_ambiguous_options_fail(native, args):
    model = _llama_native(native)
    native[0]["args"].extend(args)
    with pytest.raises(PreflightError):
        macos.bind_service("llama-cpp", model, 321, "http://127.0.0.1:8080")


def test_llama_does_not_accept_directory_model_or_python_wrapper(native):
    model = _llama_native(native)
    with pytest.raises(PreflightError, match="model_argument_mismatch"):
        macos.bind_service("llama-cpp", native[3], 321, "http://127.0.0.1:8080")
    native[0]["args"] = ["python3", "llama-server", "--model", str(model)]
    with pytest.raises(PreflightError, match="model_argument_ambiguous"):
        macos.bind_service("llama-cpp", model, 321, "http://127.0.0.1:8080")


def _mlx_native(native, monkeypatch):
    state, _, _, model = native
    script = model.parent / "mlx_server.py"
    script.write_bytes(b"# synthetic controlled server fixture\n")
    digest = hashlib.sha256(script.read_bytes()).hexdigest()
    monkeypatch.setattr(engine_fit_assets, "mlx_server_sha256", lambda: digest)
    state["args"] = ["Python", "-u", script.name, "--model", "model", "--port", "8080"]
    return script, digest


def test_mlx_only_binds_the_packaged_server_content_and_rechecks_it(native, monkeypatch):
    script, digest = _mlx_native(native, monkeypatch)
    monkeypatch.setattr(fit.platform, "system", lambda: "Darwin")
    binding = fit.bind_service("mlx-lm", native[3], 321, "http://127.0.0.1:8080")
    assert binding["entrypoint_sha256"] == digest
    assert binding["model_binding"]["source"] == "verified_startup_directory_identity"
    fit.check_service(binding)
    script.write_bytes(b"# changed server\n")
    with pytest.raises(PreflightError, match="mlx_entrypoint_changed"):
        fit.check_service(binding)


@pytest.mark.parametrize(
    "args",
    [
        ["Python", "-m", "mlx_lm.server", "--model", "model"],
        ["Python", "-c", "code", "--model", "model"],
        ["Python", "mlx_server.py", "--model", "model", "--adapter-path", "adapter"],
        ["Python", "mlx_server.py", "--model", "model", "--model=model"],
    ],
)
def test_mlx_upstream_or_ambiguous_entrypoint_is_rejected(native, monkeypatch, args):
    _mlx_native(native, monkeypatch)
    native[0]["args"] = args
    with pytest.raises(PreflightError):
        macos.bind_service("mlx-lm", native[3], 321, "http://127.0.0.1:8080")


@pytest.mark.parametrize("length", [1, 512, 32768])
@pytest.mark.parametrize("equal", [False, True])
def test_mlx_binds_valid_optional_context(native, monkeypatch, length, equal):
    _mlx_native(native, monkeypatch)
    native[0]["args"].extend(
        [f"--max-model-len={length}"] if equal else ["--max-model-len", str(length)]
    )
    binding = macos.bind_service("mlx-lm", native[3], 321, "http://127.0.0.1:8080")
    assert binding["model_binding"]["source"] == "verified_startup_directory_identity"


@pytest.mark.parametrize(
    "options",
    [
        ["--max-model-len", "0"],
        ["--max-model-len=32769"],
        ["--max-model-len", "-1"],
        ["--max-model-len", "1.5"],
        ["--max-model-len", "true"],
        ["--max-model-len="],
        ["--max-model-len"],
        ["--max-model-len=512", "--max-model-len", "512"],
        ["--max-model-len=512", "--context-length", "512"],
    ],
)
def test_mlx_rejects_invalid_duplicate_or_unknown_context(native, monkeypatch, options):
    _mlx_native(native, monkeypatch)
    native[0]["args"].extend(options)
    with pytest.raises(PreflightError, match="model_argument_ambiguous"):
        macos.bind_service("mlx-lm", native[3], 321, "http://127.0.0.1:8080")


def test_script_changed_during_native_evidence_read_is_rejected(native, monkeypatch):
    script, _ = _mlx_native(native, monkeypatch)
    original = macos.lsof_records

    def change(*args):
        result = original(*args)
        script.write_bytes(b"# replaced during binding\n")
        return result

    monkeypatch.setattr(macos, "lsof_records", change)
    with pytest.raises(PreflightError, match="mlx_entrypoint_changed"):
        macos.bind_service("mlx-lm", native[3], 321, "http://127.0.0.1:8080")


def test_additional_services_refuse_unsupported_native_platform(service):
    proc, _, model = service
    for engine in ("mlx-lm", "lmstudio"):
        with pytest.raises(PreflightError, match="invalid_service"):
            fit.bind_service(engine, model, 123, "http://127.0.0.1:8080", proc_root=proc)


def test_empty_argument_vector_fails_cleanly(tmp_path):
    with pytest.raises(PreflightError, match="arguments_unavailable"):
        startup_binding("llama-cpp", [], tmp_path / "llama-server", tmp_path)


@pytest.mark.parametrize(
    "name,value",
    [
        ("LLAMA_ARG_RPC", "private-host:1234"),
        ("LLAMA_ARG_MMPROJ", "private-file.gguf"),
        ("LLAMA_ARG_MMPROJ_URL", "https://private.example/model"),
        ("LLAMA_ARG_MODEL", "other"),
        ("LLAMA_ARG_API_KEY", "private-token"),
        ("LLAMA_ARG_METRICS", ""),
    ],
)
def test_llama_mac_environment_cannot_override_frozen_arguments(native, name, value):
    model = _llama_native(native)
    native[0]["environment"][name] = value
    with pytest.raises(PreflightError, match="requires_explicit_args") as error:
        macos.bind_service("llama-cpp", model, 321, "http://127.0.0.1:8080")
    assert "private" not in str(error.value)
    assert name not in str(error.value)


def test_llama_mac_environment_is_rechecked_after_binding(native, monkeypatch):
    model = _llama_native(native)
    monkeypatch.setattr(fit.platform, "system", lambda: "Darwin")
    binding = fit.bind_service("llama-cpp", model, 321, "http://127.0.0.1:8080")
    native[0]["environment"]["LLAMA_ARG_RPC"] = "private-host"
    with pytest.raises(PreflightError, match="requires_explicit_args"):
        fit.check_service(binding)


def test_llama_mac_unreadable_environment_fails_closed(native):
    model = _llama_native(native)

    def denied():
        raise mac_fixtures.Denied("private environment")

    native[1].environ = denied
    with pytest.raises(PreflightError, match="environment_unavailable"):
        macos.bind_service("llama-cpp", model, 321, "http://127.0.0.1:8080")


def test_llama_environment_added_during_native_evidence_read_is_rejected(native, monkeypatch):
    model = _llama_native(native)
    original = macos.lsof_records

    def changed(*args):
        result = original(*args)
        native[0]["environment"]["LLAMA_ARG_MMPROJ"] = "private-model.gguf"
        return result

    monkeypatch.setattr(macos, "lsof_records", changed)
    with pytest.raises(PreflightError, match="requires_explicit_args"):
        macos.bind_service("llama-cpp", model, 321, "http://127.0.0.1:8080")


@pytest.mark.parametrize(
    "raw",
    [
        b"LLAMA_ARG_RPC=private-host\0",
        b"LLAMA_ARG_MMPROJ=private-file\0",
        b"LLAMA_ARG_METRICS=\0",
        b"MALFORMED\0",
        b"OTHER=value",
        b"A=x\0A=y\0",
    ],
)
def test_linux_llama_environment_rejects_overrides_and_unreadable_forms(tmp_path, raw):
    from inferyard.platforms.engine_fit_entrypoints import linux_llama_environment

    (tmp_path / "environ").write_bytes(raw)
    with pytest.raises(PreflightError) as error:
        linux_llama_environment(tmp_path)
    assert "private" not in str(error.value)


def test_linux_llama_environment_missing_is_not_an_empty_environment(tmp_path):
    from inferyard.platforms.engine_fit_entrypoints import linux_llama_environment

    with pytest.raises(PreflightError, match="environment_unavailable"):
        linux_llama_environment(tmp_path)
    (tmp_path / "environ").write_bytes(b"")
    linux_llama_environment(tmp_path)
