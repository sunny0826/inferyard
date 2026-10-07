import json

import pytest

from inferyard.adapters.lab_generation import GenerationDecoder, LabProtocolError

INSTANCE = "1" * 32
REQUEST = "2" * 32
OTHER_REQUEST = "4" * 32
T_SEND = 1_000_000_000
T0 = T_SEND + 1_000_000


def chunk(content=None, finish=None, usage=None, choices_marker="delta"):
    choice = {"index": 0, "finish_reason": finish}
    if choices_marker == "delta":
        choice["delta"] = {} if content is None else {"content": content}
    root = {
        "lab_request_id": REQUEST,
        "lab_server_instance_id": INSTANCE,
        "choices": [] if choices_marker == "usage_only" else [choice],
    }
    if usage is not None:
        root["usage"] = usage
    return root


def sse(*payloads):
    return "".join(
        f"data: {p if isinstance(p, str) else json.dumps(p, ensure_ascii=False)}\n\n"
        for p in payloads
    ).encode()


def reason_of(excinfo):
    assert isinstance(excinfo.value, LabProtocolError)
    return excinfo.value.reason


def streaming_decoder():
    return GenerationDecoder(REQUEST, INSTANCE, streaming=True, t_send_ns=T_SEND)


# --- usage rejections -------------------------------------------------------


@pytest.mark.parametrize(
    "usage",
    [
        {"prompt_tokens": True},
        {"prompt_tokens": -1},
        {"prompt_tokens": {"nested": 5}},
        {"prompt_tokens_details": [1]},
        {"prompt_tokens": 3, "prompt_tokens_details": {"cached_tokens": 4}},
        {"completion_tokens": 2, "completion_tokens_details": {"reasoning_tokens": 3}},
        {"prompt_tokens": 2, "completion_tokens": 2, "total_tokens": 5},
        {"total_tokens": "many"},
    ],
)
def test_streaming_rejects_bad_usage(usage):
    decoder = streaming_decoder()
    with pytest.raises(LabProtocolError) as excinfo:
        decoder.feed(sse(chunk(usage=usage, choices_marker="usage_only")), observed_ns=T0)
    assert reason_of(excinfo) == "lab_generation_usage"


def test_streaming_rejects_usage_count_override():
    decoder = streaming_decoder()
    decoder.feed(
        sse(chunk(usage={"prompt_tokens": 5}, choices_marker="usage_only")), observed_ns=T0
    )
    with pytest.raises(LabProtocolError) as excinfo:
        decoder.feed(
            sse(chunk(usage={"prompt_tokens": 6}, choices_marker="usage_only")), observed_ns=T0 + 1
        )
    assert reason_of(excinfo) == "lab_generation_usage"


def test_streaming_usage_counter_override_rejected_across_finish():
    decoder = streaming_decoder()
    decoder.feed(
        sse(chunk(usage={"prompt_tokens": 5}, choices_marker="usage_only")), observed_ns=T0
    )
    decoder.feed(sse(chunk(content="x", finish="stop")), observed_ns=T0 + 1)
    with pytest.raises(LabProtocolError) as excinfo:
        decoder.feed(
            sse(chunk(usage={"prompt_tokens": 9}, choices_marker="usage_only")), observed_ns=T0 + 2
        )
    assert reason_of(excinfo) == "lab_generation_usage"


# --- decoder lifecycle ------------------------------------------------------


def test_decoder_rejects_feed_after_finish_and_double_finish():
    decoder = streaming_decoder()
    decoder.feed(sse(chunk(finish="stop"), "[DONE]"), observed_ns=T0)
    decoder.finish(observed_ns=T0 + 1)
    with pytest.raises(LabProtocolError) as excinfo:
        decoder.feed(b"", observed_ns=T0 + 2)
    assert reason_of(excinfo) == "lab_generation_state"
    with pytest.raises(LabProtocolError) as excinfo:
        decoder.finish(observed_ns=T0 + 2)
    assert reason_of(excinfo) == "lab_generation_state"


