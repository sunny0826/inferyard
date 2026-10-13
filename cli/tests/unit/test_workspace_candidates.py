"""Workspace inference preserves existing candidate validation and explicit overrides."""

import json
from dataclasses import replace

import pytest

from inferyard.cli import main
from inferyard.cli.arguments import parser
from inferyard.cli.request import build_request
from inferyard.config import community_candidates, preparation_io
from inferyard.config.workspace_inputs import resolve_candidate_inputs
from tests.unit.test_community_candidates import candidate_request
from tests.unit.test_device_preflight import gguf


def setup_workspace(tmp_path, capsys):
    workspace = tmp_path / "workspace"
    assert main(["init", "--out", str(workspace)]) == 0
    capsys.readouterr()
    model = gguf(tmp_path / "chosen.gguf", "chosen")
    engine = tmp_path / "llama-server"
    engine.write_bytes(b"synthetic, never executed")
    (tmp_path / "libggml-metal.dylib").write_bytes(b"library")
    request = candidate_request(tmp_path, model, engine=engine, system="Darwin")
    path = workspace / "preflight/device-1"
    path.mkdir()
    (path / "device-preflight.json").write_bytes(request.preflight.read_bytes())
    return workspace, engine, request


def test_workspace_cli_reaches_real_candidate_generation(tmp_path, capsys, monkeypatch):
    workspace, engine, _ = setup_workspace(tmp_path, capsys)
    monkeypatch.setattr(preparation_io, "native_platform", lambda: ("Darwin", "arm64"))
    monkeypatch.setattr(community_candidates, "fresh_macos", lambda selected, _: selected)
    monkeypatch.setattr(community_candidates, "query_version", lambda *a, **k: {})
    monkeypatch.setattr(
        "inferyard.platforms.identity.environment_snapshot",
        lambda: {"platform": "Darwin", "profile": "balanced"},
    )
    out = workspace / "candidates/core-1"
    code = main(
        [
            "config",
            "create",
            "--workspace",
            str(workspace),
            "--bundle",
            "zh-core",
            "--engine",
            str(engine),
            "--out",
            str(out),
        ]
    )
    result = json.loads(capsys.readouterr().out)
    assert code == 0
    assert result["details"]["bundle"] == str(workspace / "bundles/zh-core.json")
    assert result["details"]["results"] == str(workspace / "results")
    assert result["details"]["device_preflight"] == str(
        workspace / "preflight/device-1/device-preflight.json"
    )
    assert (out / "candidate.toml").exists()


def test_latest_preflight_and_explicit_inputs_take_precedence(tmp_path, capsys):
    workspace, engine, request = setup_workspace(tmp_path, capsys)
    old = workspace / "preflight/device-1/device-preflight.json"
    latest = workspace / "preflight/device-2/device-preflight.json"
    latest.parent.mkdir()
    latest.write_bytes(old.read_bytes())
    import os

    os.utime(old, ns=(1, 1))
    os.utime(latest, ns=(2, 2))
    parsed = build_request(
        parser().parse_args(
            [
                "config",
                "create",
                "--workspace",
                str(workspace),
                "--bundle",
                "zh-core",
                "--engine",
                str(engine),
                "--out",
                str(workspace / "candidates/new"),
            ]
        )
    )
    inferred = resolve_candidate_inputs(parsed)
    assert inferred.preflight == latest and inferred.output_root == workspace / "results"
    explicit = replace(
        parsed,
        preflight=request.preflight,
        bundle_path=request.bundle_path,
        output_root=tmp_path / "explicit-results",
    )
    resolved = resolve_candidate_inputs(explicit)
    assert resolved.preflight == request.preflight
    assert resolved.bundle_path == request.bundle_path
    assert resolved.output_root == explicit.output_root
    assert resolve_candidate_inputs(request) is request


@pytest.mark.parametrize(
    "damage", ["missing_preflight", "missing_bundle", "bad_preflight", "missing_workspace"]
)
def test_workspace_rejections_exit_two_without_outputs(tmp_path, capsys, damage):
    workspace, engine, _ = setup_workspace(tmp_path, capsys)
    name = "zh-core"
    preflight = workspace / "preflight/device-1/device-preflight.json"
    if damage == "missing_preflight":
        preflight.unlink()
    elif damage == "bad_preflight":
        preflight.write_text("bad JSON")
    elif damage == "missing_bundle":
        name = "not-a-bundle"
    else:
        workspace = workspace / "missing"
    out = tmp_path / "new-candidate"
    code = main(
        [
            "config",
            "create",
            "--workspace",
            str(workspace),
            "--bundle",
            name,
            "--engine",
            str(engine),
            "--out",
            str(out),
        ]
    )
    result = json.loads(capsys.readouterr().out)
    assert code == 2 and result["status"] == "blocked" and not out.exists()


def test_old_required_parameters_and_out_remain_required(tmp_path, capsys):
    for arguments in (
        ["config", "create", "--bundle", "zh-core", "--engine", "engine", "--out", "new"],
        [
            "config",
            "create",
            "--workspace",
            str(tmp_path),
            "--bundle",
            "zh-core",
            "--engine",
            "engine",
        ],
    ):
        assert main(arguments) == 2
        assert json.loads(capsys.readouterr().out)["status"] == "blocked"
