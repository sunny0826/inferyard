"""Explicit temperature overrides preserve old evidence and all other safeguards."""

import hashlib
import shutil
import struct
from copy import deepcopy
from types import SimpleNamespace

import pytest

from inferyard.application.types import CommandRequest, CommandResult
from inferyard.cli import main
from inferyard.config.engine_fit import digest, prepare, validate_plan
from inferyard.contracts.validation import ContractError
from inferyard.evidence.storage import EvidenceError
from inferyard.platforms.identity import PreflightError
from inferyard.reporting.engine_fit import compare, seal_run, verify
from inferyard.reporting.engine_fit_html import render
from inferyard.reporting.engine_fit_validation import validate_run
from inferyard.runtime import engine_fit as runtime
from tests.unit.test_engine_fit_common_evidence import common_data
from tests.unit.test_engine_fit_native_evidence import native_data
from tests.unit.test_engine_fit_report import _data, _replace
from tests.unit.test_engine_fit_report import plan as legacy_plan

REASON = 'operator approved <script>alert("temporary")</script> & 短时诊断'
LIMITATION = "temperature_stop_explicitly_disabled"


def _rehash(plan):
    plan["plan_id"] = digest({key: value for key, value in plan.items() if key != "plan_id"})


def _override_data(engine="lmstudio", reason=REASON):
    plan, run, rows = common_data(engine)
    plan["definition"] = "engine_fit_plan.v3"
    plan["parameters"].update(max_temperature_celsius=None, temperature_stop_override_reason=reason)
    _rehash(plan)
    run.update(definition="engine_fit_run.v4", plan_id=plan["plan_id"])
    run["limitations"].append(LIMITATION)
    return plan, run, rows


@pytest.fixture
def model(tmp_path, monkeypatch):
    from inferyard.platforms import engine_fit

    monkeypatch.setattr(
        engine_fit, "host_identity", lambda: {"sha256": "a" * 64, "platform": "Darwin"}
    )
    path = tmp_path / "model"
    path.mkdir()
    (path / "config.json").write_text('{"model_type":"synthetic"}')
    (path / "model.safetensors").write_bytes(b"synthetic; never loaded")
    return path


@pytest.mark.parametrize("kind", ["directory", "gguf"])
def test_explicit_override_freezes_reason_and_modern_asset_manifest(model, tmp_path, kind):
    if kind == "gguf":
        model = tmp_path / "model.gguf"
        model.write_bytes(b"GGUF" + struct.pack("<IQQ", 3, 1, 0) + b"synthetic")
    plan = prepare(
        CommandRequest(
            "engine-fit plan",
            model_path=model,
            out=tmp_path / "override-plan",
            fit_temperature_stop_override_reason=REASON,
        )
    )
    assert plan["definition"] == "engine_fit_plan.v3"
    assert plan["model"]["kind"] == kind
    assert plan["parameters"]["max_temperature_celsius"] is None
    assert plan["parameters"]["temperature_stop_override_reason"] == REASON
    assert runtime._manifest(plan) == plan["model"]
    changed = deepcopy(plan)
    changed["parameters"]["temperature_stop_override_reason"] += " changed"
    with pytest.raises(ContractError):
        validate_plan(changed)
    _rehash(changed)
    assert changed["plan_id"] != plan["plan_id"]
    assert validate_plan(changed) == changed


def test_default_request_retains_legacy_plan_and_temperature_limit(model, tmp_path):
    request = CommandRequest("engine-fit plan", model_path=model, out=tmp_path / "plan")
    assert request.fit_temperature_stop_override_reason is None
    plan = prepare(request)
    assert plan["definition"] == "engine_fit_plan.v1"
    assert "kind" not in plan["model"]
    assert plan["parameters"]["max_temperature_celsius"] == 85
    assert "temperature_stop_override_reason" not in plan["parameters"]


