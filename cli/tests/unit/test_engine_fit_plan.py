"""Plan rejection paths and lightweight CLI dispatch for engine diagnostics."""

from copy import deepcopy

import pytest

from inferyard.application.types import CommandRequest, CommandResult
from inferyard.cli import main
from inferyard.config.engine_fit import digest, load_plan, prepare, request_rows, validate_plan
from inferyard.contracts.validation import ContractError
from inferyard.evidence.storage import EvidenceError
from inferyard.platforms.identity import PreflightError


@pytest.fixture
def plan(tmp_path, monkeypatch):
    from inferyard.platforms import engine_fit

    monkeypatch.setattr(engine_fit, "host_identity", lambda: {"sha256": "a" * 64})
    model = tmp_path / "model"
    model.mkdir()
    (model / "config.json").write_text('{"model_type":"fixture"}')
    (model / "model.safetensors").write_bytes(b"synthetic-model-not-real-weights")
    return prepare(CommandRequest("engine-fit plan", model_path=model, out=tmp_path / "plan"))


def test_plan_freezes_full_directory_and_order(plan, tmp_path):
    assert load_plan(tmp_path / "plan/plan.json") == plan
    assert plan["request_count"] == 3
    assert plan["max_request_wall_seconds"] == 180
    assert [r["case_id"] for r in request_rows(plan)] == [c["id"] for c in plan["cases"]]
    assert [f["path"] for f in plan["model"]["files"]] == ["config.json", "model.safetensors"]


@pytest.mark.parametrize(
    ("field", "value"),
    [
        ("max_tokens", True),
        ("max_tokens", 0),
        ("request_timeout_seconds", float("nan")),
        ("request_timeout_seconds", float("inf")),
        ("repetitions", True),
        ("min_free_memory_bytes", 1),
        ("max_temperature_celsius", 500),
    ],
)
def test_plan_rejects_unsafe_parameters(plan, field, value):
    changed = deepcopy(plan)
    changed["parameters"][field] = value
    if isinstance(value, float) and not value < float("inf"):
        with pytest.raises((ContractError, ValueError)):
            changed["plan_id"] = digest({k: v for k, v in changed.items() if k != "plan_id"})
            validate_plan(changed)
        return
    changed["plan_id"] = digest({k: v for k, v in changed.items() if k != "plan_id"})
    with pytest.raises(ContractError):
        validate_plan(changed)


def test_changed_prompt_and_model_manifest_hash_are_rejected(plan):
    changed = deepcopy(plan)
    changed["cases"][0]["prompt"] = "tampered"
    with pytest.raises(ContractError):
        validate_plan(changed)
    changed = deepcopy(plan)
    changed["model"]["files"][0]["sha256"] = "b" * 64
    changed["plan_id"] = digest({k: v for k, v in changed.items() if k != "plan_id"})
    with pytest.raises(ContractError):
        validate_plan(changed)


def test_duplicate_keys_are_not_loaded(tmp_path):
    path = tmp_path / "bad.json"
    path.write_text('{"schema_version":3,"schema_version":3}')
    with pytest.raises(EvidenceError):
        load_plan(path)


def test_model_root_directory_symlink_is_rejected(plan, tmp_path):
    link = tmp_path / "linked-model"
    link.symlink_to(tmp_path / "model", target_is_directory=True)
    with pytest.raises(PreflightError, match="directory_symlink"):
        prepare(CommandRequest("engine-fit plan", model_path=link, out=tmp_path / "linked-plan"))


def test_foreign_platform_model_paths_are_valid_offline(plan):
    changed = deepcopy(plan)
    changed["model"]["path"] = "C:\\Models\\fixture"
    changed["plan_id"] = digest({k: v for k, v in changed.items() if k != "plan_id"})
    assert validate_plan(changed) == changed


def test_cli_run_bindings_reach_shared_request_type(capsys, tmp_path):
    requests = []

    def handler(request):
        requests.append(request)
        return 0, CommandResult(request.command, "tested")

    assert (
        main(
            [
                "engine-fit",
                "run",
                "--plan",
                str(tmp_path / "plan.json"),
                "--engine",
                "vllm",
                "--endpoint-url",
                "http://127.0.0.1:8000",
                "--server-pid",
                "42",
                "--served-model",
                "fixture",
                "--out",
                str(tmp_path / "run"),
            ],
            handlers={"engine-fit run": handler},
        )
        == 0
    )
    assert requests[0].fit_engine == "vllm"
    assert requests[0].server_pid == 42
    assert requests[0].output_root is None
    assert capsys.readouterr().err == ""


def test_cli_rejects_remote_service_without_calling_handler(capsys, tmp_path):
    def handler(request):
        pytest.fail("remote service reached execution")

    assert (
        main(
            [
                "engine-fit",
                "run",
                "--plan",
                str(tmp_path / "plan.json"),
                "--engine",
                "vllm",
                "--endpoint-url",
                "https://example.com",
                "--server-pid",
                "42",
                "--served-model",
                "fixture",
                "--out",
                str(tmp_path / "run"),
            ],
            handlers={"engine-fit run": handler},
        )
        == 2
    )
    assert "invalid_input" in capsys.readouterr().out
