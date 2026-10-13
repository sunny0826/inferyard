"""Memory overrides must be frozen, auditable, and independent of temperature rules."""

import hashlib
import shutil
import struct
from copy import deepcopy
from types import SimpleNamespace

import pytest

from inferyard.application.types import CommandRequest, CommandResult
from inferyard.cli import main
from inferyard.config.engine_fit import prepare, validate_plan
from inferyard.contracts.validation import ContractError
from inferyard.evidence.storage import EvidenceError
from inferyard.platforms.identity import PreflightError
from inferyard.reporting.engine_fit import compare, seal_run, verify
from inferyard.reporting.engine_fit_html import render
from inferyard.reporting.engine_fit_validation import validate_run
from tests.unit import test_engine_fit_temperature_override as temperature
from tests.unit.test_engine_fit_common_evidence import common_data
from tests.unit.test_engine_fit_report import _replace
from tests.unit.test_engine_fit_report import plan as legacy_plan

REASON = '本次暂停内存停止 <script>temporary</script> & "诊断"'
LIMITATION = "memory_stop_explicitly_disabled"
model = temperature.model
guard_data = temperature.guard_data


def _data(engine="lmstudio", *, both=False, reason=REASON):
    plan, run, rows = temperature._override_data(engine) if both else common_data(engine)
    plan["definition"] = "engine_fit_plan.v4"
    plan["parameters"].update(
        min_free_memory_bytes=None,
        memory_stop_override_reason=reason,
        temperature_stop_override_reason=temperature.REASON if both else None,
    )
    temperature._rehash(plan)
    run.update(definition="engine_fit_run.v5", plan_id=plan["plan_id"])
    run["limitations"].append(LIMITATION)
    return plan, run, rows


@pytest.mark.parametrize("kind", ["directory", "gguf"])
@pytest.mark.parametrize("both", [False, True])
def test_plan_freezes_memory_reason_and_optional_temperature_rule(model, tmp_path, kind, both):
    if kind == "gguf":
        model = tmp_path / "synthetic.gguf"
        model.write_bytes(b"GGUF" + struct.pack("<IQQ", 3, 1, 0) + b"synthetic")
    plan = prepare(
        CommandRequest(
            "engine-fit plan",
            model_path=model,
            out=tmp_path / "plan",
            fit_memory_stop_override_reason=REASON,
            fit_temperature_stop_override_reason=temperature.REASON if both else None,
        )
    )
    assert plan["definition"] == "engine_fit_plan.v4"
    assert plan["model"]["kind"] == kind
    assert temperature.runtime._manifest(plan) == plan["model"]
    assert plan["parameters"]["min_free_memory_bytes"] is None
    assert plan["parameters"]["memory_stop_override_reason"] == REASON
    assert plan["parameters"]["max_temperature_celsius"] == (None if both else 85)
    changed = deepcopy(plan)
    changed["parameters"]["memory_stop_override_reason"] += " revised"
    with pytest.raises(ContractError):
        validate_plan(changed)
    temperature._rehash(changed)
    assert validate_plan(changed)["plan_id"] != plan["plan_id"]


@pytest.mark.parametrize("reason", ["", " \t", False, 1, [], "温" * 342, "\ud800"])
def test_invalid_memory_reason_refused_before_hashing(model, tmp_path, monkeypatch, reason):
    from inferyard.config import engine_fit_assets

    monkeypatch.setattr(
        engine_fit_assets, "model_asset_manifest", lambda *_: pytest.fail("reached model hashing")
    )
    with pytest.raises(ContractError, match="memory_stop_override_reason"):
        prepare(
            CommandRequest(
                "engine-fit plan",
                model_path=model,
                out=tmp_path / "plan",
                fit_memory_stop_override_reason=reason,
            )
        )
    assert not (tmp_path / "plan").exists()


@pytest.mark.parametrize("version", [1, 2, 3])
@pytest.mark.parametrize("change", ["null", "reason"])
def test_old_plans_refuse_memory_override(version, change):
    plan = (
        legacy_plan.__wrapped__()
        if version == 1
        else common_data()[0]
        if version == 2
        else temperature._override_data()[0]
    )
    if change == "null":
        plan["parameters"]["min_free_memory_bytes"] = None
    else:
        plan["parameters"]["memory_stop_override_reason"] = REASON
    temperature._rehash(plan)
    with pytest.raises(ContractError):
        validate_plan(plan)


