"""Synthetic runtime/process checks: no model requests or service startup."""

import copy
import os
from pathlib import Path
from types import SimpleNamespace

import pytest

from inferyard.config.local_engines.launch import startup
from inferyard.platforms.identity import PreflightError, hash_file
from inferyard.platforms.local_engines import installation as runtime
from inferyard.platforms.local_engines import service
from inferyard.platforms.local_engines.startup import check_environment, validate_startup


@pytest.fixture
def installation(tmp_path, monkeypatch):
    monkeypatch.setattr(runtime, "darwin", lambda: None)
    root = tmp_path / "runtime"
    root.mkdir()
    executable = root / "server"
    executable.write_bytes(b"synthetic executable")
    (root / "package.py").write_text("value = 1\n")
    value = {
        "schema_version": 3,
        "definition": "local-engine-installation.v1",
        "family": "llama-cpp",
        "executable": str(executable),
        "working_directory": str(tmp_path),
        "runtime_roots": [str(root)],
        "environment": {},
    }
    return value, root


def test_old_single_file_identity_misses_python_implementation(installation):
    _, root = installation
    with pytest.raises(PreflightError):
        hash_file(root)
    executable = hash_file(root / "server")
    (root / "package.py").write_text("changed implementation\n")
    assert executable.unchanged()
    assert hash_file(root / "server").sha256 == executable.sha256


def test_snapshot_verify(installation):
    declaration, _ = installation
    snapshot = runtime.freeze_installation(declaration)
    assert len(runtime.verify_installation(snapshot)) == 2


@pytest.mark.parametrize("mutation", ["modify", "add", "delete", "pyc", "symlink", "same_mtime"])
def test_runtime_drift(installation, mutation):
    declaration, root = installation
    snapshot = runtime.freeze_installation(declaration)
    source = root / "package.py"
    if mutation == "modify":
        source.write_text("different\n")
    elif mutation == "add":
        (root / "injected.py").write_text("injected\n")
    elif mutation == "delete":
        source.unlink()
    elif mutation == "pyc":
        (root / "package.pyc").write_bytes(b"old unchecked bytecode")
    elif mutation == "symlink":
        source.unlink()
        source.symlink_to(root / "server")
    else:
        before = source.stat()
        source.write_text("value = 2\n")
        os.utime(source, ns=(before.st_atime_ns, before.st_mtime_ns))
    with pytest.raises(PreflightError, match="runtime_changed"):
        runtime.verify_installation(snapshot)


def test_directory_symlink(installation):
    declaration, root = installation
    (root / "linked").symlink_to(root, target_is_directory=True)
    with pytest.raises(PreflightError, match="directory_symlink"):
        runtime.freeze_installation(declaration)


def test_symlink_target_drift(installation, tmp_path):
    declaration, root = installation
    target = tmp_path / "outside.py"
    target.write_text("first")
    (root / "external.py").symlink_to(target)
    snapshot = runtime.freeze_installation(declaration)
    target.write_text("second")
    with pytest.raises(PreflightError, match="runtime_changed"):
        runtime.verify_installation(snapshot)


def test_removed_root_rejected(installation):
    declaration, _ = installation
    snapshot = runtime.freeze_installation(declaration)
    snapshot["roots"] = []
    from inferyard.contracts.validation import ContractError

    with pytest.raises(ContractError):
        runtime.verify_installation(snapshot)


def test_uv_hook_drift(installation, monkeypatch):
    declaration, root = installation
    declaration["family"] = "vllm"
    monkeypatch.setattr(
        runtime,
        "_python",
        lambda _: (
            {
                "path": [],
                "prefix": str(root),
                "base_prefix": str(root),
                "paths": dict.fromkeys(
                    (
                        "stdlib",
                        "platstdlib",
                        "purelib",
                        "platlib",
                        "include",
                        "platinclude",
                        "scripts",
                        "data",
                    ),
                    str(root),
                ),
                "version": [3, 12],
                "site_packages": str(root),
                "import_roots": [str(root)],
                "entrypoint_sources": [str(root / "package.py")],
            },
            [],
        ),
    )
    monkeypatch.setattr(
        "inferyard.platforms.local_engines.python_sources.resolve_sources", lambda *_: None
    )
    (root / "_virtualenv.pth").write_text("import _virtualenv\n")
    (root / "_virtualenv.py").write_text("# frozen hook\n")
    snapshot = runtime.freeze_installation(declaration)
    runtime.verify_installation(snapshot)
    (root / "_virtualenv.py").write_text("# changed hook\n")
    with pytest.raises(PreflightError, match="runtime_changed"):
        runtime.verify_installation(snapshot)


@pytest.mark.parametrize(
    "name,text",
    [
        ("editable.pth", "import editable_hook"),
        ("_virtualenv.pth", "import arbitrary"),
        ("sitecustomize.py", "raise Exception()"),
    ],
)
def test_ignored_site_hooks_remain_frozen(installation, monkeypatch, name, text):
    declaration, root = installation
    declaration["family"] = "vllm"
    monkeypatch.setattr(
        runtime,
        "_python",
        lambda _: (
            {
                "path": [],
                "prefix": str(root),
                "base_prefix": str(root),
                "paths": dict.fromkeys(
                    (
                        "stdlib",
                        "platstdlib",
                        "purelib",
                        "platlib",
                        "include",
                        "platinclude",
                        "scripts",
                        "data",
                    ),
                    str(root),
                ),
                "version": [3, 12],
                "site_packages": str(root),
                "import_roots": [str(root)],
                "entrypoint_sources": [str(root / "package.py")],
            },
            [],
        ),
    )
    monkeypatch.setattr(
        "inferyard.platforms.local_engines.python_sources.resolve_sources", lambda *_: None
    )
    (root / name).write_text(text)
    snapshot = runtime.freeze_installation(declaration)
    runtime.verify_installation(snapshot)
    (root / name).write_text(text + "\n# modified\n")
    with pytest.raises(PreflightError, match="runtime_changed"):
        runtime.verify_installation(snapshot)


