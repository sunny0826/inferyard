"""Offline verification retains dedicated semantics and refuses weak fallbacks."""

import hashlib
import json
import os
import socket
import subprocess
from dataclasses import replace
from importlib import import_module
from pathlib import Path

import pytest

from inferyard import cli
from inferyard.application.types import CommandRequest, CommandResult
from inferyard.application.verification import execute
from inferyard.config.loader import load_config
from inferyard.contracts.validation import Document, json_bytes
from inferyard.errors import ContractError, EvidenceError
from inferyard.evidence.storage import read_json
from tests.helpers import fixture_run, symlink_or_skip


def marker(root, kind, filename):
    value = {}
    if kind == "overhead":
        value = {
            "protocol_sha256": "a" * 64,
            "status": "incomplete",
            "passed": False,
            "limitations": ["four_completed_runs_required"],
        }
    (root / filename).write_bytes(json_bytes(value))


def digest_tree(root):
    return {
        str(path.relative_to(root)): hashlib.sha256(path.read_bytes()).hexdigest()
        for path in root.rglob("*")
        if path.is_file()
    }


def source_fixture(root, **kwargs):
    def portable_config(path):
        loaded = load_config(path)
        config = loaded.config.to_dict()
        config["engine"]["startup_args"] = ["-m", config["model"]["local_path"]]
        return replace(loaded, config=Document.parse("config", config))

    # Supply a valid declared model binding before the journal is constructed.
    with pytest.MonkeyPatch.context() as patched:
        patched.setattr("tests.helpers.load_config", portable_config)
        return fixture_run(root, **kwargs)


def make_artifact(tmp_path, kind):
    root = source_fixture(tmp_path / "sources")
    out = tmp_path / "artifact"
    if kind == "export":
        from inferyard.reporting.export import write_export

        write_export(out, run=root)
    elif kind == "rescore":
        from inferyard.reporting.rescore import write_rescore

        write_rescore(root, out, scorer_id="phase2.v2", reason="offline fixture regression")
    elif kind == "public":
        from inferyard.reporting.public_package import write_public

        write_public(root, out)
    elif kind == "report":
        from inferyard.reporting.studio_report import write_studio_report

        write_studio_report([root], out)
    elif kind == "comparison":
        from inferyard.application.reporting import execute as compare

        right = source_fixture(tmp_path / "sources", model="synthetic-B")
        compare(CommandRequest("compare", left=root, right=right, out=out))
    elif kind == "extension":
        from inferyard.extensions.extension_evidence import save_packet
        from tests.unit.test_closed_concurrency import rows, spec

        out, _ = save_packet(out, {"spec": spec(), "rows": rows(), "evidence_kind": "fixture"})
    return root, out


@pytest.mark.parametrize(
    "kind,filename",
    [
        ("export", "metrics.csv"),
        ("rescore", "parent-analysis.json"),
        ("public", "REPRODUCE.md"),
        ("comparison", "comparison.json"),
        ("extension", "summary.json"),
    ],
)
def test_real_payload_corruption_is_rejected(tmp_path, kind, filename):
    _, out = make_artifact(tmp_path, kind)
    with (out / filename).open("ab") as stream:
        stream.write(b"tampered")
    before = digest_tree(tmp_path)
    with pytest.raises(EvidenceError):
        execute(CommandRequest("verify", run=out))
    assert before == digest_tree(tmp_path)


def loaded_source(root):
    loaded = load_config(Path(__file__).parents[1] / "fixtures/config/valid.toml")
    return replace(
        loaded,
        config=Document.parse("config", read_json(root / "config.frozen.json")),
        bundle=Document.parse("bundle", read_json(root / "bundle.json")),
    )
