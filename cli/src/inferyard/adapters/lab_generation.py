"""Strict decoder for lab-patched OpenAI-style generation responses (T0-A, ADR030).

Pure parsing only: no HTTP, no process startup, no file writes, no dirty cleanup.
Streaming input is decoded as strict UTF-8 across chunk boundaries with CRLF/LF framing;
non-streaming input is parsed from the accumulated bytes at finish. Every generation root
object must carry the lab request/instance ID pair; unknown OpenAI extension fields are
tolerated while known fields stay strict.
"""

from __future__ import annotations

import codecs
from typing import Any

from inferyard.adapters.lab_observation_json import (
    MAX_RESPONSE_BYTES,
    MAX_SSE_FRAME_BYTES,
    LabProtocolError,
    decode_utf8,
    parse_json,
    require_id32,
    require_int,
    require_object,
    require_str_enum,
)

__all__ = ["GenerationDecoder", "LabProtocolError"]

STATE_REASON = "lab_generation_state"
TIME_REASON = "lab_generation_time"
SIZE_REASON = "lab_generation_size"
UTF8_REASON = "lab_generation_utf8"
FRAME_REASON = "lab_generation_frame"
JSON_REASON = "lab_generation_json"
ERROR_FRAME_REASON = "lab_generation_error_frame"
ID_REASON = "lab_generation_id"
CHOICES_REASON = "lab_generation_choices"
DELTA_REASON = "lab_generation_delta"
FINISH_REASON = "lab_generation_finish_reason"
DONE_REASON = "lab_generation_done"
TRUNCATED_REASON = "lab_generation_truncated"
USAGE_REASON = "lab_generation_usage"

_FINISH_REASONS = frozenset({"stop", "length"})
USAGE_COUNTERS = ("prompt_tokens", "completion_tokens", "cached_tokens", "reasoning_tokens")
NOT_REPORTED = "not_reported"


