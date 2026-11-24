from copy import deepcopy

import pytest

from inferyard.analysis.engine_blocks import compare_blocks
from tests.unit.test_engine_comparison import local


def request(index, *, prompt=100, cached=0, tokens=10, e2e_ms=1000, first_ms=100, body_ms=None):
    return {
        "plan_index": index,
        "request_id": f"r-{index}",
        "case_id": f"case-{index}",
        "execution_state": "completed",
        "completion_tokens": tokens,
        "t_send_ns": 0,
        "t_first_content_ns": first_ms * 1_000_000,
        "t_first_body_ns": (first_ms if body_ms is None else body_ms) * 1_000_000,
        "t_terminal_ns": e2e_ms * 1_000_000,
        "usage_detail": {"prompt_tokens": prompt, "cached_tokens": cached},
    }


def block(family, order, *, e2e_ms=1000, **fields):
    data = local(family)
    data["first_event_utc"] = f"2026-11-06T00:00:{order:02d}Z"
    data["run"] = {**data["run"], "run_id": f"{family}-{order}"}
    data["identity"]["local_engine"] = {
        "binding": {"service": {"pid": 100 + order, "start_ticks": order}}
    }
    data["requests"] = [request(i, e2e_ms=e2e_ms, **fields) for i in range(2)]
    return data


def interleaved(left=None, right=None, blocks=3):
    rows = []
    for i in range(blocks):
        rows.append(block("llama-cpp", 2 * i, **(left or {})))
        rows.append(block("lmstudio", 2 * i + 1, **(right or {})))
    return rows


@pytest.mark.parametrize(
    "mutate,blocker",
    [
        (lambda rows: rows[1]["requests"][0].update(usage_detail=None), "prompt_tokens_unobserved"),
        (
            lambda rows: rows[1]["requests"][0]["usage_detail"].update(prompt_tokens=101),
            "prompt_tokens_differ",
        ),
        (
            lambda rows: rows[1]["requests"][0]["usage_detail"].update(cached_tokens=None),
            "right.cold_cache_unverified",
        ),
        (
            lambda rows: rows[0]["requests"][0]["usage_detail"].update(cached_tokens=5),
            "left.cache_hit_observed",
        ),
        (lambda rows: rows.pop(), "insufficient_blocks"),
        (
            lambda rows: rows[2].update(first_event_utc="2026-11-06T00:00:00.5Z"),
            "blocks_not_interleaved",
        ),
        (
            lambda rows: rows[2]["identity"]["local_engine"]["binding"].update(
                service=deepcopy(rows[0]["identity"]["local_engine"]["binding"]["service"])
            ),
            "block_service_not_replaced",
        ),
        (
            lambda rows: rows[2]["config"]["model"].update(content_sha256="other"),
            "side_configuration_changed",
        ),
    ],
)
def test_gates_block_verdict(mutate, blocker):
    rows = interleaved(right={"e2e_ms": 800})
    mutate(rows)
    result = compare_blocks(rows)
    assert blocker in result["blockers"]
    assert result["eligible"] is False
    assert result["verdicts"]["e2e_ms"]["verdict"] == "not_eligible"


def mlx_block(order, **fields):
    data = block("mlx-lm", order, **fields)
    data["config"]["model"].update(kind="directory", content_sha256="m", assets_sha256="ma")
    for row in data["requests"]:
        row["usage_detail"]["cached_tokens"] = None
    return data


def cross_format(**right):
    rows = []
    for i in range(3):
        rows.append(block("llama-cpp", 2 * i))
        rows.append(mlx_block(2 * i + 1, **right))
    return rows


def test_cross_format_scope_and_structural_mlx_cold_cache():
    result = compare_blocks(cross_format(e2e_ms=800))
    assert result["eligible"] is True, result["blockers"]
    assert result["scope"] == "engine_and_model_format"
    assert result["verdicts"]["client_decode_interval_ms"]["verdict"] == "mlx-lm"
    for metric in ("e2e_ms", "output_tokens_per_second"):
        assert result["verdicts"][metric]["verdict"] == "display_only_output_length_unmatched"
    assert (
        result["verdicts"]["first_content_ms"]["verdict"]
        == "display_only_first_output_token_not_aligned"
    )
    assert "first_content_token_not_aligned_across_formats" in result["limitations"]
    assert result["cache_evidence"] == {
        "left": ["observed_zero_cached_tokens"],
        "right": ["formal_server_no_prompt_cache_reuse"],
    }
    assert "model_format_differs_not_attributable_to_engine" in result["limitations"]
