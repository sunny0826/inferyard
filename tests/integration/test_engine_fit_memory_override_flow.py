"""TCP simulation with low/missing memory and hot sensors; no model or NInfer engine."""

import shutil
from copy import deepcopy

import pytest

from inferyard.cli import main
from inferyard.evidence.storage import read_json
from tests.integration import test_engine_fit_temperature_override_flow as temperature

common_fit_service = temperature.common_fit_service
override_service = temperature.override_service
REASON = 'explicit memory override <script>reason</script> & "本次诊断"'


def _plan(fixture, *, both=True):
    root = fixture["root"] / "memory-plan"
    arguments = [
        "engine-fit",
        "plan",
        "--model",
        str(fixture["model"]),
        "--engines",
        "llama-cpp",
        "lmstudio",
        "--skip-memory-stop",
        REASON,
        "--out",
        str(root),
    ]
    if both:
        arguments.extend(["--skip-temperature-stop", temperature.REASON])
    assert main(arguments) == 0
    return root / "plan.json"


@pytest.fixture(params=[1, None])
def memory_service(override_service, monkeypatch, request):
    fixture = override_service
    snapshot = fixture["runtime"].resource_snapshot
    available = request.param

    def collect(binding):
        sample = deepcopy(snapshot(binding))
        sample["memory_available_bytes"] = available
        if available is None:
            sample["missing_reasons"]["memory_available_bytes"] = "synthetic_memory_unavailable"
        return sample

    monkeypatch.setattr(fixture["runtime"], "resource_snapshot", collect)
    return fixture, available


def test_low_or_missing_memory_pair_completes_only_with_frozen_overrides(
    memory_service, monkeypatch, capsys
):
    fixture, available = memory_service
    root = fixture["root"]
    assert temperature._run(fixture, root / "plan/plan.json", "llama-cpp", "blocked") == 2
    assert not fixture["state"]["posts"]
    assert not (root / "blocked").exists()
    plan_path = _plan(fixture)
    plan = read_json(plan_path)
    assert plan["definition"] == "engine_fit_plan.v4"
    assert plan["parameters"]["min_free_memory_bytes"] is None
    assert plan["parameters"]["max_temperature_celsius"] is None
    for engine in ("llama-cpp", "lmstudio"):
        assert temperature._run(fixture, plan_path, engine, engine) == 0, capsys.readouterr()
        run = read_json(root / engine / "run.json")
        assert run["definition"] == "engine_fit_run.v5"
        assert run["counts"]["completed"] == run["counts"]["planned"] == 3
        assert "memory_stop_explicitly_disabled" in run["limitations"]
        assert "temperature_stop_explicitly_disabled" in run["limitations"]
        assert run["performance_comparison_qualified"] is False
        for sample in run["resources"]:
            assert sample["memory_available_bytes"] == available
            assert sample["temperature_samples"][0]["value"] == 95.5
            if available is None:
                assert (
                    sample["missing_reasons"]["memory_available_bytes"]
                    == "synthetic_memory_unavailable"
                )
        assert main(["engine-fit", "verify", "--path", str(root / engine)]) == 0
    assert len(fixture["state"]["posts"]) == 6
    assert not read_json(root / "host.state.json")["dirty"]
    comparison = root / "comparison"
    assert (
        main(
            [
                "engine-fit",
                "compare",
                "--runs",
                str(root / "llama-cpp"),
                str(root / "lmstudio"),
                "--out",
                str(comparison),
            ]
        )
        == 0
    ), capsys.readouterr()
    fixture["close"]()
    for engine in ("llama-cpp", "lmstudio"):
        shutil.rmtree(root / engine)
    fixture["model"].unlink()

    def no_live_access(*_args, **_kwargs):
        pytest.fail("offline verify attempted live access")

    monkeypatch.setattr(fixture["runtime"], "FitClient", no_live_access)
    monkeypatch.setattr(fixture["runtime"], "host_identity", no_live_access)
    assert main(["engine-fit", "verify", "--path", str(comparison)]) == 0
    html = (comparison / "report.html").read_text()
    assert "本次已显式跳过内存停止" in html and "本次已显式跳过温度停止" in html
    assert "&lt;script&gt;reason&lt;/script&gt;" in html and "<script>" not in html


def test_memory_only_keeps_temperature_stop(memory_service, capsys):
    fixture, _ = memory_service
    plan = _plan(fixture, both=False)
    assert read_json(plan)["parameters"]["max_temperature_celsius"] == 85
    assert temperature._run(fixture, plan, "llama-cpp", "hot") == 2, capsys.readouterr()
    assert not fixture["state"]["posts"]
    assert not (fixture["root"] / "hot").exists()


def test_overrides_preserve_dirty_and_no_implicit_retry_after_broken_protocol(
    memory_service, capsys
):
    fixture, _ = memory_service
    plan = _plan(fixture)
    root = fixture["root"]
    fixture["state"]["broken"] = True
    assert temperature._run(fixture, plan, "llama-cpp", "broken") == 3, capsys.readouterr()
    dirty = read_json(root / "host.state.json")
    assert dirty["dirty"] and dirty["dirty_token"]
    run = read_json(root / "broken/run.json")
    assert run["definition"] == "engine_fit_run.v5"
    assert run["counts"]["failed"] == 1 and run["counts"]["not_executed"] == 2
    assert main(["engine-fit", "verify", "--path", str(root / "broken")]) == 0
    fixture["state"]["broken"] = False
    assert temperature._run(fixture, plan, "llama-cpp", "unrecovered") == 2
    assert len(fixture["state"]["posts"]) == 1
    assert read_json(root / "host.state.json")["dirty_token"] == dirty["dirty_token"]
