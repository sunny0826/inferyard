"""Real offline CLI subprocesses consume synthetic old and container originals."""

import json
import subprocess
import sys
from pathlib import Path

import pytest

from tests.evidence_layout_helpers import build_current_original, pack_layout2
from tests.unit.test_layout_surfaces import sources as layout_sources
from tests.unit.test_layout_surfaces import tree_bytes

sources = layout_sources


@pytest.mark.parametrize("layout", [1, 2])
def test_offline_cli_source_chain_keeps_originals_and_report_v8(sources, tmp_path, layout):
    root = sources[layout - 1]
    peer_original = build_current_original(tmp_path / "peer-original")
    peer = (
        pack_layout2(peer_original, tmp_path / "peer-container") if layout == 1 else peer_original
    )
    originals = {*sources, peer_original, peer}
    before = {p: tree_bytes(p) for p in originals}
    cwd = Path(__file__).resolve().parents[2]

    def invoke(*arguments):
        result = subprocess.run(
            [sys.executable, "-m", "inferyard", *map(str, arguments)],
            cwd=cwd,
            capture_output=True,
            text=True,
            timeout=30,
        )
        with (tmp_path / "cli-transcript.jsonl").open("a") as stream:
            stream.write(
                json.dumps(
                    {
                        "command": result.args,
                        "workdir": str(cwd),
                        "exit_code": result.returncode,
                        "stdout": result.stdout,
                        "stderr": result.stderr,
                    },
                    ensure_ascii=False,
                )
                + "\n"
            )
        assert result.returncode == 0, (arguments, result.stdout, result.stderr)
        assert not result.stderr
        assert len(result.stdout.splitlines()) == 1
        return json.loads(result.stdout)

    verified = invoke("verify", "--path", root)
    assert verified["details"]["artifact_type"] == "run"
    report, export, rescore, public, compare = (
        tmp_path / name for name in ("report", "export", "rescore", "public", "compare")
    )
    invoke("report", "--runs", root, "--out", report)
    assert {p.name for p in report.iterdir()} == {
        "index.json",
        "report.html",
        "artifact-manifest.json",
    }
    assert json.loads((report / "index.json").read_text())["report_format_version"] == 8
    invoke("verify", "--path", report, "--rerender")
    invoke("export", "--run", root, "--out", export)
    invoke("verify", "--path", export)
    invoke(
        "rescore",
        "--run",
        root,
        "--out",
        rescore,
        "--scorer",
        "phase2.v2",
        "--reason",
        "synthetic layout CLI regression",
    )
    invoke("verify", "--path", rescore)
    invoke("public", "package", "--run", root, "--out", public)
    verified = invoke("verify", "--path", public, "--source-run", root)
    assert verified["details"]["source_projection_verified"]
    invoke("compare", "--left", root, "--right", peer, "--out", compare)
    invoke("verify", "--path", compare)
    assert before == {p: tree_bytes(p) for p in originals}