@pytest.mark.parametrize(
    "kwargs",
    [
        {"request_id": "x" * 32},
        {"instance_id": "y" * 32},
        {"streaming": 1},
        {"t_send_ns": True},
        {"t_send_ns": -1},
        {"t_send_ns": 2**63},
    ],
)
def test_decoder_rejects_bad_constructor_arguments(kwargs):
    arguments = {
        "request_id": REQUEST,
        "instance_id": INSTANCE,
        "streaming": True,
        "t_send_ns": T_SEND,
    }
    arguments.update(kwargs)
    positional = {key: arguments.pop(key) for key in ("request_id", "instance_id")}
    with pytest.raises(LabProtocolError):
        GenerationDecoder(**positional, **arguments)


# --- non-streaming ------------------------------------------------------------


def non_streaming_payload(**overrides):
    payload = {
        "lab_request_id": REQUEST,
        "lab_server_instance_id": INSTANCE,
        "choices": [
            {
                "index": 0,
                "message": {"role": "assistant", "content": "final answer", "reasoning": "r"},
                "finish_reason": "stop",
            }
        ],
        "usage": {
            "prompt_tokens": 10,
            "completion_tokens": 6,
            "total_tokens": 16,
            "prompt_tokens_details": {"cached_tokens": 2},
            "completion_tokens_details": {"reasoning_tokens": 1},
        },
    }
    payload.update(overrides)
    return payload


def test_non_streaming_happy_path():
    decoder = GenerationDecoder(REQUEST, INSTANCE, streaming=False, t_send_ns=T_SEND)
    raw = json.dumps(non_streaming_payload(), ensure_ascii=False).encode()
    decoder.feed(raw[:100], observed_ns=T0)
    decoder.feed(raw[100:], observed_ns=T0 + 1)
    result = decoder.finish(observed_ns=T0 + 2)
    assert result["content"] == "final answer"
    assert result["reasoning"] == "r"
    assert result["finish_reason"] == "stop"
    assert result["protocol_complete"] is True
    assert result["cached_tokens"] == 2
    assert result["reasoning_tokens"] == 1
    assert result["t_first_content_ns"] == T0 + 2
    assert result["t_first_answer_ns"] == T0 + 2
    assert result["t_terminal_ns"] == T0 + 2
    assert result["usage_missing_reasons"]["reasoning_tokens"] is None


def test_non_streaming_without_reasoning_is_null_not_zero():
    payload = non_streaming_payload()
    payload["choices"][0]["message"] = {"content": "plain"}
    payload["usage"] = {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}
    decoder = GenerationDecoder(REQUEST, INSTANCE, streaming=False, t_send_ns=T_SEND)
    decoder.feed(json.dumps(payload).encode(), observed_ns=T0)
    result = decoder.finish(observed_ns=T0 + 1)
    assert result["reasoning"] == ""
    assert result["reasoning_tokens"] is None
    assert result["usage_missing_reasons"]["reasoning_tokens"] == "not_reported"
    assert result["cached_tokens"] is None


@pytest.mark.parametrize(
    "mutate, expected",
    [
        (lambda p: p["choices"][0].update(finish_reason=None), "lab_generation_finish_reason"),
        (lambda p: p["choices"][0].update(finish_reason="length"), None),
        (lambda p: p.update(choices=[]), "lab_generation_choices"),
        (lambda p: p["choices"].append({"index": 1}), "lab_generation_choices"),
        (lambda p: p["choices"][0].update(index=1), "lab_generation_choices"),
        (lambda p: p["choices"][0].update(message={"content": {"x": 1}}), "lab_generation_delta"),
        (lambda p: p["choices"][0].pop("message"), "lab_generation_delta"),
        (lambda p: p.update(lab_request_id=OTHER_REQUEST), "lab_generation_id"),
        (lambda p: p.update(usage={"prompt_tokens": -1}), "lab_generation_usage"),
        (lambda p: p.update(error={"code": 500}), "lab_generation_error_frame"),
    ],
)
def test_non_streaming_rejections(mutate, expected):
    payload = non_streaming_payload()
    mutate(payload)
    decoder = GenerationDecoder(REQUEST, INSTANCE, streaming=False, t_send_ns=T_SEND)
    decoder.feed(json.dumps(payload).encode(), observed_ns=T0)
    if expected is None:
        result = decoder.finish(observed_ns=T0 + 1)
        assert result["finish_reason"] == "length"
        return
    with pytest.raises(LabProtocolError) as excinfo:
        decoder.finish(observed_ns=T0 + 1)
    assert reason_of(excinfo) == expected


