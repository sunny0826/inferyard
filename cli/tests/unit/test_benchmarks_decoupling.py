"""Verify decoupled benchmarks/ catalog, custom local bundles, and metadata hints."""

from __future__ import annotations

import json
from pathlib import Path

import pytest

from inferyard.application.workspace_progress import inspect_workspace
from inferyard.bundles.registry import (
    bundle_names,
    default_bundles,
    effective_execution_hints,
    effective_metadata,
    list_bundles,
    verify_bundles,
)
from inferyard.cli import main


def _custom_bundle(bundle_id: str = "en-custom-v1") -> dict:
    return {
        "schema_version": 3,
        "bundle_id": bundle_id,
        "version": "1.0.0",
        "language": "en-US",
        "license_note": "Synthetic custom test bundle.",
        "task_protocol": "quality",
        "metadata": {
            "display_name": {"en": "Custom English Pack", "zh-Hans": "自定义英文题集"},
            "description": {"en": "Custom standalone test suite", "zh-Hans": "本地自定义测试题集"},
            "tags": ["custom", "en"],
        },
        "execution_hints": {
            "default_max_tokens": 1024,
            "reasoning_max_tokens": {"low": 1536, "medium": 3072, "high": 4096},
        },
        "answer_policy": {
            "instruction_newlines": "crlf_to_lf",
            "strip_line_edges": True,
            "ignore_empty_lines": True,
            "unicode_normalization": "none",
            "reasoning": "separate_channel_excluded_content_unchanged",
        },
        "cases": [
            {
                "case_id": "inst-01",
                "category": "instruction",
                "prompt": "Write one line containing hello.",
                "reference_answer": "hello world",
                "rules": {
                    "literal_contains": ["hello"],
                    "literal_forbidden": [],
                    "nonempty_line_count": 1,
                },
            }
        ],
    }


def test_registered_benchmarks_catalog_and_verify() -> None:
    names = bundle_names()
    assert "zh-core" in names
    assert "zh-svg-pelican" in names
    assert "zh-context-speed" in names
    assert "zh-smoke" in names
    assert default_bundles() == ("zh-core", "zh-svg-pelican")

    listed = list_bundles()
    assert listed["default_bundles"] == ["zh-core", "zh-svg-pelican"]
    assert len(listed["items"]) == len(names)

    verified = verify_bundles()
    assert verified["verified"] is True
    assert len(verified["items"]) == len(names)


def test_init_and_progress_accept_custom_local_bundle(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    monkeypatch.setattr("platform.system", lambda: "Darwin")
    monkeypatch.setattr("platform.machine", lambda: "arm64")

    custom_path = tmp_path / "my-custom.json"
    custom_doc = _custom_bundle()
    custom_path.write_text(json.dumps(custom_doc, ensure_ascii=False), encoding="utf-8")

    # Verify standalone file via CLI.
    assert main(["bundles", "verify", str(custom_path)]) == 0
    verify_out = json.loads(capsys.readouterr().out)
    assert verify_out["details"]["verified"] is True
    assert verify_out["details"]["items"][0]["bundle_id"] == "en-custom-v1"

    # Initialize workspace with both a registered bundle and a local custom file.
    ws = tmp_path / "ws"
    assert main(["init", "--out", str(ws), "--bundle", "zh-smoke", "--bundle", str(custom_path)]) == 0
    init_out = json.loads(capsys.readouterr().out)
    exported = [item["bundle"] for item in init_out["details"]["bundles"]]
    assert exported == ["zh-smoke", "my-custom"]
    assert (ws / "bundles" / "my-custom.json").is_file()

    # Workspace progress accepts custom bundle and surfaces localized metadata.
    progress = inspect_workspace(ws)
    assert [b["bundle"] for b in progress["bundles"]] == ["zh-smoke", "my-custom"]
    custom_progress = progress["bundles"][1]
    assert custom_progress["display_name"]["en"] == "Custom English Pack"
    assert custom_progress["description"]["zh-Hans"] == "本地自定义测试题集"


def test_effective_hints_and_metadata_merge_inline_over_catalog() -> None:
    custom = _custom_bundle()
    hints = effective_execution_hints(custom)
    assert hints["default_max_tokens"] == 1024
    assert hints["reasoning_max_tokens"]["medium"] == 3072
    meta = effective_metadata(custom)
    assert meta["display_name"]["en"] == "Custom English Pack"
