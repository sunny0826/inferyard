"""compare-engines writes a sealed artifact that verify recomputes from its source runs."""

import json

import pytest

from inferyard.application.types import CommandRequest
from inferyard.application.verification import execute as verify
from inferyard.errors import EvidenceError
from inferyard.evidence.artifact_seal import seal
from inferyard.reporting import engine_blocks_report
from tests.unit.test_engine_blocks import cross_format


def fake_loader(tmp_path, rows):
    def load(paths):
        sources = [
            {
                "run_id": row["run"]["run_id"],
                "path": str(path.resolve()),
                "manifest_sha256": None,
                "unsealed_content_sha256": "0" * 64,
            }
            for row, path in zip(rows, paths, strict=True)
        ]
        return rows, sources

    return load


@pytest.fixture
def artifact(tmp_path, monkeypatch):
    rows = cross_format(e2e_ms=800)
    runs = [tmp_path / "runs" / row["run"]["run_id"] for row in rows]
    monkeypatch.setattr(engine_blocks_report, "load_blocks", fake_loader(tmp_path, rows))
    out = tmp_path / "compare"
    code, result = engine_blocks_report.execute(
        CommandRequest("compare-engines", left_runs=runs[0::2], right_runs=runs[1::2], out=out)
    )
    assert code == 0 and result.status == "compared"
    return out, rows, monkeypatch


def test_artifact_is_sealed_rendered_and_verifies(artifact):
    out, _, _ = artifact
    assert {p.name for p in out.iterdir()} == {
        "engine-comparison.json",
        "report.html",
        "artifact-manifest.json",
    }
    saved = json.loads((out / "engine-comparison.json").read_text())
    assert saved["verdicts"]["client_decode_interval_ms"]["metric_id"] == "L08"
    assert saved["verdicts"]["first_body_text_ms"]["metric_id"] == "L09"
    assert all(not ref["path"].startswith("/") for ref in saved["source_runs"])
    html = (out / "report.html").read_text()
    assert "mlx-lm 更优" in html
    assert "<script" not in html and "://" not in html  # offline: no scripts or external URLs
    code, result = verify(CommandRequest("verify", run=out, rerender=True))
    assert code == 0
    assert result.details["artifact_type"] == "engine-comparison"
    assert result.details["render_checked"] is True


def test_changed_bytes_are_rejected(artifact):
    out, _, _ = artifact
    path = out / "engine-comparison.json"
    path.write_text(path.read_text().replace('"mlx-lm"', '"llama-cpp"', 1))
    with pytest.raises(EvidenceError, match="presentation_bytes_changed"):
        verify(CommandRequest("verify", run=out))


def test_resealed_result_that_differs_from_sources_is_rejected(artifact):
    out, rows, monkeypatch = artifact
    path = out / "engine-comparison.json"
    saved = json.loads(path.read_text())
    saved["verdicts"]["client_decode_interval_ms"]["verdict"] = "llama-cpp"
    path.write_text(json.dumps(saved))
    (out / "artifact-manifest.json").unlink()
    seal(out, ["engine-comparison.json", "report.html"])
    with pytest.raises(EvidenceError, match="engine_comparison_recomputation_mismatch"):
        verify(CommandRequest("verify", run=out))


def test_changed_source_runs_are_rejected(artifact):
    out, rows, monkeypatch = artifact
    rows[1]["requests"][0]["t_terminal_ns"] *= 2
    with pytest.raises(EvidenceError, match="engine_comparison_recomputation_mismatch"):
        verify(CommandRequest("verify", run=out))


def test_resealed_html_fails_only_with_rerender(artifact):
    out, _, _ = artifact
    html = out / "report.html"
    html.write_text(html.read_text().replace("更优", "更差"))
    (out / "artifact-manifest.json").unlink()
    seal(out, ["engine-comparison.json", "report.html"])
    assert verify(CommandRequest("verify", run=out))[0] == 0
    with pytest.raises(EvidenceError, match="engine_comparison_html_recomputation_mismatch"):
        verify(CommandRequest("verify", run=out, rerender=True))


def test_existing_output_is_not_overwritten(artifact):
    out, rows, _ = artifact
    runs = [out.parent / "runs" / row["run"]["run_id"] for row in rows]
    before = (out / "engine-comparison.json").read_bytes()
    with pytest.raises(FileExistsError):
        engine_blocks_report.execute(
            CommandRequest("compare-engines", left_runs=runs[0::2], right_runs=runs[1::2], out=out)
        )
    assert (out / "engine-comparison.json").read_bytes() == before
