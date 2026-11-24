"""Offline verification retains dedicated semantics and refuses weak fallbacks."""

import hashlib
from dataclasses import replace

import pytest

from inferyard.application.types import CommandRequest
from inferyard.application.verification import execute
from inferyard.config.loader import load_config
from inferyard.contracts.validation import Document
from inferyard.errors import EvidenceError
from tests.helpers import fixture_run


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

    with pytest.MonkeyPatch.context() as patched:
        patched.setattr("tests.helpers.load_config", portable_config)
        return fixture_run(root, **kwargs)


def _extension_spec():
    return {
        "schema_version": 3,
        "definition": "closed_concurrency.v1",
        "case_ids": ["a", "b", "c", "d"],
        "concurrency": 2,
        "server_slots": 4,
        "timeout_seconds": 1,
        "drain_timeout_seconds": 0.1,
        "max_wall_seconds": 3,
        "latency_limit_ms": 100,
        "require_quality": True,
        "max_dispatch_delay_ns": 1_000_000_000,
        "max_client_cpu_fraction": 0.5,
    }


def _extension_rows():
    return [
        dict(
            case_id=c,
            request_id=c,
            slot_id=i % 2,
            send_ns=1 + i // 2 * 100,
            end_ns=101 + i // 2 * 100,
            drain_end_ns=101 + i // 2 * 100,
            state="completed",
            protocol_complete=True,
            quality_pass=i != 3,
            idle_confirmed=True,
            dispatch_delay_ns=1,
        )
        for i, c in enumerate(_extension_spec()["case_ids"])
    ]


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

        out, _ = save_packet(
            out, {"spec": _extension_spec(), "rows": _extension_rows(), "evidence_kind": "fixture"}
        )
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
