"""Compatibility imports and fixed-output result calculation."""

from inferyard.config.fixed_output import (
    MODE as MODE,
)
from inferyard.config.fixed_output import (
    enabled as enabled,
)
from inferyard.config.fixed_output import (
    validate_inputs as validate_inputs,
)
from inferyard.evidence.fixed_output import (
    verify_execution as verify_execution,
)
from inferyard.evidence.fixed_output import (
    verify_probe as verify_probe,
)


def target_rate(rows, target, complete):
    valid = [r for r in rows if r["execution_state"] in ("completed", "failed")]
    known = all(
        type(r.get("completion_tokens")) is int
        and r["completion_tokens"] >= 0
        and r.get("token_source") == "endpoint.usage"
        and r.get("token_scope") == "completion_tokens"
        for r in valid
    )
    reached = sum(
        r["execution_state"] == "completed"
        and r.get("completion_tokens") == target
        and r.get("raw_finish_reason") == "length"
        for r in valid
    )
    return {
        "value": reached / len(valid) if complete and valid and known else None,
        "count": len(valid),
        "numerator": reached if known else None,
        "denominator": len(valid),
        "excluded": len(rows) - len(valid),
        "reason": "incomplete_or_terminal_output_count_unknown",
    }
