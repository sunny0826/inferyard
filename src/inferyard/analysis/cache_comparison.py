"""Conservative per-sequence cache policy comparability, never a speedup gate."""

from inferyard.analysis.engine_timing import DEFINITION
from inferyard.runtime.cache_execution import PROTOCOL


def qualification(data, workload):
    if workload.get("cache_protocol") != PROTOCOL:
        return ["cache_protocol_not_frozen"]
    observation = data["summary"].get("cache_observations")
    if (
        not isinstance(observation, dict)
        or observation.get("definition") != "observed_prefix_reuse_sequence.v1"
    ):
        return ["cache_observations_missing"]
    config = data["config"]
    policy = config["conditions"].get("cache_policy")
    if policy not in ("enabled", "disabled"):
        return ["cache_policy_unknown"]
    warmups = config["execution"].get("warmup_count")
    if type(warmups) is not int or not 0 <= warmups <= 3:
        return ["cache_warmup_history_unknown"]
    requests = data.get("requests", [])
    rows = observation.get("rows", [])
    phases = ["probe"] * 2 + ["warmup"] * warmups + ["formal"] * len(requests)
    if not requests or len(rows) != len(phases):
        return ["cache_history_incomplete"]
    reasons = []
    for index, (row, phase) in enumerate(zip(rows, phases, strict=True)):
        expected = policy == "enabled" and index != 0
        if (
            row.get("ordinal") != index + 1
            or row.get("phase") != phase
            or row.get("execution_state") != "completed"
            or row.get("requested_reuse") is not expected
            or row.get("missing_reason") is not None
            or row.get("source") != DEFINITION
        ):
            reasons.append("cache_history_unknown_or_inconsistent")
        counts = [row.get(k) for k in ("cached_prompt_tokens", "processed_prompt_tokens")]
        if any(type(n) is not int or not 0 <= n <= 2**63 - 1 for n in counts):
            reasons.append("cache_counts_missing")
        elif not expected and counts[0] != 0:
            reasons.append("cache_reuse_contradicts_request")
        if phase == "formal":
            request = requests[index - 2 - warmups]
            if request.get("execution_state") != "completed" or any(
                row.get(k) != request.get(k) for k in ("request_id", "case_id")
            ):
                reasons.append("cache_formal_history_mismatch")
    ids = [r.get("request_id") for r in rows]
    if any(not isinstance(key, str) or not key for key in ids) or len(set(ids)) != len(ids):
        reasons.append("cache_request_identity_ambiguous")
    return sorted(set(reasons))
