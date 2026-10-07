import json

import pytest

from inferyard.adapters.lab_generation import GenerationDecoder, LabProtocolError

INSTANCE = "1" * 32
REQUEST = "2" * 32
OTHER_INSTANCE = "3" * 32
OTHER_REQUEST = "4" * 32
T_SEND = 1_000_000_000
T0 = T_SEND + 1_000_000


def chunk(content=None, reasoning=None, finish=None, usage=None, choices_marker="delta"):
    choice = {"index": 0, "finish_reason": finish}
    if choices_marker == "delta":
        delta = {}
        if content is not None:
            delta["content"] = content
        if reasoning is not None:
            delta["reasoning"] = reasoning
        choice["delta"] = delta
    root = {
        "lab_request_id": REQUEST,
        "lab_server_instance_id": INSTANCE,
        "choices": [] if choices_marker == "usage_only" else [choice],
    }
    if usage is not None:
        root["usage"] = usage
    return root


def sse(*payloads, crlf=False):
    eol = "\r\n" if crlf else "\n"
    lines = []
    for payload in payloads:
        data = payload if isinstance(payload, str) else json.dumps(payload, ensure_ascii=False)
        lines.append(f"data: {data}{eol}{eol}")
    return "".join(lines).encode()


def finish_stream(decoder):
    return decoder.finish(observed_ns=T0 + 9_000_000)


def reason_of(excinfo):
    assert isinstance(excinfo.value, LabProtocolError)
    return excinfo.value.reason


def streaming_decoder():
    return GenerationDecoder(REQUEST, INSTANCE, streaming=True, t_send_ns=T_SEND)


# --- streaming happy paths ------------------------------------------------


def test_streaming_happy_path_exact_result():
    decoder = streaming_decoder()
    usage = {
        "prompt_tokens": 7,
        "completion_tokens": 5,
        "total_tokens": 12,
        "prompt_tokens_details": {"cached_tokens": 3},
        "completion_tokens_details": {"reasoning_tokens": 2},
        "vendor_extension": {"nested": [1, 2]},
    }
    decoder.feed(sse(chunk(reasoning=" 思考")), observed_ns=T0)
    decoder.feed(sse(chunk(content="Hello")), observed_ns=T0 + 1_000_000)
    decoder.feed(sse(chunk(content=" world")), observed_ns=T0 + 2_000_000)
    decoder.feed(sse(chunk(finish="stop")), observed_ns=T0 + 3_000_000)
    decoder.feed(sse(chunk(usage=usage, choices_marker="usage_only")), observed_ns=T0 + 4_000_000)
    decoder.feed(sse("[DONE]"), observed_ns=T0 + 5_000_000)
    result = decoder.finish(observed_ns=T0 + 6_000_000)
    assert result == {
        "request_id": REQUEST,
        "server_instance_id": INSTANCE,
        "content": "Hello world",
        "reasoning": " 思考",
        "finish_reason": "stop",
        "protocol_complete": True,
        "prompt_tokens": 7,
        "completion_tokens": 5,
        "cached_tokens": 3,
        "reasoning_tokens": 2,
        "usage_missing_reasons": {
            "prompt_tokens": None,
            "completion_tokens": None,
            "cached_tokens": None,
            "reasoning_tokens": None,
        },
        "t_send_ns": T_SEND,
        "t_first_content_ns": T0,
        "t_first_answer_ns": T0 + 1_000_000,
        "t_terminal_ns": T0 + 5_000_000,
    }


def test_streaming_crlf_and_comment_heartbeats():
    decoder = streaming_decoder()
    payload = (
        b": heartbeat\r\n\r\n"
        + sse(chunk(content="hi"), chunk(finish="length"), "[DONE]", crlf=True)
        + b": trailing\r\n\r\n"
    )
    decoder.feed(payload, observed_ns=T0)
    result = finish_stream(decoder)
    assert result["content"] == "hi"
    assert result["finish_reason"] == "length"


