"""Only a genuinely absent seal permits read-only partial recovery."""

from pathlib import Path

import pytest

from inferyard.application.engine_fit import execute
from inferyard.application.types import CommandRequest
from inferyard.cli import main
from inferyard.evidence.engine_fit_checkpoint import read_partial
from inferyard.evidence.storage import EvidenceError
from inferyard.reporting.engine_fit import seal_run, verify
from tests.unit.test_engine_fit_report import _data
from tests.unit.test_engine_fit_report import plan as plan


@pytest.mark.parametrize("reader", [verify, read_partial])
@pytest.mark.parametrize("replacement", ["dangling", "symlink", "directory"])
def test_unsafe_manifest_never_becomes_partial(tmp_path, plan, reader, replacement, capsys):
    run, rows = _data(plan)
    seal_run(tmp_path, plan, run, rows)
    manifest = tmp_path / "manifest.json"
    target = tmp_path / "saved-manifest.json"
    manifest.rename(target)
    if replacement == "directory":
        manifest.mkdir()
    else:
        manifest.symlink_to(target if replacement == "symlink" else tmp_path / "missing.json")
    with pytest.raises(EvidenceError, match="unsafe_file"):
        reader(tmp_path)
    assert main(["engine-fit", "verify", "--path", str(tmp_path)]) == 4
    capsys.readouterr()


@pytest.mark.parametrize("reader", [verify, read_partial])
def test_unreadable_manifest_entry_is_not_treated_as_missing(tmp_path, plan, reader, monkeypatch):
    run, rows = _data(plan)
    seal_run(tmp_path, plan, run, rows)
    original = Path.lstat

    def inaccessible(path, *args, **kwargs):
        if path == tmp_path / "manifest.json":
            raise PermissionError("simulated inaccessible seal")
        return original(path, *args, **kwargs)

    monkeypatch.setattr(Path, "lstat", inaccessible)
    with pytest.raises(EvidenceError, match="engine_fit_missing_file"):
        reader(tmp_path)


def test_missing_manifest_does_not_claim_requested_rerender(tmp_path, plan, monkeypatch):
    import inferyard.reporting.engine_fit as reporting

    run, rows = _data(plan)
    seal_run(tmp_path, plan, run, rows)
    (tmp_path / "manifest.json").unlink()
    monkeypatch.setattr(reporting, "render", lambda *a, **k: pytest.fail("partial rendered"))
    code, result = execute(CommandRequest("engine-fit verify", run=tmp_path, rerender=True))
    assert code == 3 and result.status == "partial"
    assert result.details["sealed"] is result.details["verified"] is False
    assert result.details["render_checked"] is False
    assert read_partial(tmp_path)["requests"] == rows
