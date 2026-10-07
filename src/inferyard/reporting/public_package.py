"""Local, allowlisted public candidate summary; never publishes or copies raw evidence."""

import hashlib
import math
import re

from inferyard.analysis.public_metrics import project_metrics
from inferyard.application.types import CommandResult
from inferyard.config.public_recipe import reproduction_recipe
from inferyard.evidence.formats import require_version
from inferyard.evidence.storage import (
    EvidenceError,
    atomic_bytes,
    json_bytes,
    local_file,
    read_json,
    sha256_file,
)
from inferyard.reporting.comparison_report import comparison_input
from inferyard.reporting.report_common import _new_output

POLICY = "public-summary.v5"
REMOVED = [
    "local_paths",
    "endpoint_and_credentials",
    "process_and_host_identifiers",
    "raw_prompts_answers_and_events",
    "free_text_labels_and_notes",
    "per_request_metrics_and_sensor_sources",
    "source_file_references",
]
LIMITS = [
    "candidate_summary_only_not_full_evidence_package",
    "integrity_hashes_are_not_source_authentication",
    "raw_evidence_required_for_independent_score_recomputation",
    "no_performance_comparison_or_automatic_recommendation",
]
README = """# Public candidate summary

This package is generated locally. No upload or publication is performed.
Verify with: inferyard public-check --run PACKAGE_DIRECTORY

Match model, engine, template, bundle and scorer hashes in candidate.json before
reproduction. Obtain those artifacts separately; the package contains no model,
prompts, answers, credentials, endpoint, local paths or raw execution evidence.
Use the numeric recipe with your own local paths, endpoint and bound service PID.
Freeze a new plan, preflight the service and run the same approved bundle. Preserve
failed requests in denominators. This summary does not replace the frozen plan or
the original raw evidence and cannot independently reproduce the original scores.

redactions.json enumerates omitted sections. manifest.json hashes every payload.
Hashes detect changes, not publisher identity or truth of model measurements.
"""


def number(value):
    if value is None:
        return None
    if type(value) not in (int, float) or not math.isfinite(value):
        raise EvidenceError("invalid_public_numeric_field")
    return value


def digest(value):
    if not isinstance(value, str) or not re.fullmatch(r"[0-9a-f]{64}", value):
        raise EvidenceError("invalid_public_identity_hash")
    return value


def projection(data, source, *, policy=POLICY):
    require_version({"policy": policy}, "policy", (POLICY,), "public-summary")
    config, summary = data["config"], data["summary"]
    counts = {
        key: number(summary["counts"].get(key))
        for key in ("planned", "completed", "failed", "invalid", "cancelled", "not_executed")
    }
    quality = {}
    for category in ("instruction", "extraction", "qa", "math", "classification", "structured"):
        row = summary.get("quality", {}).get("Q01", {}).get(category)
        if row is not None:
            quality[category] = {
                key: number(row["rate"].get(key))
                for key in ("numerator", "denominator", "excluded", "value")
            }
    candidate = {
        "format_version": int(policy.rsplit("v", 1)[1]),
        "policy": policy,
        "source_manifest_sha256": digest(source["manifest_sha256"]),
        "identities": {
            "model_sha256": digest(config["model"]["sha256"]),
            "engine_sha256": digest(config["engine"]["binary_sha256"]),
            "template_sha256": digest(config["model"]["template_sha256"]),
            "bundle_sha256": digest(data["selection"]["bundle_sha256"]),
            "scorer_sha256": digest(data["selection"]["scorer_sha256"]),
        },
        "recipe": {
            "generation": {
                k: number(config["generation"][k])
                for k in (
                    "seed",
                    "temperature",
                    "top_k",
                    "top_p",
                    "min_p",
                    "max_tokens",
                    "presence_penalty",
                    "repeat_penalty",
                )
            },
            "conditions": {
                k: number(config["conditions"][k])
                for k in (
                    "threads",
                    "threads_batch",
                    "context_size",
                    "slots",
                )
            },
        },
        "scope_complete": summary.get("scope_complete") is True,
        "counts": counts,
        "quality_Q01": quality,
        "limitations": LIMITS,
    }
    candidate["reproduction"] = reproduction_recipe(
        data,
        number,
    )
    candidate["metrics"] = project_metrics(summary["metric_observations"], number)
    candidate["candidate_id"] = (
        "candidate-" + hashlib.sha256(json_bytes(candidate)).hexdigest()[:32]
    )
    return candidate


