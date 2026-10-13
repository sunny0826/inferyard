"""Exercise exported CI fixtures outside checkout, with isolated host roots only.

The package copy checks import isolation; it is not a built-wheel installation receipt.
"""

import json
import os
import shutil
import subprocess
import sys
from pathlib import Path

import pytest

import inferyard
from tests.packaging import ci_installed, installed_host_state, prepare_inputs

DISPOSABLE = {
    "GITHUB_ACTIONS": "true",
    "RUNNER_ENVIRONMENT": "github-hosted",
    "LAB_DISPOSABLE_HOST_TEST": "yes",
}


@pytest.fixture
def exported(tmp_path, monkeypatch):
    inputs = tmp_path / "inputs"
    monkeypatch.setattr(sys, "argv", ["prepare-inputs", "--out", str(inputs)])
    prepare_inputs.main()
    site = tmp_path / "isolated/site-packages"
    shutil.copytree(
        Path(inferyard.__file__).parent,
        site / "inferyard",
        ignore=shutil.ignore_patterns("__pycache__"),
    )
    return inputs, site


@pytest.mark.parametrize("missing_helper", [False, True])
def test_exported_scenario_builds_under_isolated_python(exported, tmp_path, missing_helper):
    inputs, site = exported
    tests = inputs / "ci-tests/tests"
    if missing_helper:
        (tests / "host_state_helpers.py").unlink()
    script = """
import json, sys
from pathlib import Path
inputs, site, work = map(Path, sys.argv[1:])
sys.path.insert(0, str(site))
sys.path.insert(0, str(inputs / 'ci-tests'))
import inferyard, pytest, tests
assert Path(inferyard.__file__).is_relative_to(site)
assert Path(tests.__file__).is_relative_to(inputs / 'ci-tests')
from tests.integration.test_runner import scenario
work.mkdir()
with pytest.MonkeyPatch.context() as patch:
    request, deps, calls, _ = scenario.__wrapped__(
        work, patch, inputs / 'ci-tests/tests/fixtures/config/valid.toml')
    import tests.host_state_helpers as helper
    import inferyard.runtime.lock as lock
    assert Path(helper.__file__).is_relative_to(inputs / 'ci-tests')
    assert lock.STATE_PATH.parent == work
    with lock.HostLock() as held:
        assert 'migration' not in held.state
        assert held.state['dirty'] is False
    assert request.command == 'run' and calls == []
print(json.dumps({'scenario_constructed': True, 'requests_sent': 0}))
"""
    result = subprocess.run(
        [sys.executable, "-I", "-c", script, str(inputs), str(site), str(tmp_path / "scenario")],
        cwd=tmp_path,
        capture_output=True,
        text=True,
        check=False,
        timeout=30,
    )
    if missing_helper:
        assert result.returncode != 0
        assert "No module named 'tests.host_state_helpers'" in result.stderr
    else:
        assert result.returncode == 0, result.stderr
        assert json.loads(result.stdout) == {"scenario_constructed": True, "requests_sent": 0}


def isolated_script(source, out, site, host_root):
    """Inject temporary paths before any production harness call, including its children."""
    shim = f"""
import sys
from pathlib import Path
sys.path.insert(0, {str(site)!r})
import inferyard.runtime.lock as locking
locking.LOCK_PATH = Path({str(host_root / "inferyard-host.lock")!r})
locking.STATE_PATH = Path({str(host_root / "inferyard-host.state.json")!r})
"""
    out.write_text(shim + source.read_text(encoding="utf-8"), encoding="utf-8")
    return out


def test_disposable_sequence_initializes_once_then_preserves_crash_dirty(
    exported, tmp_path, monkeypatch
):
    inputs, site = exported
    out, host = tmp_path / "matrix", tmp_path / "host"
    (out / "empty-cwd").mkdir(parents=True)
    host.mkdir()
    shutil.copytree(inputs, out / "inputs/fixtures")
    directory = Path(prepare_inputs.__file__).parent
    request = isolated_script(
        directory / "installed_request_chain.py", out / "request.py", site, host
    )
    state = isolated_script(directory / "installed_host_state.py", out / "host.py", site, host)
    for key, value in DISPOSABLE.items():
        monkeypatch.setenv(key, value)
    observed = []
    run = subprocess.run

    def record(command, **options):
        result = run(command, **options, capture_output=True, text=True)
        observed.append(
            (
                command,
                (host / "inferyard-host.state.json").read_bytes(),
            )
        )
        return result

    monkeypatch.setattr(ci_installed.subprocess, "run", record)
    ci_installed.run_host_checks(
        sys.executable, sys.executable, request, state, out, os.environ.copy()
    )
    assert len(observed) == 4
    assert "--initialize" in observed[0][0]
    assert observed[1][0][observed[1][0].index("--mode") + 1] == "success"
    assert "--initialize" not in observed[2][0] and "--mode" not in observed[2][0]
    assert observed[3][0][observed[3][0].index("--mode") + 1] == "dirty"
    assert [json.loads(row[1])["dirty"] for row in observed] == [False, False, True, True]
    assert observed[2][1:] == observed[3][1:]
    assert not (host / "inferyard-host-migration.json").exists()
    assert not (host / "local-ai-benchmark-host.state.json").exists()
    initialization = json.loads((out / "host-initialization.json").read_bytes())
    assert initialization["auto_initialization"] is True
    assert "explicit_initialization" not in initialization
    assert initialization["result"]["status"] == "initialized"
    success = json.loads((out / "request-chain-success.json").read_bytes())
    dirty = json.loads((out / "request-chain-dirty.json").read_bytes())
    assert success["code"] == 0 and success["synthetic_requests"] == 8
    assert dirty["code"] == 2 and dirty["synthetic_requests"] == 0
    assert "dirty_service_requires_bound_recovery" in dirty["result"]["limitations"]
    assert json.loads((out / "host-state.json").read_bytes())["dirty_preserved"] is True
    again = run(
        [sys.executable, "-I", str(state), "--initialize", "--out", str(out / "forbidden.json")],
        cwd=out / "empty-cwd",
        env=os.environ.copy(),
        capture_output=True,
        text=True,
        check=False,
        timeout=30,
    )
    assert again.returncode != 0 and "already_initialized_or_interrupted" in again.stderr
    assert (host / "inferyard-host.state.json").read_bytes() == observed[-1][1]
    assert not (out / "forbidden.json").exists()


@pytest.mark.parametrize("missing", DISPOSABLE)
def test_disposable_guard_precedes_initialization_or_subprocess(tmp_path, monkeypatch, missing):
    for key, value in DISPOSABLE.items():
        monkeypatch.setenv(key, value)
    monkeypatch.delenv(missing)

    def forbidden(*args, **kwargs):
        raise AssertionError("guard must refuse before any host operation")

    monkeypatch.setattr("inferyard.runtime.host_files.exists", forbidden)
    monkeypatch.setattr(ci_installed.subprocess, "run", forbidden)
    with pytest.raises(RuntimeError, match="requires_explicit_disposable"):
        installed_host_state.initialize(tmp_path / "init.json")
    with pytest.raises(RuntimeError, match="requires_explicit_disposable"):
        ci_installed.run_host_checks(None, None, None, None, tmp_path, {})
    assert not (tmp_path / "init.json").exists()
