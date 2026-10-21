"""Declared subprocess environments stay separate from operator launch inputs."""

import copy
from types import SimpleNamespace

import pytest

from inferyard.config.local_engines.launch import description
from inferyard.contracts.validation import ContractError
from inferyard.platforms.identity import PreflightError
from inferyard.platforms.local_engines.installation import description as installation
from inferyard.platforms.local_engines.startup import check_environment


@pytest.fixture
def declared(tmp_path):
    runtime = tmp_path / "runtime"
    runtime.mkdir()
    executable = runtime / "python"
    executable.write_bytes(b"controlled executable")
    root = dict.fromkeys(("PYTHONDONTWRITEBYTECODE", "HF_HUB_OFFLINE", "TRANSFORMERS_OFFLINE"), "1")
    return {
        "schema_version": 3,
        "definition": "local-engine-installation.v1",
        "family": "vllm",
        "executable": str(executable),
        "working_directory": str(tmp_path),
        "runtime_roots": [str(runtime)],
        "environment": root,
        "worker_environment": {
            **root,
            "VLLM_WORKER_MULTIPROC_METHOD": "spawn",
            "MLX_MAX_OPS_PER_BUFFER": "2000",
            "TRANSFORMERS_NO_ADVISORY_WARNINGS": "1",
            "VLLM_USE_V2_MODEL_RUNNER": "0",
            "VLLM_OMP_NUM_THREADS_SET_BY_VLLM": "1",
            "OMP_NUM_THREADS": "1",
        },
    }


def test_declared_worker_environment_does_not_change_root_launch(declared):
    value = installation(declared)
    check_environment(value["worker_environment"], value["worker_environment"])
    with pytest.raises(PreflightError, match="environment_override"):
        check_environment(value["worker_environment"], value["environment"])
    config = {
        "engine": {
            "family": "vllm",
            "working_directory": declared["working_directory"],
            "environment": declared["environment"],
            "startup_args": ["controlled"],
        }
    }
    launch = description(config, value)
    assert launch["environment"] == declared["environment"]
    assert "OMP_NUM_THREADS" not in launch["environment"]


@pytest.mark.parametrize(
    "key,value",
    [
        ("PYTHONPATH", "/unfrozen"),
        ("VLLM_PLUGINS", "injected"),
        ("VLLM_USE_V2_MODEL_RUNNER", "1"),
        ("LD_LIBRARY_PATH", "relative"),
        ("LD_LIBRARY_PATH", "/outside"),
        ("LD_LIBRARY_PATH", ""),
    ],
)
def test_invalid_worker_declaration_rejected(declared, key, value):
    declared["worker_environment"][key] = value
    with pytest.raises(ContractError):
        installation(declared)


def test_empty_library_path_segment_rejected(declared):
    declared["worker_environment"]["LD_LIBRARY_PATH"] = declared["runtime_roots"][0] + ":"
    with pytest.raises(ContractError):
        installation(declared)


@pytest.mark.parametrize("change", ["remove_offline", "change_offline", "wrong_family"])
def test_worker_declaration_preserves_root_contract(declared, change):
    if change == "remove_offline":
        del declared["worker_environment"]["HF_HUB_OFFLINE"]
    elif change == "change_offline":
        declared["worker_environment"]["HF_HUB_OFFLINE"] = "0"
    else:
        declared["family"] = "llama-cpp"
    with pytest.raises(ContractError):
        installation(declared)


@pytest.mark.parametrize("key", ["OMP_NUM_THREADS", "PROMETHEUS_MULTIPROC_DIR"])
def test_additional_execution_controls_require_declaration(key):
    with pytest.raises(PreflightError, match="environment_override"):
        check_environment({key: "unfrozen"}, {})


def test_worker_values_remain_exact(declared):
    expected = installation(declared)["worker_environment"]
    for key in expected:
        actual = copy.deepcopy(expected)
        actual.pop(key)
        with pytest.raises(PreflightError, match="environment_mismatch"):
            check_environment(actual, expected)
        actual[key] = "different"
        with pytest.raises(PreflightError, match="environment_mismatch"):
            check_environment(actual, expected)


@pytest.mark.parametrize("changed", [False, True])
def test_observer_requires_stable_worker_environment(tmp_path, monkeypatch, changed):
    from inferyard.platforms.identity import hash_file
    from inferyard.platforms.local_engines import workers

    executable = tmp_path / "worker"
    executable.write_bytes(b"controlled worker executable")
    identity = hash_file(executable)
    count = 0

    def environment():
        nonlocal count
        count += 1
        return {"HF_HUB_OFFLINE": "1", "PATH": "/changed" if changed and count > 1 else "/usr/bin"}

    root = SimpleNamespace(pid=1)
    child = SimpleNamespace(pid=2, ppid=lambda: 1, environ=environment)
    psutil = SimpleNamespace(Process=lambda pid: root if pid == 1 else child)
    monkeypatch.setattr(
        workers.native, "_children", lambda process: [2] if process.pid == 1 else []
    )
    monkeypatch.setattr(
        workers.native, "_state", lambda *_: (123, ["worker"], b"worker\0", executable, tmp_path)
    )
    monkeypatch.setattr(workers, "lsof_records", lambda *_: [])
    monkeypatch.setattr(workers, "mapped_file", lambda *_: True)
    monkeypatch.setattr(workers, "mapped_closure", lambda *_, **__: None)
    if changed:
        with pytest.raises(PreflightError, match="service_changed"):
            workers.observe_workers(1, psutil, [identity], {"HF_HUB_OFFLINE": "1"})
    else:
        result = workers.observe_workers(1, psutil, [identity], {"HF_HUB_OFFLINE": "1"})
        assert result[0]["pid"] == 2