def test_streaming_multibyte_split_across_feeds():
    decoder = streaming_decoder()
    payload = sse(chunk(content="你好"), chunk(finish="stop"), "[DONE]")
    for index in range(0, len(payload), 5):
        decoder.feed(payload[index : index + 5], observed_ns=T0 + index)
    result = finish_stream(decoder)
    assert result["content"] == "你好"


def test_streaming_preserves_original_whitespace():
    decoder = streaming_decoder()
    decoder.feed(sse(chunk(content="  padded \n"), chunk(finish="stop"), "[DONE]"), observed_ns=T0)
    result = finish_stream(decoder)
    assert result["content"] == "  padded \n"


def test_streaming_whitespace_only_chunks_do_not_start_content():
    decoder = streaming_decoder()
    decoder.feed(sse(chunk(content="   ")), observed_ns=T0)
    decoder.feed(sse(chunk(reasoning="\n\t")), observed_ns=T0 + 1)
    decoder.feed(sse(chunk(content="answer")), observed_ns=T0 + 2)
    decoder.feed(sse(chunk(finish="stop"), "[DONE]"), observed_ns=T0 + 3)
    result = finish_stream(decoder)
    assert result["t_first_content_ns"] == T0 + 2
    assert result["t_first_answer_ns"] == T0 + 2


def test_streaming_missing_usage_reports_not_reported_not_zero():
    decoder = streaming_decoder()
    decoder.feed(sse(chunk(reasoning="thinking"), chunk(finish="stop"), "[DONE]"), observed_ns=T0)
    result = finish_stream(decoder)
    assert result["reasoning"] == "thinking"
    assert result["reasoning_tokens"] is None
    assert result["prompt_tokens"] is None
    assert result["usage_missing_reasons"] == {
        "prompt_tokens": "not_reported",
        "completion_tokens": "not_reported",
        "cached_tokens": "not_reported",
        "reasoning_tokens": "not_reported",
    }


def test_streaming_partial_usage_marks_only_missing():
    decoder = streaming_decoder()
    usage = {"prompt_tokens": 4, "prompt_tokens_details": {"cached_tokens": 1}}
    decoder.feed(
        sse(
            chunk(content="x", finish="stop"),
            chunk(usage=usage, choices_marker="usage_only"),
            "[DONE]",
        ),
        observed_ns=T0,
    )
    result = finish_stream(decoder)
    assert result["prompt_tokens"] == 4
    assert result["cached_tokens"] == 1
    assert result["completion_tokens"] is None
    assert result["usage_missing_reasons"]["completion_tokens"] == "not_reported"
    assert result["usage_missing_reasons"]["prompt_tokens"] is None


def test_streaming_usage_same_value_repeated_allowed():
    decoder = streaming_decoder()
    usage = {"prompt_tokens": 4, "completion_tokens": 2, "total_tokens": 6}
    decoder.feed(sse(chunk(content="x", finish="stop")), observed_ns=T0)
    decoder.feed(sse(chunk(usage=usage, choices_marker="usage_only")), observed_ns=T0 + 1)
    decoder.feed(sse(chunk(usage=usage, choices_marker="usage_only"), "[DONE]"), observed_ns=T0 + 2)
    result = finish_stream(decoder)
    assert result["completion_tokens"] == 2


# --- streaming rejections --------------------------------------------------


def test_streaming_rejects_missing_done():
    decoder = streaming_decoder()
    decoder.feed(sse(chunk(content="x", finish="stop")), observed_ns=T0)
    with pytest.raises(LabProtocolError) as excinfo:
        finish_stream(decoder)
    assert reason_of(excinfo) == "lab_generation_done"


def test_streaming_rejects_truncated_frame():
    decoder = streaming_decoder()
    decoder.feed(b"data: " + json.dumps(chunk(content="x")).encode(), observed_ns=T0)
    with pytest.raises(LabProtocolError) as excinfo:
        finish_stream(decoder)
    assert reason_of(excinfo) == "lab_generation_truncated"


