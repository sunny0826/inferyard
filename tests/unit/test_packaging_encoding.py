"""Simulated non-UTF-8 defaults; this is not native Windows code-page evidence."""

import importlib.util
import json
import subprocess
import sys
from pathlib import Path

from tests.packaging import installed_probe


def test_report_probe_preserves_unicode_with_simulated_cp1252_default(tmp_path, monkeypatch):
    fixture = tmp_path / "中文"
    read, write = Path.read_text, Path.write_text

    def read_legacy(path, encoding=None, **kwargs):
        return read(path, encoding=encoding or "cp1252", **kwargs)

    def write_legacy(path, text, encoding=None, **kwargs):
        return write(path, text, encoding=encoding or "cp1252", **kwargs)

    monkeypatch.setattr(Path, "read_text", read_legacy)
    monkeypatch.setattr(Path, "write_text", write_legacy)
    import hashlib

    import inferyard

    root = Path(inferyard.__file__).parent
    resources = {
        p.relative_to(root).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
        for p in root.rglob("*")
        if p.is_file() and "__pycache__" not in p.parts
    }
    expected = tmp_path / "expected.json"
    expected.write_bytes(
        json.dumps(
            dict(
                resources=resources,
                tool_source_hash="tool",
                scorer_hash="scorer",
                fixture_relative=fixture.relative_to(tmp_path).as_posix(),
            ),
            ensure_ascii=False,
        ).encode("utf-8")
    )
    monkeypatch.setattr(installed_probe, "tool_source_hash", lambda: "tool")
    monkeypatch.setattr(installed_probe, "scorer_hash", lambda: "scorer")
    checks = []

    def current_writer(runs, out):
        assert runs == [fixture]
        out.mkdir()
        (out / "index.json").write_bytes(
            json.dumps({"report_format_version": 7, "title": "中文🧪"}, ensure_ascii=False).encode()
        )
        (out / "report.html").write_bytes("<h1>中文🧪</h1>".encode())

    def current_verifier(out, *, options=None):
        checks.append(bool(options and options.rerender))
        return {"verified": True}

    # Only simulate the probe's text I/O, not Python 3.14's production UTF-8 runtime.
    monkeypatch.setattr(installed_probe, "write_report", current_writer)
    monkeypatch.setattr(installed_probe, "verify_report", current_verifier)
    monkeypatch.setattr(
        sys,
        "argv",
        [
            "probe",
            "--expected",
            str(expected),
            "--fixtures",
            str(tmp_path),
            "--out",
            str(tmp_path / "out"),
        ],
    )
    installed_probe.main()
    assert checks == [False, True]
    report = tmp_path / "out/current-report"
    assert "中文🧪" in (report / "report.html").read_bytes().decode("utf-8")
    index = json.loads((report / "index.json").read_bytes())
    assert index["report_format_version"] == 7
    assert "中文🧪" in json.dumps(index, ensure_ascii=False)
    for version in range(1, 7):
        old = tmp_path / f"out/unsupported-report-v{version}"
        assert not (old / "report.html").exists()
        assert json.loads((old / "index.json").read_bytes())["report_format_version"] == version


def test_child_cli_encoding_overrides_inherited_codepage(tmp_path, monkeypatch):
    directory = Path(__file__).parents[1] / "packaging"
    monkeypatch.syspath_prepend(str(directory))
    spec = importlib.util.spec_from_file_location(
        "installed_matrix_encoding", directory / "run_installed.py"
    )
    matrix = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(matrix)
    monkeypatch.setenv("PYTHONIOENCODING", "cp936")
    env = matrix.environment(tmp_path)
    result = subprocess.run(
        [sys.executable, "-c", "print('中文🧪')"],
        env=env,
        capture_output=True,
        text=True,
        encoding="utf-8",
        check=True,
    )
    assert result.stdout == "中文🧪\n"
