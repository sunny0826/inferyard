"""Acceptance must retain every selected case and reject unqualified pairs."""

import importlib
import json
from pathlib import Path

import pytest


@pytest.fixture
def entry(monkeypatch):
    monkeypatch.syspath_prepend(str(Path(__file__).resolve().parents[2] / "scripts"))
    return importlib.import_module("verify_resource_performance_trial")


def comparison(tmp_path, *, missing=None, refused=None):
    policy = {
        "case_ids": ["a", "b"],
        "required_resources": ["C01", "C02", "C04"],
        "required_performance": ["L03"],
    }
    (tmp_path / "acceptance-policy.json").write_text(json.dumps(policy))
    rows = [
        {"metric_id": code, "case_id": case, "eligible": (code, case) != refused}
        for code in ["L03", "C01", "C02", "C04"]
        for case in policy["case_ids"]
        if (code, case) != missing
    ]
    directory = tmp_path / "comparison"
    directory.mkdir()
    (directory / "comparison.json").write_text(
        json.dumps({"performance_analysis": {"differences": rows}})
    )


def test_every_required_case_and_metric_must_be_qualified(entry, tmp_path):
    comparison(tmp_path)
    assert entry.qualify_resources(tmp_path)


@pytest.mark.parametrize("kind", ["missing", "refused"])
def test_another_allowed_case_does_not_hide_unknown_resource(entry, tmp_path, kind):
    comparison(tmp_path, **{kind: ("C04", "b")})
    assert not entry.qualify_resources(tmp_path)
    record = json.loads((tmp_path / "resource-verification.json").read_text())
    assert record["missing_required_pairs"] == [{"metric_id": "C04", "case_id": "b"}]


def test_resource_pairs_do_not_authorize_missing_e2e(entry, tmp_path):
    comparison(tmp_path, refused=("L03", "a"))
    assert not entry.qualify_resources(tmp_path)


def test_no_comparison_is_not_a_pass(entry, tmp_path):
    assert not entry.qualify_resources(tmp_path)


def test_preparation_uses_explicit_frozen_inputs(entry, tmp_path):
    from inferyard.config.loader import load_config

    root = Path(__file__).resolve().parents[2]
    config = load_config(root / "tests/fixtures/config/valid.toml").config.to_dict()
    config["engine"]["startup_args"].extend(["-t", "6", "-tb", "6"])
    bundle = json.loads((root / "bundles/zh-core.json").read_text(encoding="utf-8"))
    source = tmp_path / "inputs"
    source.mkdir()
    (source / "config.json").write_text(json.dumps(config), encoding="utf-8")
    (source / "bundle.json").write_text(json.dumps(bundle), encoding="utf-8")
    before = {path.name: path.read_bytes() for path in source.iterdir()}
    out = tmp_path / "prepared"
    out.mkdir()

    prepared, selected_bundle = entry.prepare(out, source=source)

    assert selected_bundle == bundle
    assert prepared["model"] == config["model"]
    assert prepared["generation"]["max_tokens"] == 128
    assert json.loads((out / "acceptance-policy.json").read_text())["case_ids"] == entry.CASES
    assert {path.name: path.read_bytes() for path in source.iterdir()} == before
