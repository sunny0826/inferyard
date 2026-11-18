import asyncio
import json
from pathlib import Path

import httpx
import pytest

from inferyard.adapters.prism import PrismAdapter, ResponseState, SSEDecoder, StreamProtocol
from inferyard.errors import PreflightError
from inferyard.contracts.redaction import Redactor


def frame(delta=None, reason=None, usage=None):
    value = {"choices": [{"index": 0, "delta": delta or {}, "finish_reason": reason}]}
    if usage is not None:
        value["usage"] = usage
    return json.dumps(value, ensure_ascii=False)


@pytest.mark.parametrize(
    "delta,reason", [({"content": ""}, "stop"), ({"reasoning_content": "仅思考"}, "length")]
)
def test_complete_without_final_answer_is_not_quality_success(delta, reason, wire_fixture):
    from inferyard.analysis.scoring import score_case

    async def run():
        payload = ("data: " + frame(delta, reason) + "\n\ndata: [DONE]\n\n").encode()
        adapter = PrismAdapter(
            "http://127.0.0.1:8080",
            transport=httpx.MockTransport(lambda request: httpx.Response(200, content=payload)),
        )
        try:
            return await adapter.generate({"stream": True}, 1)
        finally:
            await adapter.close()

    result = asyncio.run(run())
    bundle = wire_fixture("bundle")
    assert result["execution_state"] == "completed"
    assert result["content"] == ""
    assert (
        score_case(bundle["cases"][0], result["content"], bundle["answer_policy"])["quality_state"]
        == "fail"
    )
