"""Project saved trial evidence into the single JSON document consumed by Studio."""

from inferyard import SCHEMA_VERSION
from inferyard.analysis.performance import request_timing
from inferyard.contracts.validation import json_bytes, validate_document
from inferyard.errors import EvidenceError
from inferyard.evidence.storage import atomic_bytes, local_file, new_output, read_json
from inferyard.reporting.comparison_report import comparison_input
from inferyard.reporting.studio_dashboard import CATEGORIES, dashboard_view
from inferyard.reporting.studio_profile import profile_view
from inferyard.reporting.studio_resources import memory_view

KIND = "studio-report.v1"


def _threads(value):
    return value if type(value) is int and value >= 0 else None


def _run_view(data, source):
    config, summary = data["config"], data["summary"]
    return {
        "source": {"run_id": source["run_id"], "path": source["path"]},
        "model": config["model"]["display_name"],
        "threads": _threads(config["conditions"].get("threads")),
        "diagnostic": data["run"]["diagnostic"],
        "facets": {
            "bundle_label": f"{data['bundle']['bundle_id']} · {data['bundle']['version']}"
        },
        "profile": profile_view(data),
        "summary": {
            "completion_rate": {
                key: summary["completion_rate"][key]
                for key in ("numerator", "denominator", "excluded")
            },
            "stop_reason": summary["stop_reason"],
        },
        "dashboard": dashboard_view(data, memory_view(data.get("samples") or [])),
        "requests": [
            {
                "case_id": request["case_id"],
                "category_label": CATEGORIES.get(request["category"], request["category"]),
                "state": request["execution_state"],
                "quality": (request.get("score") or {}).get("quality_state"),
                "latency_ms": request_timing(request)["L03"]["value"],
            }
            for request in data["requests"]
        ],
    }


def build_studio_report(roots, *, loaded=None, source_metadata=None):
    """Project sources into one document; nothing is written or re-scored here."""
    roots = list(roots)
    if not roots or len(roots) > 100:
        raise EvidenceError("report_requires_1_to_100_trials")
    source_metadata = {} if source_metadata is None else dict(source_metadata)

    def read(root):
        metadata = source_metadata.setdefault(root.resolve(), {})
        return comparison_input(root, bind_unsealed=True, metadata=metadata)

    inputs = loaded if loaded is not None else (read(root) for root in roots)
    runs, ids = [], set()
    for data, source in inputs:
        if source["run_id"] in ids:
            raise EvidenceError("duplicate_report_run")
        ids.add(source["run_id"])
        runs.append(_run_view(data, source))
        # Presentation owns its derived fields; do not retain raw samples across runs.
        del data, source
    document = {"schema_version": SCHEMA_VERSION, "kind": KIND, "runs": runs}
    validate_document("studio_report", document)
    return document


def write_studio_report(roots, out, *, loaded=None, source_metadata=None):
    roots = list(roots)
    document = build_studio_report(roots, loaded=loaded, source_metadata=source_metadata)
    new_output(out, roots)
    atomic_bytes(out / "studio.json", json_bytes(document))
    return document


def read_studio_report(root):
    """Read the saved structure only; sources are not re-read and nothing is verified."""
    document = read_json(local_file(root, "studio.json"))
    validate_document("studio_report", document)
    return document
