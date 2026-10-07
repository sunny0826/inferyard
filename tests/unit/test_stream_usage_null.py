"""Null is a streaming placeholder, while invalid usage still fails closed."""

import pytest

from inferyard.adapters.lab_generation import GenerationDecoder
from inferyard.adapters.lab_observation_json import LabProtocolError
from inferyard.adapters.native_observation import OpenAIGenerationDecoder
from tests.native_stream import usage_stream


@pytest.mark.parametrize("lab", [False, True])
@pytest.mark.parametrize("mode", ["object", "null", "absent", "invalid"])
def test_stream_usage_placeholder_and_object(lab, mode):
    decoder = (GenerationDecoder if lab else OpenAIGenerationDecoder)(
        "2" * 32, "1" * 32, streaming=True, t_send_ns=1
    )
    if mode == "invalid":
        with pytest.raises(LabProtocolError, match="lab_generation_usage"):
            decoder.feed(usage_stream(mode, lab=lab)[0], observed_ns=2)
        return
    for now, frame in enumerate(usage_stream(mode, lab=lab), 2):
        decoder.feed(frame, observed_ns=now)
    result = decoder.finish(observed_ns=10)
    assert result["content"] == "北京" and result["finish_reason"] == "stop"
    reported = mode == "object"
    assert result["prompt_tokens"] == (7 if reported else None)
    assert result["completion_tokens"] == (2 if reported else None)
    assert result["usage_missing_reasons"] == {
        "prompt_tokens": None if reported else "not_reported",
        "completion_tokens": None if reported else "not_reported",
        "cached_tokens": "not_reported",
        "reasoning_tokens": "not_reported",
    }


def test_nonstream_null_usage_keeps_existing_rejection():
    decoder = OpenAIGenerationDecoder("2" * 32, "1" * 32, streaming=False, t_send_ns=1)
    decoder.feed(
        b'{"choices":[{"index":0,"message":{"content":"ok"},"finish_reason":"stop"}],"usage":null}',
        observed_ns=2,
    )
    with pytest.raises(LabProtocolError, match="lab_generation_usage"):
        decoder.finish(observed_ns=3)