def test_non_streaming_rejects_invalid_utf8_and_size():
    decoder = GenerationDecoder(REQUEST, INSTANCE, streaming=False, t_send_ns=T_SEND)
    decoder.feed(b"\xff", observed_ns=T0)
    with pytest.raises(LabProtocolError) as excinfo:
        decoder.finish(observed_ns=T0 + 1)
    assert reason_of(excinfo) == "lab_generation_utf8"
    decoder = GenerationDecoder(REQUEST, INSTANCE, streaming=False, t_send_ns=T_SEND)
    with pytest.raises(LabProtocolError) as excinfo:
        decoder.feed(b"x" * (2 * 1024**2 + 1), observed_ns=T0)
    assert reason_of(excinfo) == "lab_generation_size"


# --- enum type safety and details presence (review round 1) --------------------


@pytest.mark.parametrize("bad", [[], {}, 1, True, 1.5])
def test_non_streaming_rejects_non_string_finish_reason(bad):
    payload = non_streaming_payload()
    payload["choices"][0]["finish_reason"] = bad
    decoder = GenerationDecoder(REQUEST, INSTANCE, streaming=False, t_send_ns=T_SEND)
    decoder.feed(json.dumps(payload).encode(), observed_ns=T0)
    with pytest.raises(LabProtocolError) as excinfo:
        decoder.finish(observed_ns=T0 + 1)
    assert reason_of(excinfo) == "lab_generation_finish_reason"


def test_streaming_rejects_null_usage_details():
    usage = {
        "prompt_tokens": 5,
        "prompt_tokens_details": None,
        "completion_tokens_details": None,
    }
    decoder = streaming_decoder()
    decoder.feed(sse(chunk(finish="stop")), observed_ns=T0)
    with pytest.raises(LabProtocolError) as excinfo:
        decoder.feed(sse(chunk(usage=usage, choices_marker="usage_only")), observed_ns=T0 + 1)
    assert reason_of(excinfo) == "lab_generation_usage"


@pytest.mark.parametrize("key", ["prompt_tokens_details", "completion_tokens_details"])
def test_streaming_rejects_each_null_usage_details(key):
    usage = {"prompt_tokens": 5, "completion_tokens": 2, "total_tokens": 7, key: None}
    decoder = streaming_decoder()
    with pytest.raises(LabProtocolError) as excinfo:
        decoder.feed(sse(chunk(usage=usage, choices_marker="usage_only")), observed_ns=T0)
    assert reason_of(excinfo) == "lab_generation_usage"


def test_non_streaming_rejects_null_usage_details():
    payload = non_streaming_payload()
    payload["usage"]["prompt_tokens_details"] = None
    payload["usage"]["completion_tokens_details"] = None
    decoder = GenerationDecoder(REQUEST, INSTANCE, streaming=False, t_send_ns=T_SEND)
    decoder.feed(json.dumps(payload).encode(), observed_ns=T0)
    with pytest.raises(LabProtocolError) as excinfo:
        decoder.finish(observed_ns=T0 + 1)
    assert reason_of(excinfo) == "lab_generation_usage"


def test_usage_details_empty_object_and_unknown_extension_accepted():
    usage = {
        "prompt_tokens": 5,
        "completion_tokens": 2,
        "total_tokens": 7,
        "prompt_tokens_details": {},
        "completion_tokens_details": {"vendor_ext": 1},
    }
    decoder = streaming_decoder()
    decoder.feed(sse(chunk(finish="stop")), observed_ns=T0)
    decoder.feed(sse(chunk(usage=usage, choices_marker="usage_only"), "[DONE]"), observed_ns=T0 + 1)
    result = decoder.finish(observed_ns=T0 + 2)
    assert result["prompt_tokens"] == 5
    assert result["cached_tokens"] is None
    assert result["reasoning_tokens"] is None
    assert result["usage_missing_reasons"]["cached_tokens"] == "not_reported"
    assert result["usage_missing_reasons"]["reasoning_tokens"] == "not_reported"
