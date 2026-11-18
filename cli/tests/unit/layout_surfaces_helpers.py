"""External read surfaces bind synthetic layout 2 originals without changing source bytes."""

import shutil
from copy import deepcopy
from dataclasses import replace

import pytest

from inferyard.application.types import CommandRequest
from inferyard.evidence.verification_options import VerificationOptions
from inferyard.application.verification import execute
from inferyard.cli import main
from inferyard.config.loader import load_config
from inferyard.contracts.validation import Document, json_bytes
from inferyard.errors import EvidenceError, PreflightError
from inferyard.evidence.storage import read_json, read_jsonl, sha256_file
from inferyard.evidence.trial_evidence import TrialEvidence
from inferyard.analysis.comparison_input import comparison_input
from inferyard.reporting.comparison_report import verify_comparison
from inferyard.application.reporting import execute as compare
from inferyard.reporting.export import (
    existing_analysis,
    trial_analysis,
    verify_export,
    write_export,
)
from inferyard.reporting.public_package import verify_public, write_public
from inferyard.reporting.studio_report import write_studio_report
from inferyard.reporting.rescore import verify_rescore, write_rescore
from inferyard.runtime.batch_state import remaining_budget
from inferyard.runtime.overhead_binding import bind_target, sealed_observation, workload_identity
from inferyard.runtime.runner import load_rerun
from tests.evidence_layout_helpers import build_current_original, pack_layout2, seal_layout


def tree_bytes(root):
    return {
        str(path.relative_to(root)): path.read_bytes() for path in root.rglob("*") if path.is_file()
    }


@pytest.fixture
def sources(tmp_path, monkeypatch):
    def portable_config(path):
        loaded = load_config(path)
        config = loaded.config.to_dict()
        config["engine"]["startup_args"] = ["-m", config["model"]["local_path"]]
        return replace(loaded, config=Document.parse("config", config))

    monkeypatch.setattr("inferyard.config.loader.load_config", portable_config)
    old = build_current_original(tmp_path / "legacy-original")
    new = pack_layout2(old, tmp_path / "container-original")
    return old, new
