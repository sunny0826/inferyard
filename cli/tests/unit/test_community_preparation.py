"""Installed preparation uses real local files and no model service or host lock."""

import hashlib
import json
import subprocess
import sys
from pathlib import Path

import pytest

from inferyard.application.types import CommandRequest
from inferyard.cli import main
from inferyard.cli.arguments import parser
from inferyard.cli.request import build_request
from inferyard.config import preparation_io
from inferyard.config.community_assets import prepare
from inferyard.config.preparation_io import NewDirectory, PreparationError
from tests.unit.test_device_preflight import gguf

ROOT = Path(__file__).parents[2]


def invoke(capsys, args):
    code = main(args)
    captured = capsys.readouterr()
    return code, json.loads(captured.out)


@pytest.mark.parametrize("name", ["zh-smoke", "zh-core", "zh-svg-pelican"])
def test_init_preserves_authoritative_bytes_and_review(tmp_path, capsys, name):
    out = tmp_path / "中文 workspace"
    code, result = invoke(capsys, ["init", "--out", str(out), "--bundle", name])
    assert code == 0 and result["status"] == "prepared"
    assert result["evidence_dir"] is None and result["run_id"] is None
    source = (ROOT / f"bundles/{name}.json").read_bytes()
    assert (out / f"bundles/{name}.json").read_bytes() == source
    assert result["details"]["bundle_sha256"] == hashlib.sha256(source).hexdigest()
    assert result["details"]["bundles"] == [
        {
            "bundle": name,
            "bundle_path": result["details"]["bundle_path"],
            "bundle_sha256": result["details"]["bundle_sha256"],
        }
    ]
    assert result["details"]["ready_to_run"] is False
    assert set(result["details"]["configs"]) == {"linux", "macos", "windows"}
    assert not list((out / "results").iterdir())
    code, again = invoke(capsys, ["init", "--out", str(out)])
    assert code == 2 and again["limitations"] == ["output_exists"]
    assert (out / f"bundles/{name}.json").read_bytes() == source


def test_omitted_bundle_exports_documented_default_scope(tmp_path, capsys):
    out = tmp_path / "default"
    code, result = invoke(capsys, ["init", "--out", str(out)])
    names = ("zh-core", "zh-svg-pelican")
    exported = []
    for name in names:
        source = (ROOT / f"bundles/{name}.json").read_bytes()
        assert (out / f"bundles/{name}.json").read_bytes() == source
        exported.append(
            {
                "bundle": name,
                "bundle_path": str(out / f"bundles/{name}.json"),
                "bundle_sha256": hashlib.sha256(source).hexdigest(),
            }
        )
    assert code == 0 and result["details"]["bundles"] == exported
    assert result["details"]["bundle"] == "zh-core"
    assert not (out / "bundles/zh-smoke.json").exists()


@pytest.mark.parametrize("kind", ["file", "directory", "link", "dangling"])
def test_all_existing_outputs_refused_before_resolution(tmp_path, capsys, kind):
    out, target = tmp_path / "out", tmp_path / "target"
    if kind == "file":
        out.write_text("preserve")
    elif kind == "directory":
        out.mkdir()
    else:
        if kind == "link":
            target.mkdir()
        out.symlink_to(target, target_is_directory=True)
    code, result = invoke(capsys, ["init", "--out", str(out)])
    assert code == 2 and result["limitations"] == ["output_exists"]
    if kind == "dangling":
        assert out.is_symlink() and not target.exists()


def test_request_keeps_lexical_output_without_changing_old_commands(tmp_path):
    out = tmp_path / "dangling"
    out.symlink_to(tmp_path / "other")
    assert build_request(parser().parse_args(["init", "--out", str(out)])).out == out
    old = build_request(parser().parse_args(["verify", "--path", str(out)]))
    assert old.run == out.resolve()


def test_output_tree_reparse_replacement_is_rejected(tmp_path):
    output = NewDirectory(tmp_path / "new")
    output.directory("a")
    (output.path / "a").rmdir()
    (output.path / "a").symlink_to(tmp_path)
    with pytest.raises(PreparationError, match="output_exists"):
        output.write("a/escape", b"no")
    assert not (tmp_path / "escape").exists()


def test_assets_preserves_template_utf8_bytes(tmp_path, monkeypatch):
    monkeypatch.setattr(preparation_io, "native_platform", lambda: ("Linux", "x64"))
    model = gguf(tmp_path / "model.gguf", "Test")
    engine = tmp_path / "engine"
    engine.write_bytes(b"never execute this")
    library = tmp_path / "libengine.so.1"
    library.write_bytes(b"library")
    (tmp_path / "libengine.so").symlink_to(library.name)
    raw = "中文\r\n{{ messages }}\n\r".encode()
    from inferyard.config import community_assets

    monkeypatch.setattr(
        community_assets, "read_metadata", lambda _: {"tokenizer.chat_template": raw.decode()}
    )
    result = prepare(
        CommandRequest("config assets", model_path=model, engine_path=engine, out=tmp_path / "new")
    )
    assert Path(result["template"]["path"]).read_bytes() == raw
    assert result["template"]["sha256"] == hashlib.sha256(raw).hexdigest()
    assert set(result["libraries"]) == {"engine", "libengine.so", "libengine.so.1"}
    assert result["model"]["sha256"] == hashlib.sha256(model.read_bytes()).hexdigest()


