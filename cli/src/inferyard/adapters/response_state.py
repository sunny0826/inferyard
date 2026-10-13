"""Shared request terminal state, independent of engine transport."""

from dataclasses import dataclass, field


@dataclass
class ResponseState:
    t_send_ns: int
    t_first_content_ns: int | None = None
    t_first_answer_ns: int | None = None
    finish_reason: str | None = None
    completion_tokens: int | None = None
    prompt_tokens: int | None = None
    content: list[str] = field(default_factory=list)
    reasoning: list[str] = field(default_factory=list)
    done: bool = False
    redacted: bool = False

    def terminal(self, now: int, error: str | None = None, state: str | None = None) -> dict:
        success = self.done and self.finish_reason in ("stop", "length") and error is None
        return {
            "execution_state": state or ("completed" if success else "failed"),
            "raw_finish_reason": self.finish_reason,
            "budget_exhausted": self.finish_reason == "length",
            "error_category": error if not success else None,
            "protocol_complete": self.done,
            "t_send_ns": self.t_send_ns,
            "t_first_content_ns": self.t_first_content_ns,
            "t_first_answer_ns": self.t_first_answer_ns,
            "t_terminal_ns": now,
            "content": "".join(self.content),
            "reasoning": "".join(self.reasoning),
            "completion_tokens": self.completion_tokens,
            "token_source": "endpoint.usage" if self.completion_tokens is not None else None,
            "token_scope": "completion_tokens" if self.completion_tokens is not None else None,
        }