@pytest.mark.parametrize("reason", ["", " \t\n", False, 1, [], {}, "a" * 1025, "温" * 342])
def test_invalid_reason_rejected_before_model_hashing(model, tmp_path, monkeypatch, reason):
    from inferyard.config import engine_fit_assets

    monkeypatch.setattr(
        engine_fit_assets,
        "model_asset_manifest",
        lambda *_args: pytest.fail("invalid reason reached model hashing"),
    )
    with pytest.raises(ContractError, match="temperature_stop_override_reason"):
        prepare(
            CommandRequest(
                "engine-fit plan",
                model_path=model,
                out=tmp_path / "plan",
                fit_temperature_stop_override_reason=reason,
            )
        )
    assert not (tmp_path / "plan").exists()


@pytest.mark.parametrize("reason", ["a" * 1024, "温" * 341, " reason with spaces "])
def test_reason_size_is_utf8_bytes_without_silently_normalizing(reason):
    plan, _, _ = _override_data(reason=reason)
    assert validate_plan(plan)["parameters"]["temperature_stop_override_reason"] == reason


@pytest.mark.parametrize("version", ["engine_fit_plan.v1", "engine_fit_plan.v2"])
@pytest.mark.parametrize("change", ["null", "reason"])
def test_old_plan_versions_reject_override_rules(version, change):
    plan = legacy_plan.__wrapped__() if version.endswith("v1") else common_data()[0]
    if change == "null":
        plan["parameters"]["max_temperature_celsius"] = None
    else:
        plan["parameters"]["temperature_stop_override_reason"] = REASON
    _rehash(plan)
    with pytest.raises(ContractError):
        validate_plan(plan)


@pytest.mark.parametrize("change", ["missing", "null", "blank", "long", "number", "threshold"])
def test_v3_requires_exact_override_parameters(change):
    plan, _, _ = _override_data()
    if change == "missing":
        del plan["parameters"]["temperature_stop_override_reason"]
    elif change == "threshold":
        plan["parameters"]["max_temperature_celsius"] = 85
    else:
        plan["parameters"]["temperature_stop_override_reason"] = {
            "null": None,
            "blank": " ",
            "long": "温" * 342,
            "number": 123,
        }[change]
    _rehash(plan)
    with pytest.raises(ContractError):
        validate_plan(plan)


def test_cli_plan_reason_reaches_shared_request_and_missing_value_is_rejected(tmp_path):
    requests = []

    def handler(request):
        requests.append(request)
        return 0, CommandResult(request.command, "tested")

    arguments = ["engine-fit", "plan", "--model", str(tmp_path), "--out", str(tmp_path / "plan")]
    assert (
        main(arguments + ["--skip-temperature-stop", REASON], handlers={"engine-fit plan": handler})
        == 0
    )
    assert requests[0].fit_temperature_stop_override_reason == REASON
    assert main(arguments + ["--skip-temperature-stop"], handlers={"engine-fit plan": handler}) == 2
    assert len(requests) == 1


def test_cli_run_cannot_change_frozen_temperature_rules(tmp_path):
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
                "synthetic",
                "--out",
                str(tmp_path / "run"),
                "--skip-temperature-stop",
                REASON,
            ],
            handlers={
                "engine-fit run": lambda _request: pytest.fail("run override reached execution")
            },
        )
        == 2
    )


@pytest.fixture
def guard_data(tmp_path, monkeypatch):
    from inferyard.platforms import sensors_macos

    plan, run, _ = _override_data()
    sample = deepcopy(native_data.__wrapped__()[1]["resources"][0])

    class Sensors:
        sources = [{"metric_name": "temperature"}]

        def collect(self, _phase, _request):
            return deepcopy(sample["temperature_samples"])

    monkeypatch.setattr(runtime.platform, "system", lambda: "Darwin")
    monkeypatch.setattr(sensors_macos, "MacSensors", Sensors)
    monkeypatch.setattr(runtime, "check_service", lambda _binding: None)
    monkeypatch.setattr(runtime, "resource_snapshot", lambda _binding: deepcopy(sample))
    monkeypatch.setattr(runtime.shutil, "disk_usage", lambda _out: SimpleNamespace(free=2**40))
    return runtime.Guard(run["binding"], plan, tmp_path), sample


