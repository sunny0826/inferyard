import asyncio
import json

import pytest

from inferyard.evidence.storage import EvidenceError
from inferyard.extensions.native_tool_stream import ToolStream
from inferyard.extensions.native_tools import reduce_tools, run_tool_case


def spec():
    return {
        "schema_version": 3,
        "definition": "native_tools.v1",
        "timeout_seconds": 1,
        "max_calls": 1,
        "max_rounds": 2,
        "track": "capability_diagnostic",
        "cases": [
            {
                "case_id": "add-1",
                "prompt": "7+3",
                "expected_tool": "add",
                "expected_arguments": {"a": 7, "b": 3},
                "expected_final": "10",
            }
        ],
    }


def reply(*, name="add", arguments='{"a":7,"b":3}', final=None):
    message = {"role": "assistant", "content": final}
    if final is None:
        message["tool_calls"] = [
            {"id": "call-1", "type": "function", "function": {"name": name, "arguments": arguments}}
        ]
    return {
        "choices": [
            {
                "index": 0,
                "message": message,
                "finish_reason": "stop" if final is not None else "tool_calls",
            }
        ]
    }


def run(replies, plan=None):
    plan = plan or spec()
    events = []

    async def chat(messages, tools):
        assert tools[0]["function"]["name"] == "add"
        if len(messages) > 1:
            assert messages[-1] == {"role": "tool", "tool_call_id": "call-1", "content": "10"}
        return replies.pop(0)

    return asyncio.run(run_tool_case(plan, plan["cases"][0], chat, lambda *a: events.append(a)))


def test_complete_mock_chain_has_four_independent_checks_and_no_hardware_pass():
    row = run([reply(), reply(final="10")])
    assert row["checks"]["all_pass"]
    assert row["executions"][0]["result"] == 10
    summary = reduce_tools(spec(), [row])
    assert summary["rates"]["all_pass"] == 1 and not summary["hardware_qualified"]


@pytest.mark.parametrize(
    "name,arguments",
    [
        ("exec_shell_command", "{}"),
        ("add", '{"a":true,"b":3}'),
        ("add", '{"a":7,"a":8,"b":3}'),
        ("add", '{"a":1000001,"b":3}'),
        ("add", '{"a":7,"b":3,"path":"/tmp/forbidden"}'),
        ("add", "[]"),
        ("add", "not-json"),
    ],
)
def test_malformed_or_non_allowlisted_tools_never_execute(name, arguments):
    row = run([reply(name=name, arguments=arguments)])
    assert row["state"] == "failed" and row["executions"] == []
    assert not row["checks"]["execution"] and not row["checks"]["all_pass"]


def test_correct_answer_without_tool_cannot_pass_tool_execution():
    row = run([reply(final="10")])
    assert row["checks"]["final_answer"] and not row["checks"]["selection"]
    assert not row["checks"]["all_pass"]


def test_wrong_final_answer_keeps_successful_tool_checks():
    row = run([reply(), reply(final="11")])
    assert row["checks"]["execution"] and not row["checks"]["final_answer"]


def test_tampered_execution_or_quality_does_not_replay():
    row = run([reply(), reply(final="10")])
    row["executions"][0]["result"] = 11
    with pytest.raises(EvidenceError, match="do_not_replay"):
        reduce_tools(spec(), [row])


def test_timeout_retains_executed_tool_and_failed_final_answer():
    plan = spec()
    plan["timeout_seconds"] = 0.002

    async def chat(messages, tools):
        if len(messages) == 1:
            return reply()
        await asyncio.sleep(1)

    row = asyncio.run(run_tool_case(plan, plan["cases"][0], chat, lambda *a: None))
    assert row["error"] == "tool_chain_timeout"
    assert row["checks"]["execution"] and not row["checks"]["final_answer"]


def test_streamed_argument_fragments_and_usage_reassemble_without_chunk_tokens():
    stream = ToolStream()
    for delta in [
        {
            "tool_calls": [
                {
                    "index": 0,
                    "id": "call-1",
                    "type": "function",
                    "function": {"name": "add", "arguments": '{"a":'},
                }
            ]
        },
        {"tool_calls": [{"index": 0, "function": {"arguments": '7,"b":3}'}}]},
    ]:
        stream.feed(json.dumps({"choices": [{"index": 0, "delta": delta, "finish_reason": None}]}))
    stream.feed(json.dumps({"choices": [{"index": 0, "delta": {}, "finish_reason": "tool_calls"}]}))
    stream.feed(json.dumps({"choices": [], "usage": {"prompt_tokens": 12, "completion_tokens": 9}}))
    stream.feed("[DONE]")
    assert stream.finish()["choices"][0]["message"] == reply()["choices"][0]["message"]
    assert stream.finish()["usage"]["completion_tokens"] == 9


def test_stream_without_finish_or_done_is_rejected():
    with pytest.raises(EvidenceError, match="incomplete"):
        ToolStream().finish()
    with pytest.raises(EvidenceError, match="without_finish"):
        ToolStream().feed("[DONE]")
