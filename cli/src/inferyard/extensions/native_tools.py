"""Bounded native tool chains using only deterministic, in-process mock functions."""

import asyncio

from inferyard.contracts.schemas_extensions import NATIVE_ARGS as ARGS
from inferyard.contracts.schemas_extensions import NATIVE_LOOKUP as LOOKUP
from inferyard.contracts.schemas_extensions import NATIVE_TOOLS as SPEC
from inferyard.contracts.validation import ContractError, _validate, strict_json_loads
from inferyard.evidence.storage import EvidenceError, json_bytes

CATALOGUE = {"alpha": "北京", "beta": "上海"}
TOOLS = [
    {
        "type": "function",
        "function": {"name": "add", "description": "Add two bounded integers", "parameters": ARGS},
    },
    {
        "type": "function",
        "function": {
            "name": "lookup",
            "description": "Read a fixed fixture catalogue",
            "parameters": LOOKUP,
        },
    },
]


def validate_spec(spec):
    _validate(spec, SPEC, "native_tools")
    if spec["max_calls"] > 8 or spec["max_rounds"] > 9 or len(spec["cases"]) > 128:
        raise ContractError("native_tools", "bounded chain budget exceeded")
    ids = [c["case_id"] for c in spec["cases"]]
    if len(set(ids)) != len(ids):
        raise ContractError("native_tools.cases", "duplicate case")
    for case in spec["cases"]:
        _validate(
            case["expected_arguments"],
            ARGS if case["expected_tool"] == "add" else LOOKUP,
            "native_tools.arguments",
        )
    if spec["track"] == "reviewed_formal":
        import hashlib

        expected = hashlib.sha256(json_bytes(spec["cases"])).hexdigest()
        if spec.get("content_review_sha256") != expected:
            raise EvidenceError("tool_formal_content_review_binding_missing")


def call_mock(name, arguments):
    if name not in ("add", "lookup"):
        raise EvidenceError("tool_not_allowlisted")
    _validate(arguments, ARGS if name == "add" else LOOKUP, "tool.arguments")
    return arguments["a"] + arguments["b"] if name == "add" else CATALOGUE[arguments["key"]]


def parse_reply(reply):
    if (
        not isinstance(reply, dict)
        or not isinstance(reply.get("choices"), list)
        or len(reply["choices"]) != 1
    ):
        raise EvidenceError("tool_invalid_choices")
    choice = reply["choices"][0]
    if choice.get("index") != 0 or type(choice.get("index")) is not int:
        raise EvidenceError("tool_invalid_choice_index")
    message = choice.get("message")
    if not isinstance(message, dict) or message.get("role") != "assistant":
        raise EvidenceError("tool_assistant_message_missing")
    content, calls = message.get("content"), message.get("tool_calls", [])
    if content is not None and not isinstance(content, str):
        raise EvidenceError("tool_content_invalid")
    if not isinstance(calls, list):
        raise EvidenceError("tool_calls_invalid")
    for call in calls:
        if not isinstance(call, dict) or not isinstance(call.get("function"), dict):
            raise EvidenceError("tool_call_shape_invalid")
    reason = choice.get("finish_reason")
    if reason == "tool_calls" and calls:
        pass
    elif reason == "stop" and not calls and isinstance(content, str):
        pass
    else:
        raise EvidenceError("tool_finish_semantics_invalid")
    usage = reply.get("usage")
    if usage is not None:
        if not isinstance(usage, dict) or any(
            type(usage.get(k)) is not int or usage[k] < 0
            for k in ("prompt_tokens", "completion_tokens")
        ):
            raise EvidenceError("tool_usage_invalid")
    return message, reason


