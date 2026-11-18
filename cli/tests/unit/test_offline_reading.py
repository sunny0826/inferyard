"""Direct raw reading, sealed bytes and explicit source relocation boundaries."""

import shutil

import pytest

from inferyard.application.types import CommandRequest
from inferyard.evidence.verification_options import VerificationOptions
from inferyard.application.verification import execute
from inferyard.errors import EvidenceError
from inferyard.contracts.validation import json_bytes
from inferyard.evidence.storage import read_json
from inferyard.reporting.comparison_report import verify_comparison, write_comparison
from inferyard.reporting.studio_report import read_studio_report, write_studio_report
from tests.helpers import fixture_run


@pytest.mark.parametrize("states", [["cancelled", "not_executed"], ["invalid", "not_executed"]])
def test_sealed_partial_run_verifies_without_promoting_execution(tmp_path, states):
    from inferyard.analysis.ledger import read_trial

    root = fixture_run(tmp_path, states=states)
    original = read_trial(root)
    code, result = execute(CommandRequest("verify", run=root))
    assert code == 0 and result.status == "verified"
    assert result.completeness == result.details["execution_completeness"] == "incomplete"
    assert result.details["sealed"] and result.details["verified"]
    assert result.details["stop_reason"] == original["summary"]["stop_reason"]
    assert result.details["requests"] == original["requests"]


def test_corrupt_seal_is_not_downgraded_to_partial(tmp_path):
    root = fixture_run(tmp_path)
    (root / "events.jsonl").write_bytes(b"{}\n")
    with pytest.raises(EvidenceError):
        execute(CommandRequest("verify", run=root))
