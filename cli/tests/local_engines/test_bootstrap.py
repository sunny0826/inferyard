"""Real stdlib subprocesses in disposable venvs; no engine or model is loaded."""

import copy
import json
import subprocess
import sys
from importlib.resources import files
from pathlib import Path
from types import SimpleNamespace

import pytest

from inferyard.config.local_engines.launch import startup
from inferyard.platforms.identity import PreflightError, hash_file
from inferyard.platforms.local_engines import workers
from inferyard.platforms.local_engines.installation import _python
from inferyard.platforms.local_engines.python_sources import resolve_sources
from inferyard.platforms.local_engines.startup import validate_arguments
from inferyard.platforms.local_engines.workers import validate_python_worker

BOOTSTRAP = str(files("inferyard").joinpath("data/local_engine/bootstrap.py"))
WORKER = """
import importlib.util
import sys

def run(connection):
    connection.send({
        "path": sys.path,
        "argv": sys.orig_argv,
        "flags": [sys.flags.isolated, sys.flags.no_site, sys.flags.dont_write_bytecode],
        "outside": importlib.util.find_spec("outside_module") is not None,
    })
    connection.close()
"""
ENTRY = """
import importlib.util
import json
import multiprocessing
import sys
from pathlib import Path
from fixture_worker import run

if __name__ == "__main__":
    result = {
        "path": sys.path,
        "argv": sys.orig_argv,
        "engine_argv": sys.argv[1:],
        "outside": importlib.util.find_spec("outside_module") is not None,
        "workers": [],
    }
    if "--workers" in sys.argv:
        for method in ("spawn", "forkserver"):
            context = multiprocessing.get_context(method)
            receiver, sender = context.Pipe(duplex=False)
            process = context.Process(target=run, args=(sender,))
            process.start()
            sender.close()
            if not receiver.poll(15):
                process.kill()
                process.join()
                raise RuntimeError("fixture worker timeout")
            result["workers"].append(receiver.recv())
            receiver.close()
            process.join(15)
            if process.is_alive():
                process.kill()
                process.join()
            assert process.exitcode == 0
    print(json.dumps(result))
"""


@pytest.fixture
def environment(tmp_path):
    root = tmp_path.resolve()
    venv = root / "venv"
    subprocess.run(
        [sys.executable, "-I", "-S", "-B", "-m", "venv", "--without-pip", str(venv)],
        check=True,
        capture_output=True,
        timeout=30,
    )
    executable = venv / "bin" / "python"
    version = f"python{sys.version_info.major}.{sys.version_info.minor}"
    site = venv / "lib" / version / "site-packages"
    outside = root / "outside"
    outside.mkdir()
    (outside / "outside_module.py").write_text("value = 1\n")
    marker = root / "hook-executed"
    customize = root / "customize-executed"
    (site / "_virtualenv.pth").write_text("import _virtualenv\n")
    (site / "_virtualenv.py").write_text(
        f"import sys\nfrom pathlib import Path\nPath({str(marker)!r}).touch()\n"
        f"sys.path.insert(0, {str(outside)!r})\n"
    )
    (site / "outside.pth").write_text(str(outside) + "\n")
    (site / "sitecustomize.py").write_text(
        f"from pathlib import Path\nPath({str(customize)!r}).touch()\n"
    )
    package = site / "sglang"
    package.mkdir()
    (package / "__init__.py").write_text("")
    (package / "launch_server.py").write_text(ENTRY)
    (site / "fixture_worker.py").write_text(WORKER)
    declaration = {
        "family": "sglang",
        "executable": str(executable),
        "working_directory": str(root),
    }
    return declaration, site, marker, customize


def invoke(declaration, arguments):
    return subprocess.run(
        [declaration["executable"], *arguments],
        capture_output=True,
        text=True,
        cwd=declaration["working_directory"],
        timeout=60,
    )


def test_old_site_startup_executes_hook_but_bootstrap_does_not(environment):
    declaration, _, marker, customize = environment
    baseline = invoke(declaration, ["-I", "-B", "-m", "sglang.launch_server", "--fixture"])
    assert baseline.returncode == 0, baseline.stderr
    assert marker.exists() and customize.exists()
    assert json.loads(baseline.stdout)["outside"] is True
    marker.unlink()
    customize.unlink()

    result = invoke(declaration, ["-I", "-S", "-B", BOOTSTRAP, "sglang", "--fixture"])
    assert result.returncode == 0, result.stderr
    observed = json.loads(result.stdout)
    assert not marker.exists() and not customize.exists()
    assert observed["outside"] is False
    assert observed["engine_argv"] == ["--fixture"]
    python, roots = _python(declaration)
    resolve_sources(python, declaration, roots)
    assert observed["path"] == python["import_roots"]
    assert str(Path(declaration["working_directory"])) not in observed["path"]
    assert not marker.exists() and not customize.exists()


def test_spawn_and_forkserver_inherit_isolation_and_frozen_path(environment):
    declaration, _, marker, customize = environment
    result = invoke(declaration, ["-I", "-S", "-B", BOOTSTRAP, "sglang", "--workers"])
    assert result.returncode == 0, result.stderr
    observed = json.loads(result.stdout)
    assert len(observed["workers"]) == 2
    for worker in observed["workers"]:
        assert worker["flags"] == [1, 1, 1]
        assert worker["path"] == observed["path"]
        assert worker["outside"] is False
        validate_python_worker(worker["argv"], observed["argv"], observed["path"])
    assert not marker.exists() and not customize.exists()