async def run_tool_case(spec, case, chat, emit):
    validate_spec(spec)
    messages = [{"role": "user", "content": case["prompt"]}]
    replies, executions, ids = [], [], set()
    state, error = "failed", None
    try:
        async with asyncio.timeout(spec["timeout_seconds"]):
            for _ in range(spec["max_rounds"]):
                reply = await chat(messages, TOOLS)
                emit("tool_response_received", {"case_id": case["case_id"], "reply": reply})
                message, reason = parse_reply(reply)
                replies.append(reply)
                emit("tool_reply", {"case_id": case["case_id"], "reply": reply})
                messages.append(message)
                if reason == "stop":
                    state = "completed"
                    break
                for call in message["tool_calls"]:
                    if len(ids) >= spec["max_calls"]:
                        raise EvidenceError("tool_call_budget_exceeded")
                    if not isinstance(call, dict) or call.get("type") != "function":
                        raise EvidenceError("tool_call_type_invalid")
                    key, function = call.get("id"), call.get("function")
                    if (
                        not isinstance(key, str)
                        or not key
                        or key in ids
                        or not isinstance(function, dict)
                    ):
                        raise EvidenceError("tool_call_identity_ambiguous")
                    ids.add(key)
                    arguments = function.get("arguments")
                    if not isinstance(arguments, str) or len(arguments.encode()) > 4096:
                        raise EvidenceError("tool_arguments_unbounded")
                    try:
                        parsed = strict_json_loads(arguments)
                    except (ValueError, RecursionError) as exc:
                        raise EvidenceError("tool_arguments_invalid_json") from exc
                    value = call_mock(function.get("name"), parsed)
                    record = {
                        "tool_call_id": key,
                        "name": function["name"],
                        "arguments": parsed,
                        "result": value,
                    }
                    executions.append(record)
                    emit("tool_execution", {"case_id": case["case_id"], **record})
                    messages.append(
                        {
                            "role": "tool",
                            "tool_call_id": key,
                            "content": json_bytes(value).decode().strip(),
                        }
                    )
            else:
                raise EvidenceError("tool_round_budget_exceeded")
    except TimeoutError:
        error = "tool_chain_timeout"
    except asyncio.CancelledError:
        state, error = "cancelled", "tool_chain_cancelled"
    except (EvidenceError, ContractError) as exc:
        error = str(exc)
    except Exception:
        error = "tool_adapter_failure"
    result = {
        "case_id": case["case_id"],
        "state": state,
        "error": error,
        "replies": replies,
        "executions": executions,
    }
    result["checks"] = score_tool_case(case, result)
    emit("tool_terminal", result)
    return result


def score_tool_case(case, row):
    expected = case["expected_arguments"]
    calls = [c for reply in row["replies"] for c in parse_reply(reply)[0].get("tool_calls", [])]
    names = [c.get("function", {}).get("name") for c in calls]
    selection = names == [case["expected_tool"]]
    arguments, execution = False, False
    if selection:
        try:
            parsed = strict_json_loads(calls[0]["function"]["arguments"])
            call_mock(case["expected_tool"], parsed)
            arguments = json_bytes(parsed) == json_bytes(expected)
            expected_result = call_mock(case["expected_tool"], expected)
            execution = row["executions"] == [
                {
                    "tool_call_id": calls[0]["id"],
                    "name": case["expected_tool"],
                    "arguments": expected,
                    "result": expected_result,
                }
            ]
        except KeyError, ValueError, TypeError, ContractError, EvidenceError:
            pass
    last = parse_reply(row["replies"][-1])[0] if row["replies"] else {}
    final = row["state"] == "completed" and last.get("content") == case["expected_final"]
    return {
        "selection": selection,
        "arguments": arguments,
        "execution": execution,
        "final_answer": final,
        "all_pass": selection and arguments and execution and final,
    }


def reduce_tools(spec, rows, *, evidence_kind="fixture"):
    validate_spec(spec)
    if evidence_kind not in ("fixture", "live") or [r.get("case_id") for r in rows] != [
        c["case_id"] for c in spec["cases"]
    ]:
        raise EvidenceError("tool_denominator_or_kind_invalid")
    for case, row in zip(spec["cases"], rows, strict=True):
        if row.get("state") not in ("completed", "failed", "cancelled", "not_executed"):
            raise EvidenceError("tool_terminal_invalid")
        if row.get("checks") != score_tool_case(case, row):
            raise EvidenceError("tool_checks_do_not_replay")
    rates = {
        k: sum(row["checks"][k] for row in rows) / len(rows)
        for k in ("selection", "arguments", "execution", "final_answer", "all_pass")
    }
    return {
        "definition": spec["definition"],
        "evidence_kind": evidence_kind,
        "planned": len(rows),
        "completed": sum(r["state"] == "completed" for r in rows),
        "rates": rates,
        "hardware_qualified": evidence_kind == "live"
        and all(r["state"] == "completed" for r in rows)
        and any(r["checks"]["all_pass"] for r in rows),
        "track": spec["track"],
        "limitations": ["deterministic_mock_functions_only", "not_real_world_agent_tasks"],
    }
