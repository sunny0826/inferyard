"""Recheck archived lab token budgets and both effective-parameter probes offline."""

from inferyard.adapters.lab_parameters import (
    _equal,
    effective_evidence,
    response,
    validate_budget,
    validate_parameters,
)
from inferyard.evidence.engine_capabilities import observation_sealed
from inferyard.evidence.storage import EvidenceError, json_bytes
from inferyard.evidence.token_budgets import probe_budget, read_budgets
from inferyard.evidence.trial_evidence import TrialEvidence
from inferyard.platforms.identity import PreflightError


def verify_parameters(root, config, props, events, bodies, usage, manifest, *, reads=None):
    reads = reads if reads is not None else TrialEvidence(root)
    successful = any(
        e["event_type"] == "run_stopped"
        and e["data"]["reason"] in ("plan_finished", "check_only_no_formal_cases")
        for e in events
    )
    try:
        budgets = _budgets(root, config, props, manifest, required=successful, reads=reads)
        probes = [
            e
            for e in events
            if e["event_type"] == "request_finished"
            and e["phase"] == "probe"
            and e["data"]["execution_state"] == "completed"
        ]
        if successful and len(probes) != 2:
            raise EvidenceError("lab_parameter_probes_missing")
        for event in probes:
            body = bodies[event["request_id"]]
            stage = "stream" if body["stream"] else "ordinary"
            if not reads.has_observation("effective_parameters", stage=stage):
                if successful:
                    raise EvidenceError("lab_parameter_evidence_missing")
                continue  # A completed probe can still fail its parameter preflight.
            if manifest is not None and not observation_sealed(
                reads, "effective_parameters", manifest, stage=stage
            ):
                raise EvidenceError("lab_parameter_evidence_unsealed")
            evidence = reads.observation("effective_parameters", stage=stage)
            value = response(
                json_bytes(evidence["lab_parameters"]),
                props,
                {"request_id", "generation", "conditions", "auxiliary_routes"},
                request_id=body["lab_request_id"],
            )
            validate_parameters(value, config)
            if not _equal(evidence, effective_evidence(value, config)):
                raise EvidenceError("lab_parameter_evidence_mismatch")
            if (
                budgets is None
                or usage[event["request_id"]]["prompt_tokens"]
                != probe_budget(budgets)["input_tokens"]
            ):
                raise EvidenceError("lab_template_usage_mismatch")
    except EvidenceError as exc:
        if str(exc) == "original_evidence_unsealed":
            raise EvidenceError("lab_parameter_evidence_unsealed") from exc
        raise
    except (PreflightError, KeyError, TypeError, ValueError) as exc:
        raise EvidenceError("lab_parameter_evidence_invalid") from exc


def _budgets(root, config, props, manifest, *, required, reads=None):
    selected = reads.input("selection")["case_ids"]
    kind = reads.input("run")["kind"]
    _, budgets = read_budgets(root, config, selected, kind=kind, manifest=manifest, reads=reads)
    if budgets is None:
        if required:
            raise EvidenceError("lab_token_budget_evidence_missing")
        return None
    records = [entry["budget"] for entry in budgets["entries"]]
    for record in records:
        if type(record) is not dict or set(record) != {
            "input_tokens",
            "output_budget",
            "template_prompt_sha256",
            "source",
            "verification",
            "lab_token_budget",
        }:
            raise EvidenceError("lab_token_budget_record_invalid")
        value = response(
            json_bytes(record["lab_token_budget"]),
            props,
            {"input_tokens", "context_size", "template_sha256", "template_prompt_sha256"},
        )
        validate_budget(value, config)
        expected = {
            "input_tokens": value["input_tokens"],
            "output_budget": config["generation"]["max_tokens"],
            "template_prompt_sha256": value["template_prompt_sha256"],
            "source": "/lab/v1/token-budget",
            "verification": "verified",
            "lab_token_budget": value,
        }
        if not _equal(record, expected):
            raise EvidenceError("lab_token_budget_evidence_mismatch")
    return budgets
