"""Temperature override flow with synthetic hot sensors; no models or live engines."""

import shutil
from copy import deepcopy

import pytest

from inferyard.cli import main
from inferyard.evidence.storage import read_json
from tests.integration import test_engine_fit_common as common_fixtures
from tests.unit.test_engine_fit_native_evidence import native_data

REASON = 'explicit short diagnostic <script>reason</script> & "approval"'
common_fit_service = common_fixtures.common_fit_service


def _plan(fixture):
    path = fixture["root"] / "override-plan"
    assert (
        main(
            [
                "engine-fit",
                "plan",
                "--model",
                str(fixture["model"]),
                "--engines",
                "llama-cpp",
                "lmstudio",
                "--skip-temperature-stop",
                REASON,
                "--out",
                str(path),
            ]
        )
        == 0
    )
    return path / "plan.json"


def _run(fixture, plan, engine, name):
    fixture["state"]["engine"] = engine
    arguments = [
        "engine-fit",
        "run",
        "--plan",
        str(plan),
        "--engine",
        engine,
        "--endpoint-url",
        fixture["origin"],
        "--server-pid",
        "42",
        "--served-model",
        "synthetic",
        "--out",
        str(fixture["root"] / name),
    ]
    if engine == "lmstudio":
        arguments.extend(
            ["--lms-path", str(fixture["lms_path"]), "--models-root", str(fixture["models_root"])]
        )
    return main(arguments)


@pytest.fixture
def override_service(common_fit_service, monkeypatch):
    from inferyard.platforms import sensors_macos

    sample = native_data.__wrapped__()[1]["resources"][0]["temperature_samples"][0]

    class SyntheticSensors:
        sources = [{"metric_name": "temperature"}]

        def collect(self, _phase, _request):
            values = []
            for temperature in (95.5, 42.5, None):
                value = deepcopy(sample)
                value.update(raw_value=temperature, value=temperature)
                value["missing_reason"] = (
                    "synthetic_source_unavailable" if temperature is None else None
                )
                values.append(value)
            return values

    monkeypatch.setattr(sensors_macos, "MacSensors", SyntheticSensors)
    return common_fit_service


def test_hot_pair_completes_with_frozen_override_and_verifies_offline(
    override_service, monkeypatch, capsys
):
    fixture = override_service
    root, state = fixture["root"], fixture["state"]
    assert read_json(root / "plan/plan.json")["parameters"]["max_temperature_celsius"] == 85
    plan_path = _plan(fixture)
    plan = read_json(plan_path)
    assert plan["definition"] == "engine_fit_plan.v3"
    assert plan["parameters"]["temperature_stop_override_reason"] == REASON
    assert plan["parameters"]["max_temperature_celsius"] is None
    for engine in ("llama-cpp", "lmstudio"):
        assert _run(fixture, plan_path, engine, engine) == 0, capsys.readouterr()
        evidence = read_json(root / engine / "run.json")
        assert evidence["definition"] == "engine_fit_run.v4"
        assert evidence["platform"] == "Darwin"
        assert evidence["counts"]["completed"] == evidence["counts"]["planned"] == 3
        assert evidence["completeness"] == "complete" and evidence["stop_reason"] is None
        assert "temperature_stop_explicitly_disabled" in evidence["limitations"]
        assert evidence["performance_comparison_qualified"] is False
        for resource in evidence["resources"]:
            assert [sample["value"] for sample in resource["temperature_samples"]] == [
                95.5,
                42.5,
                None,
            ]
            assert (
                resource["temperature_samples"][-1]["missing_reason"]
                == "synthetic_source_unavailable"
            )
        assert main(["engine-fit", "verify", "--path", str(root / engine)]) == 0
    assert len(state["posts"]) == 6
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
    shutil.rmtree(root / "llama-cpp")
    shutil.rmtree(root / "lmstudio")
    fixture["model"].unlink()

    def no_live_access(*_args, **_kwargs):
        pytest.fail("offline verification attempted live service access")

    monkeypatch.setattr(fixture["runtime"], "FitClient", no_live_access)
    monkeypatch.setattr(fixture["runtime"], "host_identity", no_live_access)
    assert main(["engine-fit", "verify", "--path", str(comparison)]) == 0
    html = (comparison / "report.html").read_text()
    assert "本次已显式跳过温度停止" in html
    assert "&lt;script&gt;reason&lt;/script&gt;" in html and "<script>" not in html
    assert "缺测 · lmstudio_service_version_not_exposed" in html and "<td>None</td>" not in html


def test_override_does_not_clear_dirty_after_incomplete_protocol(override_service, capsys):
    fixture = override_service
    plan_path = _plan(fixture)
    root = fixture["root"]
    fixture["state"]["broken"] = True
    assert _run(fixture, plan_path, "llama-cpp", "broken") == 3, capsys.readouterr()
    assert len(fixture["state"]["posts"]) == 1
    dirty = read_json(root / "host.state.json")
    assert dirty["dirty"] and dirty["dirty_token"]
    run = read_json(root / "broken/run.json")
    assert run["definition"] == "engine_fit_run.v4"
    assert run["counts"]["failed"] == 1 and run["counts"]["not_executed"] == 2
    assert "temperature_stop_explicitly_disabled" in run["limitations"]
    assert main(["engine-fit", "verify", "--path", str(root / "broken")]) == 0
    fixture["state"]["broken"] = False
    assert _run(fixture, plan_path, "llama-cpp", "unrecovered") == 2
    assert len(fixture["state"]["posts"]) == 1
    assert read_json(root / "host.state.json")["dirty_token"] == dirty["dirty_token"]
