"""Cross-run environment checks for independently replayed ABBA evidence.

Endpoint agreement is weaker than request-window qualification. In particular,
resource-off arms must not inherit the environment eligibility of an on arm.
"""

from inferyard.analysis.environment_identity import fields as identity_fields
from inferyard.evidence.trial_reads import TrialReads


def environment_record(root, data, *, require_boundary=False):
    reads = TrialReads(root)
    manifest = reads.checked_json("manifest.json")
    sealed = set(manifest["files"])
    endpoints, evidence, reasons = [], [], []
    for name in ("environment.start.json", "environment.end.json"):
        if name not in sealed:
            reasons.append("environment_endpoint_unsealed_or_missing:" + name)
            endpoints.append({})
            continue
        endpoints.append(reads.checked_json(name))
        evidence.append({"path": name, "sha256": reads.hashes[name]})
    qualification = data["summary"]["measurement_context"]["environment_qualification"]
    boundary_only = (
        "collector.json" in sealed
        and reads.checked_json("collector.json").get("collector") == "boundary-only.v1"
    )
    if boundary_only or require_boundary:
        from inferyard.analysis.boundary_environment import qualify_boundary_environment

        bracketed = qualify_boundary_environment(root, data, sealed, reads=reads)
        evidence.extend(bracketed["evidence_refs"])
        qualification = (
            bracketed
            if boundary_only
            else {
                "definition": "periodic_and_outside_request_environment.v1",
                "eligible": qualification["eligible"] and bracketed["eligible"],
                "reasons": sorted(set(qualification["reasons"] + bracketed["reasons"])),
                "limitations": qualification.get("limitations", [])
                + bracketed.get("limitations", []),
            }
        )
    return {
        "run_id": data["run"]["run_id"],
        "endpoints": endpoints,
        "evidence_refs": evidence,
        "endpoint_reasons": reasons,
        "qualification": qualification,
    }


def assess_environments(arms, target):
    records = [*arms, target]
    reasons = set()
    if len(arms) != 4:
        reasons.add("four_arm_environments_required")
    fields = identity_fields([e for r in records for e in r["endpoints"]], include_policy=True)
    baseline = None
    details = []
    for index, record in enumerate(records):
        label = "target" if index == len(arms) else f"arm_{index + 1}"
        local = set(record["endpoint_reasons"])
        for endpoint in record["endpoints"]:
            identity = {field: endpoint.get(field) for field in fields}
            for field, value in identity.items():
                if value is None:
                    local.add("environment_identity_unknown:" + field)
            if baseline is None:
                baseline = identity
            else:
                for field in fields:
                    if identity[field] != baseline[field]:
                        local.add("environment_identity_changed:" + field)
        qualification = record["qualification"]
        if not qualification["eligible"]:
            local.add("request_window_environment_not_qualified")
            local.update(qualification["reasons"])
        reasons.update(label + ":" + reason for reason in local)
        details.append(
            {
                "role": label,
                "run_id": record["run_id"],
                "eligible": not local,
                "reasons": sorted(local),
                "evidence_refs": record["evidence_refs"],
                "qualification_definition": qualification.get("definition"),
                "limitations": qualification.get("limitations", []),
            }
        )
    return {
        "definition": "abba_target_environment_qualification.v1",
        "eligible": not reasons,
        "reasons": sorted(reasons),
        "runs": details,
        "limitations": [
            "endpoint_agreement_does_not_prove_request_window_stationarity",
            "resource_off_arms_without_observations_remain_unqualified",
            "no_environment_eligibility_inherited_from_other_arms",
            "performance_comparison_requires_other_metric_and_protocol_gates",
        ],
    }