@pytest.mark.parametrize(
    "change",
    [
        "missing_memory",
        "missing_temperature",
        "blank",
        "null",
        "number",
        "threshold",
        "temperature_null",
        "temperature_reason",
        "temperature_boolean",
    ],
)
def test_v4_refuses_missing_or_contradictory_stop_rules(change):
    plan, _, _ = _data()
    parameters = plan["parameters"]
    if change.startswith("missing_"):
        del parameters[change.removeprefix("missing_") + "_stop_override_reason"]
    elif change == "threshold":
        parameters["min_free_memory_bytes"] = 512 * 2**20
    elif change == "temperature_null":
        parameters["max_temperature_celsius"] = None
    elif change.startswith("temperature_"):
        parameters["temperature_stop_override_reason"] = (
            REASON if change.endswith("reason") else False
        )
    else:
        parameters["memory_stop_override_reason"] = {"blank": " ", "null": None, "number": 1}[
            change
        ]
    temperature._rehash(plan)
    with pytest.raises(ContractError):
        validate_plan(plan)


def test_cli_freezes_reason_and_refuses_missing_value_or_conflicting_threshold(tmp_path):
    requests = []

    def handler(request):
        requests.append(request)
        return 0, CommandResult(request.command, "tested")

    arguments = ["engine-fit", "plan", "--model", str(tmp_path), "--out", str(tmp_path / "plan")]
    handlers = {"engine-fit plan": handler}
    assert main(arguments + ["--skip-memory-stop", REASON], handlers=handlers) == 0
    assert requests[0].fit_memory_stop_override_reason == REASON
    assert main(arguments + ["--skip-memory-stop"], handlers=handlers) == 2
    assert (
        main(
            arguments + ["--skip-memory-stop", REASON, "--min-free-memory-mib", "512"],
            handlers=handlers,
        )
        == 2
    )
    assert len(requests) == 1
    assert CommandRequest("engine-fit plan").fit_memory_stop_override_reason is None
    assert (
        main(
            ["engine-fit", "run", "--skip-memory-stop", REASON],
            handlers={"engine-fit run": handler},
        )
        == 2
    )
    assert len(requests) == 1


@pytest.mark.parametrize("available", [1, None, 2**40])
def test_disabled_memory_is_still_collected_with_missing_reasons(guard_data, available):
    guard, sample = guard_data
    guard.parameters["min_free_memory_bytes"] = None
    sample["memory_available_bytes"] = available
    if available is None:
        sample["missing_reasons"]["memory_available_bytes"] = "synthetic_unavailable"
    result = guard.check("before_run")
    assert result["memory_available_bytes"] == available
    assert result["missing_reasons"] == sample["missing_reasons"]
    assert result["temperature_samples"] == sample["temperature_samples"]


@pytest.mark.parametrize("failure", ["disk", "identity", "temperature"])
def test_memory_override_keeps_other_conditions(guard_data, monkeypatch, failure):
    guard, sample = guard_data
    guard.parameters["min_free_memory_bytes"] = None
    sample["memory_available_bytes"] = 1
    expected = failure + "_safety_threshold_reached"
    if failure == "disk":
        monkeypatch.setattr(
            temperature.runtime.shutil, "disk_usage", lambda _: SimpleNamespace(free=1)
        )
    elif failure == "temperature":
        guard.parameters["max_temperature_celsius"] = 85
        sample["temperature_samples"][0].update(value=96.5, raw_value=96.5)
    else:
        expected = "service_identity_changed"

        def changed(_binding):
            raise PreflightError(expected)

        monkeypatch.setattr(temperature.runtime, "check_service", changed)
    with pytest.raises(PreflightError, match=expected):
        guard.check("before_run")


