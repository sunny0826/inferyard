"""Same-base quantization qualification requires replayed receipts and file bytes."""

from inferyard.config.lineage_records import validate_records
from inferyard.contracts.validation import ContractError
from inferyard.evidence.lineage_files import verify_lineage_files

BASE_FIELDS = (
    "base_repo",
    "base_revision",
    "base_artifacts",
    "tokenizer_sha256",
    "conversion_tool_sha256",
    "conversion_recipe_sha256",
    "quantization_tool_sha256",
)


def qualification(left, right):
    """No hash-only receipt or manually supplied verification boolean grants a pass.

    read_trial keeps the manifest-bound config; optional bindings refer to actual
    mounted artifacts, streamed and hashed again on the verification machine.
    """
    reasons, lineages, files = [], [], []
    for label, data in (("left", left), ("right", right)):
        try:
            trial = next(
                t for t in data["plan"]["trials"] if t["trial_id"] == data["run"]["trial_id"]
            )
            workload = next(
                w
                for w in data["plan"]["experiment"]["workloads"]
                if w["workload_id"] == trial["workload_id"]
            )
            lineage = workload.get("model_lineage")
            if not lineage or not workload.get("model_lineage_records"):
                reasons.append(label + ":lineage_or_receipts_missing")
                continue
            validate_records(workload)
            if (
                lineage["output_sha256"] != data["config"]["model"]["sha256"]
                or lineage["packing"] != data["config"]["model"]["packing"]
            ):
                reasons.append(label + ":output_binding_mismatch")
            binding = data["config"].get("quantization_artifact_binding")
            if not isinstance(binding, dict) or set(binding) != {"root", "files"}:
                reasons.append(label + ":artifact_file_bindings_missing")
            else:
                verified = verify_lineage_files(workload, binding["files"], binding["root"])
                files.append({"side": label, **verified})
                if not verified["all_files_verified"]:
                    reasons.append(label + ":artifact_files_not_verified")
            lineages.append(lineage)
        except KeyError, StopIteration, TypeError, ContractError, OSError, ValueError:
            reasons.append(label + ":lineage_invalid")
    if len(lineages) != 2:
        reasons.append("two_complete_lineages_required")
    else:
        for key in BASE_FIELDS:
            a, b = (lineage[key] for lineage in lineages)
            if key == "base_artifacts":
                a, b = (sorted(v, key=lambda x: x["name"]) for v in (a, b))
            if a != b:
                reasons.append("same_base_mismatch:" + key)
        if (
            lineages[0]["packing"] == lineages[1]["packing"]
            or lineages[0]["output_sha256"] == lineages[1]["output_sha256"]
        ):
            reasons.append("quantization_factor_not_different")
    return {
        "definition": "same_base_quantization.v1",
        "eligible": not reasons,
        "reasons": sorted(set(reasons)),
        "file_verification": files,
        "limitations": [
            "local_file_and_receipt_identity_not_author_authentication",
            "performance_qualification_is_separate",
        ],
    }


def verified_tokenizer_equal(left, right, qualification_result):
    """Consume this comparison's verified qualification; do not reopen lineage files."""
    if not qualification_result or qualification_result.get("eligible") is not True:
        return False
    tokens, templates = [], []
    for data in (left, right):
        trial = next(t for t in data["plan"]["trials"] if t["trial_id"] == data["run"]["trial_id"])
        workload = next(
            w
            for w in data["plan"]["experiment"]["workloads"]
            if w["workload_id"] == trial["workload_id"]
        )
        tokens.append(workload.get("model_lineage", {}).get("tokenizer_sha256"))
        templates.append(data["config"]["model"].get("template_sha256"))
    return bool(
        tokens[0] and tokens[0] == tokens[1] and templates[0] and templates[0] == templates[1]
    )