class GenerationDecoder:
    def __init__(self, request_id: str, instance_id: str, *, streaming: bool, t_send_ns: int):
        self._request_id = require_id32(request_id, ID_REASON)
        self._instance_id = require_id32(instance_id, ID_REASON)
        if type(streaming) is not bool:
            raise LabProtocolError(STATE_REASON)
        self._streaming = streaming
        self._t_send_ns = require_int(t_send_ns, TIME_REASON)
        self._last_ns = t_send_ns
        self._finished = False
        self._complete = False
        self._raw_total = 0
        self._buffer = bytearray()
        self._utf8 = codecs.getincrementaldecoder("utf-8")("strict") if streaming else None
        self._pending = ""
        self._frame_data: list[str] = []
        self._frame_bytes = 0
        self._seen_done = False
        self._finish_reason: str | None = None
        self._content_parts: list[str] = []
        self._reasoning_parts: list[str] = []
        self._usage: dict[str, int] = {}
        self._t_first_content_ns: int | None = None
        self._t_first_answer_ns: int | None = None
        self._t_terminal_ns: int | None = None

    def feed(self, raw: bytes, *, observed_ns: int) -> None:
        if self._finished:
            raise LabProtocolError(STATE_REASON)
        self._check_time(observed_ns)
        if not isinstance(raw, (bytes, bytearray)):
            raise LabProtocolError(FRAME_REASON)
        self._raw_total += len(raw)
        if self._raw_total > MAX_RESPONSE_BYTES:
            raise LabProtocolError(SIZE_REASON)
        if not self._streaming:
            self._buffer += raw
            return
        try:
            self._pending += self._utf8.decode(bytes(raw))
        except UnicodeDecodeError as exc:
            raise LabProtocolError(UTF8_REASON) from exc
        while "\n" in self._pending:
            line, self._pending = self._pending.split("\n", 1)
            self._frame_bytes += len(line.encode("utf-8")) + 1
            if self._frame_bytes > MAX_SSE_FRAME_BYTES:
                raise LabProtocolError(SIZE_REASON)
            line = line.removesuffix("\r")
            if not line:
                self._dispatch_frame(observed_ns)
            elif line.startswith(":"):
                continue
            elif line.startswith("data:"):
                value = line[5:]
                self._frame_data.append(value[1:] if value.startswith(" ") else value)
            else:
                raise LabProtocolError(FRAME_REASON)
        buffered = self._utf8.getstate()[0]
        pending_bytes = len(self._pending.encode("utf-8")) + len(buffered)
        if self._frame_bytes + pending_bytes > MAX_SSE_FRAME_BYTES:
            raise LabProtocolError(SIZE_REASON)

    def finish(self, *, observed_ns: int) -> dict:
        if self._finished:
            raise LabProtocolError(STATE_REASON)
        self._check_time(observed_ns)
        self._finished = True
        if self._streaming:
            try:
                self._pending += self._utf8.decode(b"", final=True)
            except UnicodeDecodeError as exc:
                raise LabProtocolError(UTF8_REASON) from exc
            if self._pending or self._frame_data:
                raise LabProtocolError(TRUNCATED_REASON)
            if not self._seen_done:
                raise LabProtocolError(DONE_REASON)
            if self._finish_reason is None:
                raise LabProtocolError(FINISH_REASON)
        else:
            payload = require_object(
                parse_json(decode_utf8(bytes(self._buffer), UTF8_REASON), JSON_REASON), JSON_REASON
            )
            if "error" in payload:
                raise LabProtocolError(ERROR_FRAME_REASON)
            self._check_ids(payload)
            choices = payload.get("choices")
            if type(choices) is not list or len(choices) != 1:
                raise LabProtocolError(CHOICES_REASON)
            choice = require_object(choices[0], CHOICES_REASON)
            self._check_choice_index(choice)
            message = require_object(choice.get("message"), DELTA_REASON)
            self._accept_message(message, observed_ns)
            finish = choice.get("finish_reason")
            self._finish_reason = require_str_enum(finish, _FINISH_REASONS, FINISH_REASON)
            if "usage" in payload:
                self._merge_usage(require_object(payload["usage"], USAGE_REASON))
            self._t_terminal_ns = observed_ns
        self._complete = True
        return self._result()

    def snapshot(self) -> dict:
        """Return detached partial state for durable deltas and abnormal terminals."""
        return self._result()

    def _check_time(self, observed_ns: int) -> None:
        require_int(observed_ns, TIME_REASON)
        if observed_ns < self._last_ns:
            raise LabProtocolError(TIME_REASON)
        self._last_ns = observed_ns

    def _check_ids(self, payload: dict) -> None:
        if payload.get("lab_request_id") != self._request_id:
            raise LabProtocolError(ID_REASON)
        if payload.get("lab_server_instance_id") != self._instance_id:
            raise LabProtocolError(ID_REASON)

    @staticmethod
    def _check_choice_index(choice: dict) -> None:
        if type(choice.get("index")) is not int or choice["index"] != 0:
            raise LabProtocolError(CHOICES_REASON)

    def _dispatch_frame(self, observed_ns: int) -> None:
        data_lines = self._frame_data
        self._frame_data = []
        self._frame_bytes = 0
        if not data_lines:
            return
        payload = "\n".join(data_lines)
        if self._seen_done:
            raise LabProtocolError(DONE_REASON)
        if payload == "[DONE]":
            self._seen_done = True
            self._t_terminal_ns = observed_ns
            return
        chunk = require_object(parse_json(payload, JSON_REASON), JSON_REASON)
        if "error" in chunk:
            raise LabProtocolError(ERROR_FRAME_REASON)
        self._check_ids(chunk)
        choices = chunk.get("choices")
        if type(choices) is not list or len(choices) > 1:
            raise LabProtocolError(CHOICES_REASON)
        if choices:
            self._accept_chunk_choice(require_object(choices[0], CHOICES_REASON), observed_ns)
        # OpenAI streams report null until the optional include_usage object arrives.
        if chunk.get("usage") is not None:
            self._merge_usage(require_object(chunk["usage"], USAGE_REASON))

    def _accept_chunk_choice(self, choice: dict, observed_ns: int) -> None:
        self._check_choice_index(choice)
        delta = choice.get("delta")
        if delta is not None:
            delta = require_object(delta, DELTA_REASON)
            self._accept_text(delta.get("content"), observed_ns, is_content=True)
            self._accept_text(delta.get("reasoning"), observed_ns, is_content=False)
        finish = choice.get("finish_reason")
        if finish is not None:
            require_str_enum(finish, _FINISH_REASONS, FINISH_REASON)
            if self._finish_reason is not None:
                raise LabProtocolError(FINISH_REASON)
            self._finish_reason = finish

    def _accept_text(self, value: Any, observed_ns: int, *, is_content: bool) -> None:
        if value is None:
            return
        if type(value) is not str:
            raise LabProtocolError(DELTA_REASON)
        if value and self._finish_reason is not None:
            raise LabProtocolError(FINISH_REASON)
        if is_content:
            self._content_parts.append(value)
        else:
            self._reasoning_parts.append(value)
        if value.strip():
            if self._t_first_content_ns is None:
                self._t_first_content_ns = observed_ns
            if is_content and self._t_first_answer_ns is None:
                self._t_first_answer_ns = observed_ns

    def _accept_message(self, message: dict, observed_ns: int) -> None:
        content = message.get("content")
        if content is not None and type(content) is not str:
            raise LabProtocolError(DELTA_REASON)
        reasoning = message.get("reasoning")
        if reasoning is not None and type(reasoning) is not str:
            raise LabProtocolError(DELTA_REASON)
        content = content or ""
        reasoning = reasoning or ""
        self._content_parts.append(content)
        self._reasoning_parts.append(reasoning)
        if content.strip() or reasoning.strip():
            self._t_first_content_ns = observed_ns
        if content.strip():
            self._t_first_answer_ns = observed_ns

    def _merge_usage(self, usage: dict) -> None:
        self._merge_counter(usage, "prompt_tokens")
        self._merge_counter(usage, "completion_tokens")
        self._merge_counter(usage, "total_tokens")
        if "prompt_tokens_details" in usage:
            details = require_object(usage["prompt_tokens_details"], USAGE_REASON)
            self._merge_counter(details, "cached_tokens")
        if "completion_tokens_details" in usage:
            details = require_object(usage["completion_tokens_details"], USAGE_REASON)
            self._merge_counter(details, "reasoning_tokens")
        prompt = self._usage.get("prompt_tokens")
        completion = self._usage.get("completion_tokens")
        cached = self._usage.get("cached_tokens")
        reasoning = self._usage.get("reasoning_tokens")
        total = self._usage.get("total_tokens")
        if cached is not None and prompt is not None and cached > prompt:
            raise LabProtocolError(USAGE_REASON)
        if reasoning is not None and completion is not None and reasoning > completion:
            raise LabProtocolError(USAGE_REASON)
        if (
            total is not None
            and prompt is not None
            and completion is not None
            and total != prompt + completion
        ):
            raise LabProtocolError(USAGE_REASON)

    def _merge_counter(self, source: dict, key: str) -> None:
        if key not in source:
            return
        value = require_int(source[key], USAGE_REASON)
        reported = self._usage.get(key)
        if reported is not None and reported != value:
            raise LabProtocolError(USAGE_REASON)
        self._usage[key] = value

    def _result(self) -> dict:
        return {
            "request_id": self._request_id,
            "server_instance_id": self._instance_id,
            "content": "".join(self._content_parts),
            "reasoning": "".join(self._reasoning_parts),
            "finish_reason": self._finish_reason,
            "protocol_complete": self._complete,
            "prompt_tokens": self._usage.get("prompt_tokens"),
            "completion_tokens": self._usage.get("completion_tokens"),
            "cached_tokens": self._usage.get("cached_tokens"),
            "reasoning_tokens": self._usage.get("reasoning_tokens"),
            "usage_missing_reasons": {
                key: None if key in self._usage else NOT_REPORTED for key in USAGE_COUNTERS
            },
            "t_send_ns": self._t_send_ns,
            "t_first_content_ns": self._t_first_content_ns,
            "t_first_answer_ns": self._t_first_answer_ns,
            "t_terminal_ns": self._t_terminal_ns,
        }