def test_assets_rejects_missing_template_and_escaping_library(tmp_path, monkeypatch):
    monkeypatch.setattr(preparation_io, "native_platform", lambda: ("Linux", "x64"))
    from inferyard.config import community_assets

    folder = tmp_path / "engine"
    folder.mkdir()
    engine = folder / "engine"
    engine.write_bytes(b"fixture")
    model = gguf(tmp_path / "model.gguf", "Test")
    request = CommandRequest(
        "config assets", model_path=model, engine_path=engine, out=tmp_path / "new"
    )
    monkeypatch.setattr(community_assets, "read_metadata", lambda _: {})
    with pytest.raises(PreparationError, match="model_chat_template_required"):
        prepare(request)
    monkeypatch.setattr(
        community_assets, "read_metadata", lambda _: {"tokenizer.chat_template": "x"}
    )
    (folder / "bad.so").symlink_to(model)
    with pytest.raises(PreparationError, match="invalid_library_manifest"):
        prepare(request)
    assert not request.out.exists()


@pytest.mark.parametrize(
    "args",
    [
        [
            "config",
            "bind",
            "--candidate",
            "x",
            "--pid",
            "0",
            "--endpoint",
            "http://127.0.0.1",
            "--out",
            "new",
        ],
        [
            "config",
            "create",
            "--preflight",
            "x",
            "--bundle",
            "x",
            "--results",
            "x",
            "--engine",
            "x",
            "--out",
            "new",
            "--port",
            "65536",
        ],
        ["init", "--out", "new", "--force"],
    ],
)
def test_invalid_parameters_are_json_blocked(args, capsys):
    code, result = invoke(capsys, args)
    assert code == 2 and result["limitations"] == ["invalid_input"]


def test_missing_input_is_io_error(tmp_path, capsys, monkeypatch):
    monkeypatch.setattr(preparation_io, "native_platform", lambda: ("Linux", "x64"))
    code, result = invoke(
        capsys,
        [
            "config",
            "assets",
            "--model",
            str(tmp_path / "missing"),
            "--engine",
            str(tmp_path / "e"),
            "--out",
            str(tmp_path / "out"),
        ],
    )
    assert code == 4 and result["limitations"] == ["io_error"]


def test_help_and_versions_do_not_import_new_live_backends():
    code = """
import importlib.abc, sys
class Reject(importlib.abc.MetaPathFinder):
    def find_spec(self, fullname, path=None, target=None):
        forbidden = ("inferyard.platforms.windows_runtime_prepare",
                     "inferyard.config.community_candidates", "inferyard.runtime")
        if fullname.startswith(forbidden):
            raise AssertionError(fullname)
sys.meta_path.insert(0, Reject())
from inferyard.cli import main
assert main(["--versions"]) == 0
try:
    main(["runtime", "prepare", "--help"])
except SystemExit as exc:
    assert exc.code == 0
"""
    result = subprocess.run([sys.executable, "-c", code], capture_output=True, text=True)
    assert result.returncode == 0, result.stderr


@pytest.mark.parametrize("failure", ["dangling", "loop", "permission", "io"])
def test_library_failures_keep_cli_error_categories(tmp_path, monkeypatch, capsys, failure):
    import errno

    from inferyard.config import community_assets

    monkeypatch.setattr(preparation_io, "native_platform", lambda: ("Linux", "x64"))
    model = gguf(tmp_path / "model.gguf", "Test")
    engine = tmp_path / "engine"
    engine.write_bytes(b"never execute")
    library = tmp_path / "bad.so"
    monkeypatch.setattr(
        community_assets, "read_metadata", lambda _: {"tokenizer.chat_template": "中文模板"}
    )
    if failure in ("dangling", "loop"):
        library.symlink_to(library if failure == "loop" else tmp_path / "missing")
    else:
        library.write_bytes(b"library")
        original = Path.resolve

        def resolve(self, *args, **kwargs):
            if self == library:
                raise OSError(errno.EACCES if failure == "permission" else errno.EIO, "injected")
            return original(self, *args, **kwargs)

        monkeypatch.setattr(Path, "resolve", resolve)
    code, result = invoke(
        capsys,
        [
            "config",
            "assets",
            "--model",
            str(model),
            "--engine",
            str(engine),
            "--out",
            str(tmp_path / "new"),
        ],
    )
    invalid = failure in ("dangling", "loop")
    assert code == (2 if invalid else 4)
    assert result["limitations"] == ["invalid_library_manifest" if invalid else "io_error"]
    assert not (tmp_path / "new").exists()
