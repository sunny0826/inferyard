"""Tool-call SSE assembly is independent of the serial answer-only parser."""

from inferyard.contracts.validation import strict_json_loads
from inferyard.evidence.storage import EvidenceError
from inferyard.extensions.native_tools import parse_reply


class ToolStream:
    def __init__(self):
        self.calls = {}
        self.content = []
        self.reason = None
        self.usage = None
        self.done = False
        self.bytes = 0

    def feed(self, data):
        if self.done:
            raise EvidenceError("tool_stream_after_done")
        self.bytes += len(data.encode())
        if self.bytes > 1024 * 1024:
            raise EvidenceError("tool_stream_budget_exceeded")
        if data == "[DONE]":
            if self.reason is None:
                raise EvidenceError("tool_stream_done_without_finish")
            self.done = True
            return
        event = strict_json_loads(data)
        if not isinstance(event, dict):
            raise EvidenceError("tool_stream_event_invalid")
        if event.get("usage") is not None:
            if self.usage is not None:
                raise EvidenceError("tool_stream_duplicate_usage")
            self.usage = event["usage"]
        choices = event.get("choices")
        if not isinstance(choices, list) or len(choices) > 1:
            raise EvidenceError("tool_stream_choices_invalid")
        if not choices:
            return
        choice = choices[0]
        if type(choice.get("index")) is not int or choice["index"] != 0:
            raise EvidenceError("tool_stream_choice_invalid")
        delta = choice.get("delta")
        if not isinstance(delta, dict):
            raise EvidenceError("tool_stream_delta_invalid")
        if self.reason is not None and any(delta.get(k) for k in ("content", "tool_calls")):
            raise EvidenceError("tool_stream_content_after_finish")
        content = delta.get("content")
        if content is not None:
            if not isinstance(content, str):
                raise EvidenceError("tool_stream_content_invalid")
            self.content.append(content)
        chunks = delta.get("tool_calls", [])
        if not isinstance(chunks, list):
            raise EvidenceError("tool_stream_calls_invalid")
        for chunk in chunks:
            index = chunk.get("index")
            if type(index) is not int or not 0 <= index < 8:
                raise EvidenceError("tool_stream_index_invalid")
            row = self.calls.setdefault(
                index, {"id": "", "type": "function", "function": {"name": "", "arguments": ""}}
            )
            if chunk.get("type", "function") != "function":
                raise EvidenceError("tool_stream_call_type_invalid")
            function = chunk.get("function", {})
            if not isinstance(function, dict):
                raise EvidenceError("tool_stream_function_invalid")
            for target, key, value in (
                (row, "id", chunk.get("id")),
                (row["function"], "name", function.get("name")),
                (row["function"], "arguments", function.get("arguments")),
            ):
                if value is not None:
                    if not isinstance(value, str):
                        raise EvidenceError("tool_stream_fragment_invalid")
                    target[key] += value
        if choice.get("finish_reason") is not None:
            if self.reason is not None or choice["finish_reason"] not in ("stop", "tool_calls"):
                raise EvidenceError("tool_stream_finish_invalid")
            self.reason = choice["finish_reason"]

    def finish(self):
        if not self.done or sorted(self.calls) != list(range(len(self.calls))):
            raise EvidenceError("tool_stream_incomplete")
        message = {"role": "assistant", "content": "".join(self.content) if self.content else None}
        if self.calls:
            message["tool_calls"] = [self.calls[i] for i in sorted(self.calls)]
        result = {"choices": [{"index": 0, "finish_reason": self.reason, "message": message}]}
        if self.usage is not None:
            result["usage"] = self.usage
        parse_reply(result)
        return result
