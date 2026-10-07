"""Frozen fixed-output input rules."""

from inferyard.contracts.validation import ContractError

MODE = "strict_fixed_length"


def enabled(workload):
    return workload.get("output_mode") == MODE


def validate_inputs(workload, config, bundle):
    if not enabled(workload):
        return
    if config.get("engine", {}).get("adapter") in ("kvmem", "ninfer"):
        raise ContractError("output_mode", "lab adapters support natural serial text only")
    if workload["purpose"] != "performance" or workload["protocol"]["kind"] != "fixed":
        raise ContractError("output_mode", "strict output requires fixed performance workload")
    if config["generation"]["stop"]:
        raise ContractError("output_mode", "strict output requires empty explicit stop sequences")
    selected = set(workload["protocol"]["case_ids"])
    for case in bundle["cases"]:
        if case["case_id"] in selected and (
            case["category"] != "performance"
            or case["rules"].get("output_target_tokens")
            not in (None, workload["output_budget_tokens"])
        ):
            raise ContractError("output_mode", "requires performance cases with consistent target")