@pytest.mark.parametrize("temperature", [42.5, 85, 96.5, None])
def test_override_collects_cold_hot_and_missing_temperature_without_stopping(
    guard_data, temperature
):
    guard, sample = guard_data
    sample["temperature_samples"][0].update(raw_value=temperature, value=temperature)
    sample["temperature_samples"][0]["missing_reason"] = (
        "source_unavailable" if temperature is None else None
    )
    result = guard.check("before_run")
    assert result["temperature_samples"] == sample["temperature_samples"]
    assert result["temperature_missing_reason"] == (
        "no_verified_temperature_source" if temperature is None else None
    )


@pytest.mark.parametrize("failure", ["memory", "memory_missing", "disk", "identity"])
def test_override_keeps_other_stops(guard_data, monkeypatch, failure):
    guard, sample = guard_data
    expected = failure + "_safety_threshold_reached"
    if failure == "memory":
        sample["memory_available_bytes"] = 1
    elif failure == "memory_missing":
        sample["memory_available_bytes"] = None
        expected = "system_memory_unavailable"
    elif failure == "disk":
        monkeypatch.setattr(runtime.shutil, "disk_usage", lambda _out: SimpleNamespace(free=1))
    else:
        expected = "service_identity_changed"

        def changed(_binding):
            raise PreflightError(expected)

        monkeypatch.setattr(runtime, "check_service", changed)
    with pytest.raises(PreflightError, match=expected):
        guard.check("before_run")


def test_default_guard_still_stops_at_85_and_keeps_the_reading(guard_data):
    guard, sample = guard_data
    guard.parameters["max_temperature_celsius"] = 85
    sample["temperature_samples"][0].update(raw_value=85, value=85)
    with pytest.raises(PreflightError, match="temperature_safety_threshold_reached"):
        guard.check("before_run")
    assert guard.last_sample["temperature_samples"][0]["value"] == 85


@pytest.mark.parametrize(
    "change", ["old_run", "old_plan", "missing_limitation", "temperature_stop"]
)
def test_run_v4_requires_plan_v3_and_explicit_limitation(change):
    plan, run, rows = _override_data()
    if change == "old_run":
        run["definition"] = "engine_fit_run.v3"
    elif change == "old_plan":
        plan = common_data()[0]
    elif change == "missing_limitation":
        run["limitations"].remove(LIMITATION)
    else:
        run["stop_reason"] = "temperature_safety_threshold_reached"
    with pytest.raises(EvidenceError):
        validate_run(plan, run, rows)


@pytest.mark.parametrize("status", ["cancelled", "failed", "invalid", "not_executed"])
def test_v4_temperature_row_reason_rejected_by_seal_and_offline_verify(tmp_path, status):
    plan, run, rows = _override_data()
    rows[-1].update(status=status, reason="memory_safety_threshold_reached", response=None)
    run.update(completeness="incomplete", stop_reason="memory_safety_threshold_reached")
    run["counts"].update(completed=1, **{status: 1})
    sealed = tmp_path / "sealed"
    sealed.mkdir()
    seal_run(sealed, plan, run, rows)
    assert verify(sealed)["run"] == run
    rows[-1]["reason"] = "temperature_safety_threshold_reached"
    rejected = tmp_path / "rejected"
    rejected.mkdir()
    with pytest.raises(EvidenceError, match="temperature_override"):
        seal_run(rejected, plan, run, rows)
    # Hashes and HTML are coherent: the refusal must come from semantic validation.
    _replace(sealed, "requests.json", rows)
    _replace(sealed, "report.html", render([dict(plan=plan, run=run, requests=rows)]).encode())
    with pytest.raises(EvidenceError, match="temperature_override"):
        verify(sealed)


