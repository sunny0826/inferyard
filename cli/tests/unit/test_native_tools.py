import asyncio
import json

import pytest

from inferyard.errors import EvidenceError
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