def test_streaming_rejects_truncated_multibyte():
    decoder = streaming_decoder()
    payload = sse(chunk(content="你好"), chunk(finish="stop"), "[DONE]")
    decoder.feed(payload[:-2], observed_ns=T0)
    with pytest.raises(LabProtocolError) as excinfo:
        finish_stream(decoder)
    assert reason_of(excinfo) == "lab_generation_truncated"
    cut = sse(chunk(content="你好"), chunk(finish="stop"), "[DONE]")
    body = cut[: cut.index("好".encode()) + 1]
    decoder = streaming_decoder()
    decoder.feed(body, observed_ns=T0)
    with pytest.raises(LabProtocolError) as excinfo:
        finish_stream(decoder)
    assert reason_of(excinfo) == "lab_generation_utf8"


def test_streaming_rejects_data_after_done():
    decoder = streaming_decoder()
    decoder.feed(sse(chunk(content="x", finish="stop"), "[DONE]"), observed_ns=T0)
    with pytest.raises(LabProtocolError) as excinfo:
        decoder.feed(sse(chunk(content="y")), observed_ns=T0 + 1)
    assert reason_of(excinfo) == "lab_generation_done"


def test_streaming_rejects_duplicate_done():
    decoder = streaming_decoder()
    with pytest.raises(LabProtocolError) as excinfo:
        decoder.feed(sse(chunk(finish="stop"), "[DONE]", "[DONE]"), observed_ns=T0)
    assert reason_of(excinfo) == "lab_generation_done"


def test_streaming_rejects_text_after_finish():
    decoder = streaming_decoder()
    decoder.feed(sse(chunk(content="x", finish="stop")), observed_ns=T0)
    with pytest.raises(LabProtocolError) as excinfo:
        decoder.feed(sse(chunk(content="y")), observed_ns=T0 + 1)
    assert reason_of(excinfo) == "lab_generation_finish_reason"
    decoder = streaming_decoder()
    decoder.feed(sse(chunk(content="x", finish="stop")), observed_ns=T0)
    with pytest.raises(LabProtocolError) as excinfo:
        decoder.feed(sse(chunk(reasoning="y")), observed_ns=T0 + 1)
    assert reason_of(excinfo) == "lab_generation_finish_reason"


def test_streaming_rejects_duplicate_finish():
    decoder = streaming_decoder()
    decoder.feed(sse(chunk(finish="stop")), observed_ns=T0)
    with pytest.raises(LabProtocolError) as excinfo:
        decoder.feed(sse(chunk(finish="length")), observed_ns=T0 + 1)
    assert reason_of(excinfo) == "lab_generation_finish_reason"


def test_streaming_rejects_missing_finish_reason():
    decoder = streaming_decoder()
    decoder.feed(sse(chunk(content="x"), "[DONE]"), observed_ns=T0)
    with pytest.raises(LabProtocolError) as excinfo:
        finish_stream(decoder)
    assert reason_of(excinfo) == "lab_generation_finish_reason"


def test_streaming_rejects_error_frame():
    decoder = streaming_decoder()
    with pytest.raises(LabProtocolError) as excinfo:
        decoder.feed(sse({"error": {"message": "boom", "code": 500}}), observed_ns=T0)
    assert reason_of(excinfo) == "lab_generation_error_frame"


@pytest.mark.parametrize(
    "payload",
    [
        chunk(content="x") | {"lab_request_id": OTHER_REQUEST},
        chunk(content="x") | {"lab_server_instance_id": OTHER_INSTANCE},
        {"lab_server_instance_id": INSTANCE, "choices": []},
        {"lab_request_id": REQUEST, "choices": []},
    ],
)
def test_streaming_rejects_id_mismatch(payload):
    decoder = streaming_decoder()
    with pytest.raises(LabProtocolError) as excinfo:
        decoder.feed(sse(payload), observed_ns=T0)
    assert reason_of(excinfo) == "lab_generation_id"


