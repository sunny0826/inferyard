"""Synthetic startup identity evidence for additional diagnostic engines."""

import hashlib
from pathlib import Path

import pytest

from inferyard.platforms import engine_fit_assets
from inferyard.errors import PreflightError
from inferyard.platforms import engine_fit as fit
from inferyard.platforms import engine_fit_macos as macos
from inferyard.platforms.engine_fit_entrypoints import startup_binding
from tests.unit import test_engine_fit_identity as linux_fixtures
from tests.unit import engine_fit_macos_helpers as mac_fixtures

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
