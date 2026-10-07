"""Version seven verifies saved bytes and source semantics without invoking a renderer."""

from copy import deepcopy

from inferyard.contracts.validation import strict_json_loads
from inferyard.evidence.artifact_seal import read_sealed, seal
from inferyard.evidence.source_locations import (
    relative_comparison,
    relative_source,
    resolve_source,
    retain_comparison_locations,
)
from inferyard.evidence.storage import EvidenceError, json_bytes


def portable_index(index, out, comparison_path=None):
    index = deepcopy(index)
    index["comparison"] = deepcopy(index["comparison"])
    for run in index["runs"]:
        run["source"] = dict(run["source"])
        run["source"]["path"] = relative_source(run["source"]["path"], out)
    if "comparison_source" in index:
        index["comparison_source"]["path"] = relative_source(comparison_path, out)
    if index["comparison"] and index["comparison"].get("source_runs"):
        from inferyard.evidence.source_locations import comparison_locations

        if index["comparison"].get("format_version") == 4:
            for ref in comparison_locations(index["comparison"]):
                ref["path"] = str(resolve_source(comparison_path, ref["path"]))
        relative_comparison(index["comparison"], out)
    return index


def seal_report(out, *, comparison=False):
    seal(out, ["index.json", "report.html", *(["comparison.json"] if comparison else [])])


def verify(out, saved, *, options):
    from inferyard.reporting.comparison_report import (
        comparison_input,
        read_verified_comparison,
    )
    from inferyard.reporting.report import build_index

    blobs = read_sealed(
        out,
        [
            "index.json",
            "report.html",
            *(["comparison.json"] if (out / "comparison.json").exists() else []),
        ],
    )
    if json_bytes(saved) != json_bytes(strict_json_loads(blobs["index.json"].decode())):
        raise EvidenceError("report_changed_during_read")
    roots = [
        resolve_source(out, run["source"]["path"], options.source_roots) for run in saved["runs"]
    ]
    loaded = [comparison_input(root, bind_unsealed=True) for root in roots]
    linked, comparison = None, None
    if "comparison_source" in saved:
        linked = resolve_source(out, saved["comparison_source"]["path"], options.source_roots)
        comparison = read_verified_comparison(
            linked, loaded=loaded, source_roots=options.source_roots
        )
    expected = build_index(
        roots,
        out,
        producer=saved["generator_source_sha256"],
        comparison_path=linked,
        format_version=7,
        loaded=loaded,
        verified_comparison=comparison,
    )
    for key in ("generator_source_sha256", "template_sha256"):
        value = saved[key]
        if (
            type(value) is not str
            or len(value) != 64
            or any(c not in "0123456789abcdef" for c in value)
        ):
            raise EvidenceError("invalid_report_producer")
    # Renderer identity is provenance, not authority for source-derived results.
    expected["template_sha256"] = saved["template_sha256"]
    from inferyard.reporting.report_common import evidence_url

    for actual, original in zip(expected["runs"], saved["runs"], strict=True):
        actual["source"]["path"] = original["source"]["path"]
        actual["evidence"] = {
            name: evidence_url(out / original["source"]["path"] / name, out)
            for name in actual["evidence"]
        }
    if linked is not None:
        expected["comparison_source"]["path"] = saved["comparison_source"]["path"]
        expected["comparison_source"]["url"] = evidence_url(
            out / saved["comparison_source"]["path"] / "comparison.json", out
        )
        retain_comparison_locations(expected["comparison"], saved["comparison"])
    if json_bytes(expected) != json_bytes(saved):
        raise EvidenceError("report_index_recomputation_mismatch")
    if options.rerender:
        from inferyard.reporting.report_common import _environment, render_report_html

        if (
            render_report_html(_environment(), "report.html", saved).encode()
            != blobs["report.html"]
        ):
            raise EvidenceError("report_html_recomputation_mismatch")
    return {
        "verified": True,
        "source_runs": len(roots),
        "bytes_verified": True,
        "sources_verified": True,
        "semantic_verified": True,
        "render_checked": options.rerender,
    }