@pytest.mark.parametrize(
    "mutate",
    [
        lambda c: c.update(choices=[{"index": 1, "delta": {}, "finish_reason": None}]),
        lambda c: c.update(
            choices=[{"index": 0, "delta": {}, "finish_reason": None}, {"index": 1}]
        ),
        lambda c: c.update(choices=[{"index": True, "delta": {}, "finish_reason": None}]),
        lambda c: c.update(choices="x"),
        lambda c: c["choices"][0].update(delta="text"),
        lambda c: c["choices"][0].update(delta={"content": 5}),
        lambda c: c["choices"][0].update(delta={"reasoning": ["x"]}),
        lambda c: c["choices"][0].update(finish_reason="tool_calls"),
    ],
)
def test_streaming_rejects_choice_shape_violations(mutate):
    payload = chunk(content="x")
    mutate(payload)
    decoder = streaming_decoder()
    with pytest.raises(LabProtocolError) as excinfo:
        decoder.feed(sse(payload), observed_ns=T0)
    assert reason_of(excinfo) in {
        "lab_generation_choices",
        "lab_generation_delta",
        "lab_generation_finish_reason",
    }


def test_streaming_rejects_oversize_frame():
    decoder = streaming_decoder()
    payload = sse(chunk(content="x" * (256 * 1024)))
    with pytest.raises(LabProtocolError) as excinfo:
        decoder.feed(payload, observed_ns=T0)
    assert reason_of(excinfo) == "lab_generation_size"


def test_streaming_rejects_cumulative_over_limit():
    decoder = streaming_decoder()
    frame = sse(chunk(content="x" * (200 * 1024)))
    for index in range(10):
        decoder.feed(frame, observed_ns=T0 + index)
    with pytest.raises(LabProtocolError) as excinfo:
        decoder.feed(frame, observed_ns=T0 + 10)
    assert reason_of(excinfo) == "lab_generation_size"


def test_streaming_rejects_time_regression():
    decoder = streaming_decoder()
    decoder.feed(sse(chunk(content="x")), observed_ns=T0)
    with pytest.raises(LabProtocolError) as excinfo:
        decoder.feed(sse(chunk(content="y")), observed_ns=T0 - 1)
    assert reason_of(excinfo) == "lab_generation_time"
    with pytest.raises(LabProtocolError) as excinfo:
        decoder.feed(b"", observed_ns=True)
    assert reason_of(excinfo) == "lab_generation_time"


def test_streaming_rejects_time_before_send():
    decoder = streaming_decoder()
    with pytest.raises(LabProtocolError) as excinfo:
        decoder.feed(b"", observed_ns=T_SEND - 1)
    assert reason_of(excinfo) == "lab_generation_time"


def test_streaming_rejects_unknown_sse_field_and_bad_json():
    decoder = streaming_decoder()
    with pytest.raises(LabProtocolError) as excinfo:
        decoder.feed(b"event: message\n\n", observed_ns=T0)
    assert reason_of(excinfo) == "lab_generation_frame"
    decoder = streaming_decoder()
    with pytest.raises(LabProtocolError) as excinfo:
        decoder.feed(b"data: {not json}\n\n", observed_ns=T0)
    assert reason_of(excinfo) == "lab_generation_json"
    decoder = streaming_decoder()
    with pytest.raises(LabProtocolError) as excinfo:
        decoder.feed(b'data: {"a": 1, "a": 2}\n\n', observed_ns=T0)
    assert reason_of(excinfo) == "lab_generation_json"


# --- enum type safety (review round 1) ---------------------------------------


@pytest.mark.parametrize("bad", [[], {}, 1, True, 1.5])
def test_streaming_rejects_non_string_finish_reason(bad):
    decoder = streaming_decoder()
    with pytest.raises(LabProtocolError) as excinfo:
        decoder.feed(sse(chunk(finish=bad)), observed_ns=T0)
    assert reason_of(excinfo) == "lab_generation_finish_reason"


