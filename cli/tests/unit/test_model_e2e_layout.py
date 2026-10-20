import runpy
from pathlib import Path

import pytest


@pytest.fixture
def check_layout(monkeypatch):
    scripts = Path(__file__).parents[1] / "packaging"
    monkeypatch.syspath_prepend(str(scripts))
    return runpy.run_path(str(scripts / "ci_model_e2e.py"))["assert_workspace"]


def workspace(root, system):
    names = [
        "environment.jsonl",
        "events.jsonl",
        "inputs.json",
        "manifest.json",
        "memory.jsonl",
        "observations.jsonl",
        "request-bodies.jsonl",
        "schedule.jsonl",
    ]
    if system in ("Darwin", "Linux"):
        names += ["external-cpu.jsonl", "request-environment.jsonl"]
    runs = [root / "results" / name for name in ("smoke", "svg")]
    for run in runs:
        run.mkdir(parents=True)
        for name in names:
            (run / name).touch()
    report = root / "report"
    report.mkdir()
    for name in ("artifact-manifest.json", "index.json", "report.html"):
        (report / name).touch()
    return runs


@pytest.mark.parametrize("system", ["Darwin", "Linux", "Windows"])
def test_model_e2e_accepts_current_platform_layout(check_layout, monkeypatch, tmp_path, system):
    monkeypatch.setattr("platform.system", lambda: system)
    expected_count = 19 if system == "Windows" else 23
    assert check_layout(tmp_path, workspace(tmp_path, system)) == expected_count


@pytest.mark.parametrize("system", ["Darwin", "Linux", "Windows"])
@pytest.mark.parametrize("change", ["missing", "unexpected"])
def test_model_e2e_rejects_missing_or_extra_files(
    check_layout, monkeypatch, tmp_path, system, change
):
    monkeypatch.setattr("platform.system", lambda: system)
    runs = workspace(tmp_path, system)
    if change == "missing":
        name = "memory.jsonl" if system == "Windows" else "external-cpu.jsonl"
        (runs[0] / name).unlink()
    else:
        (runs[0] / "unexpected.jsonl").touch()
    with pytest.raises(SystemExit, match="workspace layout mismatch"):
        check_layout(tmp_path, runs)