def test_editable_path_is_not_an_import_root(environment):
    declaration, site, _, _ = environment
    (site / "sglang" / "launch_server.py").unlink()
    (site / "sglang" / "__init__.py").unlink()
    (site / "sglang").rmdir()
    external = Path(declaration["working_directory"]) / "outside" / "sglang"
    external.mkdir()
    (external / "__init__.py").write_text("")
    (external / "launch_server.py").write_text(ENTRY)
    python, roots = _python(declaration)
    roots.append(external.parent)
    with pytest.raises(PreflightError, match="entrypoint_missing"):
        resolve_sources(python, declaration, roots)
    result = invoke(declaration, ["-I", "-S", "-B", BOOTSTRAP, "sglang"])
    assert result.returncode != 0 and "No module named" in result.stderr


def test_bootstrap_rejects_unknown_family(environment):
    declaration, _, marker, _ = environment
    result = invoke(declaration, ["-I", "-S", "-B", BOOTSTRAP, "arbitrary"])
    assert result.returncode != 0 and "direct_family_invalid" in result.stderr
    assert not marker.exists()


def test_bootstrap_requires_venv(tmp_path):
    declaration = {
        "executable": str(Path(sys.executable).resolve()),
        "working_directory": str(tmp_path),
    }
    result = invoke(declaration, ["-I", "-S", "-B", BOOTSTRAP, "sglang"])
    assert result.returncode != 0 and "python_venv_required" in result.stderr


@pytest.mark.parametrize("family", ["mlx-lm", "vllm", "sglang"])
def test_formal_entry_rejects_bootstrap_bypass(family, tmp_path):
    declaration = {"family": family, "working_directory": str(tmp_path)}
    arguments, _ = startup(declaration, tmp_path / "model", "fixture", 8192, 8000)
    config = {
        "engine": {"family": family, "startup_args": arguments},
        "model": {"local_path": str(tmp_path / "model")},
        "conditions": {"context_size": 8192},
    }
    validate_arguments(config)
    config["engine"]["startup_args"] = [
        "-I",
        "-S",
        "-B",
        "-m",
        "sglang.launch_server",
        *arguments[5:],
    ]
    with pytest.raises(PreflightError, match="entrypoint_mismatch"):
        validate_arguments(config)


@pytest.mark.parametrize("family", ["llama-cpp", "mlx-lm", "vllm", "sglang"])
def test_context_matches_frozen_conditions(family, tmp_path):
    declaration = {"family": family, "working_directory": str(tmp_path)}
    args, _ = startup(declaration, tmp_path / "model", "fixture", 8192, 8000)
    config = {
        "engine": {"family": family, "startup_args": args},
        "model": {"local_path": str(tmp_path / "model")},
        "conditions": {"context_size": 128},
    }
    with pytest.raises(PreflightError, match="context_argument_mismatch"):
        validate_arguments(config)


def test_llama_context_shift_is_disabled(tmp_path):
    args, _ = startup(
        {"family": "llama-cpp", "working_directory": str(tmp_path)},
        tmp_path / "model",
        "fixture",
        8192,
        8000,
    )
    args.remove("--no-context-shift")
    with pytest.raises(PreflightError, match="llama_lifecycle_flags_required"):
        validate_arguments(
            {
                "engine": {"family": "llama-cpp", "startup_args": args},
                "model": {"local_path": str(tmp_path / "model")},
                "conditions": {"context_size": 8192},
            }
        )


@pytest.mark.parametrize("mutation", ["site", "command", "path"])
def test_worker_rejects_unfrozen_execution(environment, mutation):
    declaration, _, _, _ = environment
    result = invoke(declaration, ["-I", "-S", "-B", BOOTSTRAP, "sglang", "--workers"])
    assert result.returncode == 0, result.stderr
    observed = json.loads(result.stdout)
    arguments = copy.deepcopy(observed["workers"][1]["argv"])
    if mutation == "site":
        arguments.remove("-S")
    elif mutation == "command":
        arguments[arguments.index("-c") + 1] += "; import arbitrary"
    else:
        arguments[arguments.index("-c") + 1] = arguments[arguments.index("-c") + 1].replace(
            repr(observed["path"][0]), repr("/outside")
        )
    with pytest.raises(PreflightError, match="python_worker"):
        validate_python_worker(arguments, observed["argv"], observed["path"])


def test_worker_observer_rejects_same_venv_without_no_site(environment, monkeypatch):
    declaration, _, _, _ = environment
    executable = Path(declaration["executable"]).resolve()
    cwd = Path(declaration["working_directory"])
    arguments = [declaration["executable"], "-I", "-S", "-B", BOOTSTRAP, "sglang"]
    child_args = [
        declaration["executable"],
        "-B",
        "-I",
        "-c",
        "from multiprocessing.spawn import spawn_main; spawn_main(tracker_fd=1, pipe_handle=2)",
        "--multiprocessing-fork",
    ]
    monkeypatch.setattr(
        workers.native,
        "_state",
        lambda pid, _: (1, arguments if pid == 1 else child_args, b"", executable, cwd),
    )
    psutil = SimpleNamespace(
        Process=lambda pid: SimpleNamespace(
            children=lambda recursive=False: [SimpleNamespace(pid=2)] if pid == 1 else [],
            ppid=lambda: 1,
            environ=lambda: {},
        )
    )
    with pytest.raises(PreflightError, match="python_worker_isolation_required"):
        workers.observe_workers(
            1,
            psutil,
            [hash_file(executable), hash_file(Path(BOOTSTRAP))],
            {},
        )