# --- frame budget enforced at feed time (review round 1) ----------------------


def exact_frame(total, crlf=False, tail=""):
    reference = sse(chunk(content=tail), crlf=crlf)
    pad = total - len(reference)
    assert pad >= 0
    frame = sse(chunk(content="x" * pad + tail), crlf=crlf)
    assert len(frame) == total
    return frame


def test_feed_rejects_oversize_pending_frame_without_newline():
    decoder = streaming_decoder()
    with pytest.raises(LabProtocolError) as excinfo:
        decoder.feed(b"data: " + b"x" * (256 * 1024 + 1), observed_ns=T0)
    assert reason_of(excinfo) == "lab_generation_size"


def test_feed_enforces_frame_budget_across_partial_feeds():
    decoder = streaming_decoder()
    decoder.feed(b"data: " + b"x" * (100 * 1024), observed_ns=T0)
    decoder.feed(b"x" * (100 * 1024), observed_ns=T0 + 1)
    with pytest.raises(LabProtocolError) as excinfo:
        decoder.feed(b"x" * (100 * 1024), observed_ns=T0 + 2)
    assert reason_of(excinfo) == "lab_generation_size"


def test_feed_counts_completed_line_and_pending_together():
    decoder = streaming_decoder()
    decoder.feed(b"data: " + b"y" * (200 * 1024) + b"\n", observed_ns=T0)
    with pytest.raises(LabProtocolError) as excinfo:
        decoder.feed(b"data: " + b"z" * (60 * 1024), observed_ns=T0 + 1)
    assert reason_of(excinfo) == "lab_generation_size"


def test_frame_at_exact_budget_accepted():
    decoder = streaming_decoder()
    pad = 256 * 1024 - len(sse(chunk(content="")))
    decoder.feed(exact_frame(256 * 1024), observed_ns=T0)
    decoder.feed(sse(chunk(finish="stop"), "[DONE]"), observed_ns=T0 + 1)
    result = decoder.finish(observed_ns=T0 + 2)
    assert result["protocol_complete"] is True
    assert result["content"] == "x" * pad


def test_frame_one_byte_over_budget_rejected():
    decoder = streaming_decoder()
    with pytest.raises(LabProtocolError) as excinfo:
        decoder.feed(exact_frame(256 * 1024 + 1), observed_ns=T0)
    assert reason_of(excinfo) == "lab_generation_size"


def test_frame_budget_crlf_exact_boundary_accepted():
    decoder = streaming_decoder()
    decoder.feed(exact_frame(256 * 1024, crlf=True), observed_ns=T0)
    decoder.feed(sse(chunk(finish="stop"), "[DONE]", crlf=True), observed_ns=T0 + 1)
    result = decoder.finish(observed_ns=T0 + 2)
    assert result["protocol_complete"] is True


def test_frame_budget_multibyte_split_at_boundary_accepted():
    decoder = streaming_decoder()
    frame = exact_frame(256 * 1024, tail="好")
    cut = frame.index("好".encode()) + 1
    decoder.feed(frame[:cut], observed_ns=T0)
    decoder.feed(frame[cut:], observed_ns=T0 + 1)
    decoder.feed(sse(chunk(finish="stop"), "[DONE]"), observed_ns=T0 + 2)
    result = decoder.finish(observed_ns=T0 + 3)
    assert result["content"].endswith("好")
    assert result["protocol_complete"] is True


def test_multiple_legal_frames_do_not_share_budget():
    decoder = streaming_decoder()
    frame = sse(chunk(content="x" * (200 * 1024)))
    decoder.feed(frame, observed_ns=T0)
    decoder.feed(frame, observed_ns=T0 + 1)
    decoder.feed(sse(chunk(finish="stop"), "[DONE]"), observed_ns=T0 + 2)
    result = decoder.finish(observed_ns=T0 + 3)
    assert result["protocol_complete"] is True
