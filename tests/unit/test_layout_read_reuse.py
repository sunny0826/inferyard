"""Source readers belong to one command and retain real reference validation."""

from collections import Counter
from copy import deepcopy
from pathlib import Path

import pytest

from inferyard.application.types import CommandRequest
from inferyard.evidence.storage import EvidenceError
from inferyard.evidence.trial_evidence import TrialEvidence
from inferyard.reporting.comparison_report import (
    comparison_input,
    verify_metric_references,
)
from inferyard.reporting.comparison_report import (
    execute as compare,
)
from inferyard.reporting.export import write_export
from inferyard.reporting.public_package import write_public
from inferyard.reporting.report import verify_report, write_report
from inferyard.reporting.rescore import write_rescore
from tests.evidence_layout_helpers import build_current_original, pack_layout2
from tests.unit.test_layout_surfaces import sources as sources


def count_source_reads(monkeypatch, roots):
    counts = Counter()
    original = Path.open

    def opened(path, *args, **kwargs):
        if path.parent in roots:
            counts[path.parent, path.name] += 1
        return original(path, *args, **kwargs)

    monkeypatch.setattr(Path, "open", opened)
    return counts


@pytest.mark.parametrize("layout", [1, 2])
@pytest.mark.parametrize(
    "surface",
    ["report", "report-check", "compare", "compare-report-check", "export", "rescore", "public"],
)
def test_external_commands_read_each_original_once(sources, tmp_path, monkeypatch, layout, surface):
    old, new = sources
    root = old if layout == 1 else new
    roots = [root]
    out = tmp_path / "derived"
    if surface.startswith("compare"):
        other = build_current_original(tmp_path / "other")
        roots.append(other if layout == 1 else pack_layout2(other, tmp_path / "other-container"))
        request = CommandRequest("compare", left=roots[0], right=roots[1], out=out)
        if surface.endswith("check"):
            assert compare(request)[0] == 0
    elif surface == "report-check":
        write_report(roots, out)
    counts = count_source_reads(monkeypatch, roots)
    if surface.endswith("check"):
        assert verify_report(out)["verified"]
    elif surface == "report":
        write_report(roots, out)
    elif surface == "compare":
        assert compare(request)[0] == 0
    elif surface == "export":
        write_export(out, run=root)
    elif surface == "rescore":
        write_rescore(root, out, scorer_id="phase2.v2", reason="read-bound regression")
    else:
        write_public(root, out)
    for source in roots:
        names = ["events.jsonl", "memory.jsonl"] + (
            ["inputs.json", "observations.jsonl"]
            if layout == 2
            else ["config.frozen.json", "bundle.json", "environment.start.json"]
        )
        assert {name: counts[source, name] for name in names} == dict.fromkeys(names, 1)


@pytest.mark.parametrize("layout", [1, 2])
def test_read_bound_metric_validation_rejects_wrong_references(sources, layout):
    root = sources[layout - 1]
    metadata = {}
    data, _ = comparison_input(root, metadata=metadata)
    metrics = deepcopy(data["summary"]["metric_observations"])
    ref = next(ref for metric in metrics for ref in metric["evidence_refs"])
    ref["sha256"] = "0" * 64
    with pytest.raises(EvidenceError, match="evidence_reference_hash_mismatch"):
        verify_metric_references(root, metrics, reads=metadata["evidence"])
    if layout == 2:
        evidence = metadata["evidence"]
        ref = evidence.input_reference("config")
        ref["member"]["sha256"] = "0" * 64
        with pytest.raises(EvidenceError, match="evidence_reference_member_mismatch"):
            verify_metric_references(root, [{"evidence_refs": [ref]}], reads=evidence)


@pytest.mark.parametrize("layout", [1, 2])
def test_next_command_rechecks_changed_original_bytes(sources, tmp_path, layout):
    root = sources[layout - 1]
    out = tmp_path / "report"
    write_report([root], out)
    assert verify_report(out)["verified"]
    with (root / "events.jsonl").open("ab") as stream:
        stream.write(b"\n")
    with pytest.raises(EvidenceError, match="original_evidence_hash_mismatch"):
        verify_report(out)


def test_standalone_member_references_share_one_source_reader(sources, monkeypatch):
    old, root = sources
    evidence = TrialEvidence(root)
    refs = [
        evidence.observation_reference("environment", stage=stage) for stage in ("start", "end")
    ]
    assert all(ref is not None for ref in refs)
    counts = count_source_reads(monkeypatch, [root])
    verify_metric_references(root, [{"evidence_refs": refs + refs}])
    assert counts[root, "inputs.json"] == counts[root, "observations.jsonl"] == 1
    assert counts[old, "events.jsonl"] == 0
    refs[1]["member"]["sha256"] = "0" * 64
    with pytest.raises(EvidenceError, match="evidence_reference_member_mismatch"):
        verify_metric_references(root, [{"evidence_refs": refs}])