def test_platform_rejected(monkeypatch):
    monkeypatch.setattr(runtime.platform, "system", lambda: "Linux")
    with pytest.raises(PreflightError, match="darwin_arm64"):
        runtime.darwin()


@pytest.fixture
def bound(tmp_path, monkeypatch):
    executable, model = tmp_path / "python", tmp_path / "model"
    executable.write_bytes(b"interpreter")
    model.mkdir()
    declaration = {
        "family": "vllm",
        "executable": str(executable),
        "working_directory": str(tmp_path),
        "environment": {},
    }
    args, _ = startup(declaration, model, "fixture", 8192, 8000)
    config = {
        "engine": {
            "family": "vllm",
            "startup_args": args,
            "environment": {},
            "working_directory": str(tmp_path),
            "installation_sha256": "a" * 64,
        },
        "model": {"local_path": str(model)},
        "conditions": {"context_size": 8192},
        "endpoint": {"server_pid": 42, "process_start_ticks": 123, "url": "http://127.0.0.1:8000"},
    }
    argv = [str(executable), *args]
    state = (123, argv, b"\0".join(x.encode() for x in argv) + b"\0", executable, tmp_path)
    psutil = SimpleNamespace(Process=lambda _: SimpleNamespace(environ=lambda: {}), Error=OSError)
    identity = hash_file(executable)
    row = {
        "f": "txt",
        "t": "REG",
        "n": str(executable),
        "p": 42,
        "D": hex(identity.device),
        "i": str(identity.inode),
    }
    monkeypatch.setattr(service.native, "_state", lambda *_: state)
    monkeypatch.setattr(service, "lsof_records", lambda *_: [row])
    monkeypatch.setattr(service, "_listener_identity", lambda *_: "listener")
    monkeypatch.setattr(service.native, "unique_listener", lambda *_: None)
    return config, declaration, [identity], psutil, state


def test_direct_binding(bound):
    config, declaration, files, psutil, _ = bound
    result = service._direct(config, declaration, files, psutil)
    assert result["pid"] == 42
    assert result["model_binding"]["source"] == "verified_startup_directory_identity"


@pytest.mark.parametrize("mutation", ["entrypoint", "isolation", "second_model", "argv", "cwd"])
def test_bad_startup(bound, mutation):
    config, declaration, _, _, state = bound
    config = copy.deepcopy(config)
    args = config["engine"]["startup_args"]
    if mutation == "entrypoint":
        args[3] = "unrelated.server"
    elif mutation == "isolation":
        args[0] = "-E"
    elif mutation == "second_model":
        args.extend(["--model", config["model"]["local_path"]])
    elif mutation == "argv":
        args.append("--arbitrary")
    else:
        config["engine"]["working_directory"] = "/"
    with pytest.raises(PreflightError):
        validate_startup(
            config, declaration, [declaration["executable"], *args], state[3], state[4]
        )


def test_pid_reuse(bound):
    config, declaration, files, psutil, _ = bound
    config["endpoint"]["process_start_ticks"] = 124
    with pytest.raises(PreflightError, match="pid_reused"):
        service._direct(config, declaration, files, psutil)


def test_listener_ambiguity(bound, monkeypatch):
    config, declaration, files, psutil, _ = bound

    def ambiguous(*_):
        raise PreflightError("engine_fit_listener_ambiguous_or_changed")

    monkeypatch.setattr(service.native, "unique_listener", ambiguous)
    with pytest.raises(PreflightError, match="listener_ambiguous"):
        service._direct(config, declaration, files, psutil)


@pytest.mark.parametrize(
    "environment",
    [{"PYTHONPATH": "/tmp"}, {"DYLD_INSERT_LIBRARIES": "x"}, {"VLLM_PLUGINS": "injected"}],
)
def test_env_override(environment):
    with pytest.raises(PreflightError, match="environment_override"):
        check_environment(environment, {})


def test_required_env():
    with pytest.raises(PreflightError, match="environment_mismatch"):
        check_environment({}, {"SGLANG_USE_MLX": "1"})


@pytest.mark.skipif(os.name != "posix", reason="Darwin mapping identity primitives")
def test_mapping_closure(tmp_path):
    plugin = tmp_path / "plugin.dylib"
    plugin.write_bytes(b"\xcf\xfa\xed\xfe")
    info = plugin.stat()
    with pytest.raises(PreflightError, match="mapping_outside"):
        service.mapped_closure(
            [
                {
                    "f": "txt",
                    "t": "REG",
                    "n": str(plugin),
                    "D": hex(info.st_dev),
                    "i": str(info.st_ino),
                }
            ],
            [],
            Path("/fixture"),
        )


def test_does_not_reaccept_argv(bound, monkeypatch):
    config, declaration, files, psutil, _ = bound
    observed = service._direct(config, declaration, files, psutil)
    observed["workers"] = []
    frozen = {
        "schema_version": 3,
        "definition": service.DEFINITION,
        "installation_sha256": "a" * 64,
        "service": observed,
    }
    config["endpoint"].update(binding_path="fixture", binding_sha256="b" * 64)
    monkeypatch.setattr(service, "bound_json", lambda *_: (frozen, None))
    changed = copy.deepcopy(frozen)
    changed["service"]["argv_sha256"] = "c" * 64
    monkeypatch.setattr(service, "bind_service", lambda _: changed)
    with pytest.raises(PreflightError, match="bound_service_changed"):
        service.verify_bound_service(config)
