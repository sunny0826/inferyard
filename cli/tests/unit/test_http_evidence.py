import asyncio

import httpx
import pytest

from inferyard.adapters.prism import PrismAdapter
from inferyard.evidence.ledger import reduce_events
from inferyard.evidence.storage import EvidenceError, read_json, read_jsonl
from tests.unit.test_ledger import attempt, journal, setup

__all__ = ["setup"]


@pytest.mark.parametrize("status", [400, 413, 429, 500])
def test_http_metadata_omits_error_body_and_preserves_failure(status):
    async def run():
        adapter = PrismAdapter(
            "http://127.0.0.1:8080",
            transport=httpx.MockTransport(
                lambda request: httpx.Response(status, text="SECRET-error-prompt")
            ),
        )
        adapter.capture_arrivals = True
        events = []
        result = await adapter.generate({"stream": True}, 1, lambda *e: events.append(e))
        await adapter.close()
        assert result["execution_state"] == "failed" and result["error_category"] == "http_error"
        assert events[1][0:2] == ("http_response", {"status_code": status})
        assert "SECRET" not in str(events) + str(result)

    asyncio.run(run())


@pytest.mark.parametrize("corruption", [None, "duplicate", "status", "phase"])
def test_http_event_binding(tmp_path, setup, corruption):
    store = journal(tmp_path, setup)
    attempt(store, setup, 0, "failed")
    store.close()
    events, _ = read_jsonl(store.path / "events.jsonl")
    # Failed fixture has no response payload; bind one HTTP error to its terminal.
    terminal = next(e["data"] for e in events if e["event_type"] == "request_finished")
    terminal.update(
        error_category="http_error",
        content="",
        reasoning="",
        t_first_content_ns=None,
        t_first_answer_ns=None,
    )
    events = [e for e in events if e["event_type"] not in ("content", "reasoning")]
    capture = {
        **events[0],
        "event_type": "arrival_capture",
        "monotonic_ns": 101,
        "data": {"streaming": True, "source": "decoded_delta"},
    }
    response = {
        **events[0],
        "event_type": "http_response",
        "monotonic_ns": 101,
        "data": {"status_code": 200 if corruption == "status" else 413},
    }
    if corruption == "phase":
        response["phase"] = "warmup"
    events[1:1] = [capture, response, *([response.copy()] if corruption == "duplicate" else [])]
    for i, event in enumerate(events, 1):
        event["seq"] = i
    args = (
        events,
        read_json(store.path / "run.json"),
        read_json(store.path / "selection.json"),
        {c["case_id"]: c for c in setup[1]["cases"]},
    )
    if corruption:
        with pytest.raises(EvidenceError):
            reduce_events(*args)
    else:
        assert reduce_events(*args)[0][0]["http_response"] == {"status_code": 413}
