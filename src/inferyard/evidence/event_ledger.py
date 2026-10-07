"""Validate and reduce request events independently of fixed/duration summaries."""

from inferyard import SCHEMA_VERSION
from inferyard.contracts.validation import ContractError, validate_document
from inferyard.evidence.storage import EvidenceError


def reduce_events(events, run, selection, cases, *, duration=None):
    """Only a durable start creates an attempt; no terminal implies tool-invalid."""
    selected = selection["case_ids"]
    starts, terminals, scores, formal, partial = {}, {}, {}, {}, {}
    protocol = {}
    active, clock, stopped = None, None, None
    previous_time = 0
    for index, event in enumerate(events, 1):
        try:
            validate_document("event", event)
        except ContractError as exc:
            raise EvidenceError("invalid_event") from exc
        if event["schema_version"] != SCHEMA_VERSION or event["seq"] != index:
            raise EvidenceError("invalid_event_sequence")
        if any(event[k] != run[k] for k in ("run_id", "experiment_id", "trial_id")):
            raise EvidenceError("event_trial_identity_mismatch")
        clock = event["clock_id"] if clock is None else clock
        if event["clock_id"] != clock or event["monotonic_ns"] < previous_time:
            raise EvidenceError("invalid_event_clock")
        previous_time = event["monotonic_ns"]
        if stopped is not None:
            raise EvidenceError("event_after_run_stopped")
        key, kind, data = event["request_id"], event["event_type"], event["data"]
        if kind == "request_started":
            if key in starts or active is not None:
                raise EvidenceError("duplicate_or_overlapping_request")
            if event["phase"] not in ("probe", "warmup", "formal"):
                raise EvidenceError("invalid_request_phase")
            if event["phase"] == "formal":
                if run["kind"] == "check":
                    raise EvidenceError("check_cannot_execute_formal_cases")
                offset = len(formal)
                if (
                    offset >= (duration["max_requests"] if duration else len(selected))
                    or data["plan_index"] != offset
                    or data["case_id"] != selected[offset % len(selected)]
                ):
                    raise EvidenceError("formal_plan_identity_mismatch")
                formal[offset] = key
            elif data["case_id"] is not None or data["plan_index"] is not None:
                raise EvidenceError("nonformal_case_identity")
            starts[key], active = event, key
            partial[key] = {"content": [], "reasoning": []}
            protocol[key] = {
                "finish": None,
                "ended": False,
                "timestamps": [],
                "capture": None,
                "arrivals": [],
                "engine_timings": [],
                "finish_at": None,
                "usage": None,
                "http_response": None,
            }
        elif kind == "request_finished":
            if key != active or key not in starts or key in terminals:
                raise EvidenceError("invalid_request_terminal")
            if (
                event["phase"] != starts[key]["phase"]
                or data["t_send_ns"] < starts[key]["monotonic_ns"]
            ):
                raise EvidenceError("terminal_binding_mismatch")
            if any(
                data[channel] != "".join(partial[key][channel])
                for channel in ("content", "reasoning")
            ):
                raise EvidenceError("terminal_text_differs_from_events")
            if (
                data["protocol_complete"] != protocol[key]["ended"]
                or data["raw_finish_reason"] != protocol[key]["finish"]
                or any(
                    not data["t_send_ns"] <= t <= data["t_terminal_ns"]
                    for t in protocol[key]["timestamps"]
                )
            ):
                raise EvidenceError("terminal_protocol_differs_from_events")
            response = protocol[key]["http_response"]
            if response is not None and (
                (
                    response["status_code"] != 200
                    and (
                        data["execution_state"] != "failed"
                        or data["error_category"] != "http_error"
                    )
                )
                or (response["status_code"] == 200 and data["error_category"] == "http_error")
            ):
                raise EvidenceError("http_response_terminal_mismatch")
            if protocol[key]["capture"] is not None:
                arrivals = protocol[key]["arrivals"]
                first = arrivals[0]["monotonic_ns"] if arrivals else None
                answer = next(
                    (a["monotonic_ns"] for a in arrivals if a["channel"] == "content"), None
                )
                if first != data["t_first_content_ns"] or answer != data["t_first_answer_ns"]:
                    raise EvidenceError("arrival_first_timestamp_mismatch")
            if any(
                record["final"]
                and (
                    protocol[key]["finish_at"] is None
                    or record["monotonic_ns"] < protocol[key]["finish_at"]
                )
                for record in protocol[key]["engine_timings"]
            ):
                raise EvidenceError("engine_timings_final_before_finish")
            usage = protocol[key]["usage"] or {}
            count = usage.get("completion_tokens")
            expected_usage = (
                count,
                usage.get("source") if count is not None else None,
                usage.get("scope") if count is not None else None,
            )
            if (
                tuple(data[k] for k in ("completion_tokens", "token_source", "token_scope"))
                != expected_usage
            ):
                raise EvidenceError("terminal_usage_differs_from_events")
            terminals[key], active = data, None
        elif kind == "score":
            if (
                active is not None
                or key not in terminals
                or key in scores
                or starts[key]["phase"] != "formal"
                or event["phase"] != "scoring"
                or terminals[key]["execution_state"] != "completed"
            ):
                raise EvidenceError("invalid_score_sequence")
            case = cases[starts[key]["data"]["case_id"]]
            if (
                data["category"] != case["category"]
                or data["scorer_sha256"] != selection["scorer_sha256"]
            ):
                raise EvidenceError("score_identity_mismatch")
            scores[key] = data
        elif kind in ("duration_started", "duration_closed"):
            if duration is None or key is not None or event["phase"] != "formal":
                raise EvidenceError("unexpected_duration_event")
            if active is not None and (
                kind == "duration_started" or data.get("reason") == "duration_elapsed"
            ):
                raise EvidenceError("duration_boundary_with_active_request")
        elif kind == "run_stopped":
            if key is not None or event["phase"] != "finalizing":
                raise EvidenceError("invalid_run_stop")
            stopped = data["reason"]
            if stopped == "plan_finished" and active is not None:
                raise EvidenceError("success_with_inflight_request")
        elif kind in ("idle_observed", "native_observed"):
            if key is not None and key not in starts:
                raise EvidenceError("idle_observation_unknown_request")
        else:
            if key != active or key not in starts or event["phase"] != starts[key]["phase"]:
                raise EvidenceError("response_outside_active_request")
            if protocol[key]["ended"]:
                raise EvidenceError("response_after_protocol_end")
            protocol[key]["timestamps"].append(event["monotonic_ns"])
            if kind == "arrival_capture":
                if protocol[key]["capture"] is not None or len(protocol[key]["timestamps"]) != 1:
                    raise EvidenceError("invalid_arrival_capture_sequence")
                if data["streaming"] != starts[key]["data"]["body"]["stream"]:
                    raise EvidenceError("arrival_stream_mode_mismatch")
                protocol[key]["capture"] = data
            elif kind == "http_response":
                if (
                    protocol[key]["capture"] is None
                    or protocol[key]["http_response"] is not None
                    or len(protocol[key]["timestamps"]) != 2
                ):
                    raise EvidenceError("invalid_http_response_sequence")
                protocol[key]["http_response"] = data
            elif protocol[key]["http_response"] is not None and (
                protocol[key]["http_response"]["status_code"] != 200
            ):
                raise EvidenceError("response_payload_after_http_error")
            elif kind == "block_arrived":
                if (
                    protocol[key]["capture"] is None
                    or protocol[key]["finish"] is not None
                    or data["index"] != len(protocol[key]["arrivals"]) + 1
                ):
                    raise EvidenceError("invalid_arrival_sequence")
                protocol[key]["arrivals"].append({**data, "monotonic_ns": event["monotonic_ns"]})
            elif kind == "engine_timings":
                if protocol[key]["capture"] is None:
                    raise EvidenceError("engine_timings_without_capture")
                protocol[key]["engine_timings"].append(
                    {**data, "monotonic_ns": event["monotonic_ns"]}
                )
            elif kind == "usage":
                protocol[key]["usage"] = data
            elif kind in ("content", "reasoning"):
                partial[key][kind].append(data["text"])
            elif kind == "finish":
                if protocol[key]["finish"] is not None:
                    raise EvidenceError("duplicate_finish")
                protocol[key]["finish"] = data["raw_finish_reason"]
                protocol[key]["finish_at"] = event["monotonic_ns"]
            elif kind == "protocol_end":
                if protocol[key]["finish"] not in ("stop", "length"):
                    raise EvidenceError("protocol_end_without_finish")
                protocol[key]["ended"] = True
    rows = []
    sequence = [selected[i % len(selected)] for i in range(len(formal))] if duration else selected
    for offset, cid in enumerate(sequence):
        case = cases[cid]
        key = formal.get(offset)
        row = dict(
            case_id=cid,
            category=case["category"],
            plan_index=offset,
            request_id=key,
            clock_id=clock,
            execution_state="not_executed",
            quality_state="not_scored",
            error_category="not_started",
            content="",
            reasoning="",
            score=None,
        )
        if key is not None:
            row["http_response"] = protocol[key]["http_response"]
            row["arrival_capture"] = protocol[key]["capture"]
            row["block_arrivals"] = protocol[key]["arrivals"]
            row["engine_timings"] = protocol[key]["engine_timings"]
            if key not in terminals:
                row.update(execution_state="invalid", error_category="tool_interrupted")
                for channel in ("content", "reasoning"):
                    row[channel] = "".join(partial[key][channel])
            else:
                row.update(terminals[key], score=scores.get(key))
                row["quality_state"] = (
                    "not_applicable"
                    if case["category"] in ("performance", "svg")
                    else scores[key]["quality_state"]
                    if key in scores
                    else "fail"
                    if row["execution_state"] == "failed"
                    else "unscorable"
                    if row["execution_state"] == "completed"
                    else "not_scored"
                )
        rows.append(row)
    return rows, clock, stopped
