from copy import deepcopy

import pytest

from inferyard.analysis.engine_blocks import compare_blocks
from inferyard.analysis.first_body import first_body_times
from inferyard.contracts.engine_tuning import llama_arguments, validate_tuning
from inferyard.errors import ContractError, EvidenceError
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


def test_consistent_direction_names_winner_with_product_scope():
    result = compare_blocks(interleaved(right={"e2e_ms": 800}))
    assert result["eligible"] is True, result["blockers"]
    assert result["verdicts"]["e2e_ms"]["verdict"] == "lmstudio"
    assert result["verdicts"]["output_tokens_per_second"]["verdict"] == "lmstudio"
    assert result["verdicts"]["first_content_ms"]["verdict"] == "no_clear_difference"
    assert result["scope"] == "product_configuration"
    assert result["definition"] == "engine-comparison.v1"


def test_inconsistent_direction_is_no_clear_difference():
    rows = interleaved(right={"e2e_ms": 800})
    rows[3]["requests"] = [request(i, e2e_ms=1200) for i in range(2)]
    result = compare_blocks(rows)
    assert result["verdicts"]["e2e_ms"]["verdict"] == "no_clear_difference"


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


def test_unequal_output_tokens_are_excluded_not_ranked():
    rows = interleaved(right={"e2e_ms": 800})
    rows[1]["requests"][0]["completion_tokens"] = 9
    result = compare_blocks(rows)
    assert result["blocks"][0]["excluded"] == {"output_tokens_differ": 1}
    assert result["blocks"][0]["paired_requests"] == 1


def test_single_family_is_rejected():
    with pytest.raises(EvidenceError, match="two_families"):
        compare_blocks([block("llama-cpp", 0), block("llama-cpp", 1)])


def test_llama_tuning_must_match_bound_startup_args():
    engine = {
        "family": "llama-cpp",
        "startup_args": ["-m", "x.gguf", "-fa", "on", "--batch-size=512"],
        "tuning": {"flash_attention": "on", "batch_size": "512", "prompt_cache": "disabled"},
    }
    assert llama_arguments(engine["startup_args"]) == {
        "flash_attention": "on",
        "batch_size": "512",
    }
    validate_tuning(engine)
    engine["tuning"]["batch_size"] = "1024"
    with pytest.raises(ContractError, match="startup_args"):
        validate_tuning(engine)


def test_lmstudio_rejects_unobservable_tuning():
    with pytest.raises(ContractError, match="not observable"):
        validate_tuning({"family": "lmstudio", "startup_args": [], "tuning": {"gpu_layers": "99"}})
    with pytest.raises(ContractError, match="not controllable"):
        validate_tuning(
            {"family": "lmstudio", "startup_args": [], "tuning": {"prompt_cache": "disabled"}}
        )


def test_prompt_cache_declaration_adds_cache_prompt_for_llama_only():
    from inferyard.adapters.local_engines.adapter import LocalEngineAdapter

    config = local("llama-cpp")["config"]
    config["engine"].update(served_model="m", tuning={"prompt_cache": "disabled"})
    config["generation"] = {"temperature": 0, "max_tokens": 8}
    assert LocalEngineAdapter.request_body(config, "hi")["cache_prompt"] is False
    config["engine"]["tuning"] = {}
    assert "cache_prompt" not in LocalEngineAdapter.request_body(config, "hi")


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
    # mlx-lm streams `<think>` as text and llama-server parses it away: different tokens.
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


def test_cross_format_keeps_unequal_lengths_for_per_token_metrics():
    rows = cross_format(e2e_ms=800, tokens=12)
    result = compare_blocks(rows)
    assert result["eligible"] is True, result["blockers"]
    assert result["blocks"][0]["paired_requests"] == 2
    assert result["blocks"][0]["output_length_unmatched"] == 2
    assert result["blocks"][0]["excluded"] == {}
    assert result["verdicts"]["client_decode_interval_ms"]["verdict"] == "mlx-lm"
    assert "decode_interval_not_matched_by_output_length" in result["limitations"]


def test_cross_format_first_body_decides_when_first_content_is_misaligned():
    # The real failure: mlx-lm reaches `<think>` first but its first body text comes later.
    result = compare_blocks(cross_format(first_ms=50, body_ms=150))
    assert result["verdicts"]["first_body_text_ms"]["verdict"] == "llama-cpp"
    assert (
        result["verdicts"]["first_content_ms"]["verdict"]
        == "display_only_first_output_token_not_aligned"
    )


def test_cross_format_missing_first_body_has_no_winner():
    rows = cross_format()
    for row in rows[1]["requests"]:
        row["t_first_body_ns"] = None
    assert compare_blocks(rows)["verdicts"]["first_body_text_ms"]["verdict"] == (
        "no_clear_difference"
    )


@pytest.mark.parametrize(
    "chunks,index",
    [
        (["<think>", "\n", "好的"], 2),
        (["<think>\n好的"], 0),
        (["<thi", "nk>", "\n", "好"], 3),
        (["好的"], 0),
        (["<think>\n", "\n"], None),
    ],
)
def test_first_body_skips_only_the_leading_think_tag(chunks, index):
    events = [
        {"event_type": "content", "request_id": "r", "monotonic_ns": i, "data": {"text": text}}
        for i, text in enumerate(chunks)
    ]
    assert first_body_times(events).get("r") == index


def test_first_body_spans_reasoning_and_ignores_other_events():
    events = [
        {"event_type": "finish", "request_id": "r", "monotonic_ns": 0, "data": {}},
        {"event_type": "reasoning", "request_id": "r", "monotonic_ns": 1, "data": {"text": "好"}},
        {"event_type": "content", "request_id": "r", "monotonic_ns": 2, "data": {"text": "京"}},
    ]
    assert first_body_times(events) == {"r": 1}


def test_missing_llama_cache_counter_is_not_structural():
    rows = cross_format()
    rows[0]["requests"][0]["usage_detail"]["cached_tokens"] = None
    assert "left.cold_cache_unverified" in compare_blocks(rows)["blockers"]


def test_temperature_override_is_a_limitation():
    rows = cross_format(e2e_ms=800)
    for row in rows:
        row["config"]["safety"] = {"temperature_stop_override_reason": "operator"}
    assert "temperature_stop_overridden" in compare_blocks(rows)["limitations"]