@pytest.mark.parametrize("version", ["v1", "v2", "v3"])
def test_legacy_disabled_limitation_rejected_by_seal_and_offline_verify(tmp_path, version):
    if version == "v1":
        plan = legacy_plan.__wrapped__()
        run, rows = _data(plan)
    elif version == "v2":
        plan, run, rows = native_data.__wrapped__()
    else:
        plan, run, rows = common_data()
    sealed = tmp_path / "sealed"
    sealed.mkdir()
    seal_run(sealed, plan, run, rows)
    assert verify(sealed)["run"] == run
    run["limitations"].append(LIMITATION)
    rejected = tmp_path / "rejected"
    rejected.mkdir()
    with pytest.raises(EvidenceError, match="temperature_override"):
        seal_run(rejected, plan, run, rows)
    _replace(sealed, "run.json", run)
    _replace(sealed, "report.html", render([dict(plan=plan, run=run, requests=rows)]).encode())
    with pytest.raises(EvidenceError, match="temperature_override"):
        verify(sealed)


def test_override_pair_seals_compares_and_verifies_copied_sources_offline(tmp_path, monkeypatch):
    monkeypatch.setattr(runtime.platform, "system", lambda: "Windows")
    paths = []
    for engine in ("llama-cpp", "lmstudio"):
        plan, run, rows = _override_data(engine)
        path = tmp_path / engine
        path.mkdir()
        seal_run(path, plan, run, rows)
        assert verify(path)["run"] == run
        paths.append(path)
    out = tmp_path / "comparison"
    expected = compare(paths, out)
    for path in paths:
        shutil.rmtree(path)
    assert verify(out)["comparison"] == expected
    html = (out / "report.html").read_text()
    assert "本次已显式跳过温度停止" in html
    assert "&lt;script&gt;" in html and "<script>" not in html
    assert "缺测 · lmstudio_service_version_not_exposed" in html and "<td>None</td>" not in html


@pytest.mark.parametrize("other", ["legacy", "different_reason"])
def test_comparison_refuses_mixed_temperature_rules_or_reasons(tmp_path, other):
    entries = [_override_data("llama-cpp")]
    entries.append(common_data() if other == "legacy" else _override_data(reason="other approval"))
    paths = []
    for index, (plan, run, rows) in enumerate(entries):
        path = tmp_path / str(index)
        path.mkdir()
        seal_run(path, plan, run, rows)
        paths.append(path)
    with pytest.raises(EvidenceError, match="plan_mismatch"):
        compare(paths, tmp_path / "comparison")


@pytest.mark.parametrize(
    "version,expected",
    [
        ("v1", "147f701083c1d8e647ef87bc5ebdc71d10c5c5ec791ed98536edc8d783b350b0"),
        ("v2", "f05c6b609511dcedeeda69819934d076b0f21b7e3cc76d6561047b45a006f885"),
        ("v3", "0534776daa2c20696582128924e2e20d708365d154de51fe4dad453129120867"),
        ("comparison", "9b73d788c159b5b93db4a8652ffec38ef807fefbd878ca7c308e10aa0cde3295"),
    ],
)
def test_old_html_bytes_and_sealed_evidence_remain_readable(tmp_path, version, expected):
    # Hashes captured from the pre-ADR-016 renderer, not recomputed from the new behavior.
    if version == "v1":
        plan = legacy_plan.__wrapped__()
        run, rows = _data(plan)
        entries = [(plan, run, rows)]
    elif version == "v2":
        entries = [native_data.__wrapped__()]
    elif version == "v3":
        entries = [common_data()]
    else:
        entries = [common_data("llama-cpp"), common_data()]
    data = [dict(plan=plan, run=run, requests=rows) for plan, run, rows in entries]
    html = render(data, comparison=version == "comparison").encode()
    assert hashlib.sha256(html).hexdigest() == expected
    paths = []
    for index, (plan, run, rows) in enumerate(entries):
        path = tmp_path / str(index)
        path.mkdir()
        seal_run(path, plan, run, rows)
        assert verify(path)["run"] == run
        paths.append(path)
    if version == "comparison":
        compare(paths, tmp_path / "comparison")
        assert (tmp_path / "comparison/report.html").read_bytes() == html
        verify(tmp_path / "comparison")