@pytest.mark.parametrize("change", ["old_run", "missing_memory", "extra_temperature"])
def test_v5_requires_matching_plan_and_limitations(change):
    plan, run, rows = _data()
    if change == "old_run":
        run["definition"] = "engine_fit_run.v4"
    elif change == "missing_memory":
        run["limitations"].remove(LIMITATION)
    else:
        run["limitations"].append(temperature.LIMITATION)
    with pytest.raises(EvidenceError):
        validate_run(plan, run, rows)


@pytest.mark.parametrize("both", [False, True])
@pytest.mark.parametrize("reason", ["memory_safety_threshold_reached", "system_memory_unavailable"])
def test_disabled_stop_reason_is_refused_even_with_coherent_seal(tmp_path, both, reason):
    plan, run, rows = _data(both=both)
    root = tmp_path / "sealed"
    root.mkdir()
    seal_run(root, plan, run, rows)
    rows[-1].update(status="failed", reason="disk_safety_threshold_reached", response=None)
    run.update(completeness="incomplete", stop_reason="disk_safety_threshold_reached")
    run["counts"].update(completed=1, failed=1)
    rejected = tmp_path / "rejected"
    rejected.mkdir()
    rows[-1]["reason"] = reason
    with pytest.raises(EvidenceError, match="memory_override"):
        seal_run(rejected, plan, run, rows)
    _replace(root, "requests.json", rows)
    _replace(root, "run.json", run)
    _replace(root, "report.html", render([dict(plan=plan, run=run, requests=rows)]).encode())
    with pytest.raises(EvidenceError, match="memory_override"):
        verify(root)
    rows[-1]["reason"] = "disk_safety_threshold_reached"
    run["stop_reason"] = reason
    with pytest.raises(EvidenceError, match="memory_override"):
        validate_run(plan, run, rows)


@pytest.mark.parametrize("version", [2, 3])
def test_legacy_run_cannot_claim_memory_override(version):
    plan, run, rows = common_data() if version == 2 else temperature._override_data()
    run["limitations"].append(LIMITATION)
    with pytest.raises(EvidenceError, match="memory_override"):
        validate_run(plan, run, rows)


@pytest.mark.parametrize("both", [False, True])
def test_pair_copies_sources_and_displays_escaped_reasons_offline(tmp_path, both):
    paths = []
    for engine in ("llama-cpp", "lmstudio"):
        plan, run, rows = _data(engine, both=both)
        root = tmp_path / engine
        root.mkdir()
        seal_run(root, plan, run, rows)
        assert verify(root)["run"] == run
        paths.append(root)
    out = tmp_path / "comparison"
    expected = compare(paths, out)
    for path in paths:
        shutil.rmtree(path)
    assert verify(out)["comparison"] == expected
    html = (out / "report.html").read_text()
    assert "本次已显式跳过内存停止" in html
    assert "&lt;script&gt;temporary&lt;/script&gt;" in html and "<script>" not in html
    assert ("本次已显式跳过温度停止" in html) == both
    assert ("温度停止上限仍为 85°C" in html) != both
    assert "缺测 · lmstudio_service_version_not_exposed" in html


@pytest.mark.parametrize("other", ["old", "reason", "temperature"])
def test_comparison_refuses_mixed_frozen_stopping_conditions(tmp_path, other):
    entries = [_data("llama-cpp")]
    entries.append(
        common_data()
        if other == "old"
        else _data(reason="other" if other == "reason" else REASON, both=other == "temperature")
    )
    paths = []
    for index, (plan, run, rows) in enumerate(entries):
        root = tmp_path / str(index)
        root.mkdir()
        seal_run(root, plan, run, rows)
        paths.append(root)
    with pytest.raises(EvidenceError, match="plan_mismatch"):
        compare(paths, tmp_path / "comparison")


def test_old_temperature_override_html_bytes_remain_unchanged():
    plan, run, rows = temperature._override_data()
    html = render([dict(plan=plan, run=run, requests=rows)]).encode()
    # Captured with the HEAD renderer before ADR 019, not the modified renderer.
    assert hashlib.sha256(html).hexdigest() == (
        "b114bb30dc8d61a7fda2b87a968246c7629267d47f2e3041aa155177f4b48ee1"
    )