def payloads(candidate):
    instructions = README
    instructions += (
        "\n## Reproduction conditions\n\n"
        "The reproduction section includes cache/reasoning mode, power policy, "
        "execution and telemetry settings, workload timing, order and budgets. "
        "case_order uses zero-based indices into the hash-matched bundle; verify "
        "each case_sha256 using canonical JSON before running.\n\n"
        "locally_required_sha256 lists redacted values that must be supplied from "
        "local artifacts. Null policy labels mean undisclosed/unknown, not default. "
        "A missing safety section means no periodic safety policy was frozen. "
        "Do not treat this as a ready-to-run configuration or infer equivalence "
        "when any required local value is unavailable.\n"
    )
    instructions += (
        "\n## Local binding check\n\n"
        "Run: inferyard public-config-check --run PACKAGE_DIRECTORY --config LOCAL_CONFIG\n\n"
        "Model/template paths and loopback host/port can change. The portable startup "
        "fingerprint retains all other arguments and their order; duplicate local "
        "bindings are rejected. Artifact hashes remain bound separately. This checks "
        "declared configuration only, not artifact bytes or a running service. "
        "A match still requires a new frozen plan and runtime preflight.\n"
    )
    instructions += (
        "\n## Freeze the reproduced workload\n\n"
        "Run: inferyard public-plan --run PACKAGE_DIRECTORY --config LOCAL_CONFIG "
        "--out NEW_PRIVATE_DIRECTORY\n\n"
        "This creates a private input snapshot and frozen/plan.json, with all repeats "
        "and exact per-repeat case orders for the selected workload. Other workloads "
        "from the original experiment are not included. No generation is performed. "
        "Local model files, service preparation and live preflight are still required; "
        "run the frozen plan only after those checks. The private output contains "
        "local configuration and task text; it is not a public package.\n"
    )
    removed = REMOVED
    removed = [item for item in REMOVED if item != "per_request_metrics_and_sensor_sources"] + [
        "unreviewed_metric_labels_and_sensor_source_text",
        "absolute_sample_clocks_and_raw_request_identifiers",
    ]
    instructions += (
        "\n## Public metric observations\n\n"
        "metrics preserves every source observation and numeric value, null, denominator, "
        "sample count and exclusion. Text descriptors contain value, sha256 and redacted; "
        "unknown labels and sensor paths are hashed, never silently combined. A redacted "
        "missing reason is distinct from no missing reason. Request/workload identities "
        "are hashed; absolute clocks and raw evidence references are omitted. Hashes "
        "are linkable and do not guarantee anonymity. Source eligibility does not authorize "
        "public performance comparisons. Source-backed verification requires "
        "original evidence.\n"
    )
    return {
        "candidate.json": json_bytes(candidate),
        "redactions.json": json_bytes({"policy": candidate["policy"], "omitted_sections": removed}),
        "REPRODUCE.md": instructions.encode(),
    }


def write_public(root, out):
    data, source = comparison_input(root)
    candidate = projection(data, source)
    files = payloads(candidate)
    _new_output(out, [root])
    for name, content in files.items():
        atomic_bytes(out / name, content)
    manifest = {
        "policy": POLICY,
        "candidate_id": candidate["candidate_id"],
        "files": {name: hashlib.sha256(content).hexdigest() for name, content in files.items()},
    }
    atomic_bytes(out / "manifest.json", json_bytes(manifest))
    return manifest


def verify_public(out, source_root=None):
    saved = read_json(local_file(out, "manifest.json"))
    expected_names = {"candidate.json", "redactions.json", "REPRODUCE.md"}
    if (
        type(saved) is not dict
        or type(saved.get("files")) is not dict
        or set(saved["files"]) != expected_names
    ):
        raise EvidenceError("public_inventory_mismatch")
    if {p.name for p in out.iterdir()} != expected_names | {"manifest.json"}:
        raise EvidenceError("public_unlisted_files")
    for name, expected in saved["files"].items():
        if sha256_file(local_file(out, name)) != expected:
            raise EvidenceError("public_hash_mismatch")
    candidate = read_json(local_file(out, "candidate.json"))
    if (
        type(candidate) is not dict
        or type(saved.get("policy")) is not str
        or not re.fullmatch(r"public-summary\.v[0-9]+", saved["policy"])
    ):
        raise EvidenceError("public_policy_mismatch")
    if (
        candidate.get("policy") != saved["policy"]
        or type(candidate.get("format_version")) is not int
        or candidate["format_version"] != int(saved["policy"].rsplit("v", 1)[1])
    ):
        raise EvidenceError("public_policy_mismatch")
    require_version(saved, "policy", (POLICY,), "public-summary")
    require_version(candidate, "format_version", (5,), "public-summary")
    identity = candidate.pop("candidate_id")
    if identity != "candidate-" + hashlib.sha256(json_bytes(candidate)).hexdigest()[:32]:
        raise EvidenceError("public_candidate_identity_mismatch")
    if identity != saved["candidate_id"]:
        raise EvidenceError("public_candidate_identity_mismatch")
    candidate["candidate_id"] = identity
    for name, expected in payloads(candidate).items():
        if name != "candidate.json" and local_file(out, name).read_bytes() != expected:
            raise EvidenceError("public_policy_payload_mismatch")
    from inferyard.analysis.public_metrics_check import validate_metrics

    validate_metrics(candidate.get("metrics"))
    result = {
        "integrity_verified": True,
        "policy_consistency_verified": True,
        "candidate_id": identity,
        "source_authenticated": False,
    }
    if source_root is not None:
        data, source = comparison_input(source_root)
        expected = payloads(projection(data, source, policy=saved["policy"]))
        if any(local_file(out, name).read_bytes() != content for name, content in expected.items()):
            raise EvidenceError("public_source_projection_mismatch")
        result["source_projection_verified"] = True
    return result


def execute(request):
    result = (
        verify_public(request.run, request.from_run)
        if request.command == "public-check"
        else write_public(request.run, request.out)
    )
    return 0, CommandResult(
        request.command,
        "verified" if request.command == "public-check" else "exported",
        "complete",
        details=result,
        limitations=tuple(LIMITS),
    )
