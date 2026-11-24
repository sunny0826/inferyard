
import pytest

from inferyard.analysis.cache_observations import observe
from inferyard.contracts.engine_timing import capture_timings


def record(cached=0, processed=0):
    return capture_timings(
        dict(cache_n=cached, prompt_n=processed, predicted_n=2, prompt_ms=0, predicted_ms=1),
        final=True,
    )


@pytest.mark.parametrize(
    "change",
    ["missing", "ambiguous", "build", "state", "definition", "speculative", "invalid", "bool"],
)
def test_unknown_never_becomes_zero_or_verified(change):
    records = [record(3, 4)]
    build = True
    state = "completed"
    if change == "missing":
        records = []
    elif change == "ambiguous":
        records *= 2
    elif change == "build":
        build = False
    elif change == "state":
        state = "failed"
    elif change == "definition":
        records[0]["definition"] = "other"
    elif change == "speculative":
        records[0]["speculative"] = True
    elif change == "invalid":
        records[0]["missing_reason"] = "invalid_engine_timings"
    else:
        records[0]["cache_n"] = True
    result = observe(records, state, build, True)
    assert result["missing_reason"]
    assert result["cached_prompt_tokens"] is None
    assert result["policy_consistent"] is None
