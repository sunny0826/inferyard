"""One serial request lifecycle, shared by single, fixed and duration trial drivers."""

import asyncio
import time

from inferyard.adapters.requests import request_body
from inferyard.adapters.response_state import ResponseState
from inferyard.analysis.scoring import ScoringContext, score_case
from inferyard.contracts.validation import validate_document
from inferyard.evidence.request_snapshots import snapshot_filename
from inferyard.evidence.storage import EvidenceError
from inferyard.platforms.identity import PreflightError
from inferyard.runtime.service_observation import clean_observed, native, observe_service


class TrialRequests:
    def __init__(
        self,
        store,
        config,
        bundle,
        adapter,
        sampler,
        sampler_task,
        lock,
        guard,
        files,
        *,
        scorer=score_case,
    ):
        self.store, self.config, self.bundle = store, config, bundle
        self.adapter, self.sampler, self.sampler_task = adapter, sampler, sampler_task
        self.adapter.capture_arrivals = True
        self.lock, self.guard, self.files, self.scorer = lock, guard, files, scorer
        from inferyard.registry import registered_score

        self.unscorable = False
        self.builtin_scorer = scorer in (score_case, registered_score)
        self.scoring = getattr(store, "scoring_context", None) or ScoringContext()
        if self.builtin_scorer:
            self.scoring.prepare([c for c in bundle["cases"] if c["case_id"] in store.selected])
        self.ordinal = 0
        self.formal_started = False
        self.cases = {case["case_id"]: case for case in bundle["cases"]}
        self.next_formal = 0
        self.cancellation_reason = "user_cancelled"

    async def idle(self, phase="probe", key=None):
        return await observe_service(
            self.adapter, self.config["execution"]["idle_wait_seconds"], self.store, phase, key
        )

    async def one(self, phase, prompt, *, index=None, stream=True, admission_deadline_ns=None):
        if self.lock.fd is None:
            raise PreflightError("host_lock_not_held")
        case = None
        if phase == "formal":
            duration = getattr(self.store, "duration_protocol", None)
            limit = duration["max_requests"] if duration else len(self.store.selected)
            if index != self.next_formal or index >= limit:
                raise EvidenceError("formal_request_out_of_order")
            case = self.cases[self.store.selected[index % len(self.store.selected)]]
            if prompt != case["prompt"]:
                raise EvidenceError("formal_prompt_differs_from_plan")
        elif phase not in ("probe", "warmup") or index is not None:
            raise EvidenceError("invalid_request_phase")
        self.guard(self.config, self.files)
        if self.sampler_task.done():
            await self.sampler_task
            raise EvidenceError("sampler_stopped_early")
        if not await self.idle(phase):
            self.lock.dirty(
                self.store.run_id,
                self.config["endpoint"],
                "pre_request_idle_unknown",
                kind=self.store.kind,
            )
            raise PreflightError("service_not_idle")
        if admission_deadline_ns is not None and time.monotonic_ns() >= admission_deadline_ns:
            return None
        self.ordinal += 1
        key = f"{self.store.run_id}-{phase}-{self.ordinal}"
        body = request_body(self.config, prompt, stream=stream)
        if getattr(self.store, "cache_protocol", None):
            from inferyard.runtime.cache_execution import request_value

            body["cache_prompt"] = request_value(self.config, phase, self.ordinal)
            body["n_cache_reuse"] = 0
        if getattr(self.store, "strict_output", False):
            body["ignore_eos"] = True
        if hasattr(self.adapter, "prepare_request"):
            body = self.adapter.prepare_request(body)
        self.store.snapshot(snapshot_filename(self.store.run_id, phase, self.ordinal), body)
        self.lock.dirty(self.store.run_id, self.config["endpoint"], key, kind=self.store.kind)
        self.sampler.set_phase(phase, key)
        if hasattr(self.sampler, "before_request"):
            self.sampler.before_request()
        started_ns = None
        declined = False

        def start_request(send_ns):
            nonlocal started_ns, declined
            if admission_deadline_ns is not None and send_ns >= admission_deadline_ns:
                declined = True
                return False
            self.store.event(
                "request_started",
                phase,
                key,
                {
                    "case_id": case["case_id"] if case else None,
                    "plan_index": index,
                    "attempt": 1,
                    "body": {
                        "model": body["model"],
                        "messages": body["messages"],
                        "stream": stream,
                        "generation": self.config["generation"],
                    },
                },
                monotonic_ns=send_ns,
            )
            started_ns = send_ns
            if phase == "formal":
                self.formal_started = True
                self.next_formal += 1
            return True

        if admission_deadline_ns is None:
            start_request(time.monotonic_ns())

        def emit(kind, data, timestamp):
            self.store.event(kind, phase, key, data, monotonic_ns=timestamp)
            if kind == "arrival_capture" and hasattr(self.sampler, "boundary"):
                self.sampler.boundary("request_start")

        self.adapter.last_state = None
        dispatch_ns = time.monotonic_ns()
        options = {"before_send": start_request} if admission_deadline_ns is not None else {}
        generation = asyncio.create_task(
            self.adapter.generate(
                body, self.config["execution"]["timeout_seconds"], emit, **options
            )
        )
        try:
            done, _ = await asyncio.wait(
                [generation, self.sampler_task], return_when=asyncio.FIRST_COMPLETED
            )
            if self.sampler_task in done:
                generation.cancel()
                await asyncio.gather(generation, return_exceptions=True)
                raise EvidenceError("sampler_failed")
            terminal = await generation
        except asyncio.CancelledError:
            generation.cancel()
            result = (await asyncio.gather(generation, return_exceptions=True))[0]
            if isinstance(result, BaseException) and not isinstance(result, asyncio.CancelledError):
                raise result from None
            if started_ns is not None:
                fallback_ns = started_ns if admission_deadline_ns is not None else dispatch_ns
                state = self.adapter.last_state or ResponseState(fallback_ns)
                error = self.cancellation_reason + (
                    "" if self.adapter.last_state else "_before_send"
                )
                terminal = state.terminal(time.monotonic_ns(), error, "cancelled")
                self.store.event("request_finished", phase, key, terminal)
                if hasattr(self.sampler, "boundary"):
                    self.sampler.boundary("request_end")
            residual_key = key if started_ns is not None else None
            self.sampler.set_phase("residual", residual_key)
            if await self.idle("residual", residual_key):
                clean_observed(
                    self.lock, self.adapter, self.store, run_id=self.store.run_id, key=residual_key
                )
            raise
        except BaseException:
            # No invented model terminal on tool/IO failure. Leave the durable attempt invalid.
            generation.cancel()
            await asyncio.gather(generation, return_exceptions=True)
            raise
        if terminal is None:
            if not declined or started_ns is not None or self.adapter.last_state is not None:
                raise EvidenceError("request_terminal_missing")
            self.sampler.set_phase("residual", None)
            if not await self.idle("residual"):
                raise PreflightError("service_stop_unconfirmed")
            clean_observed(self.lock, self.adapter, self.store, run_id=self.store.run_id, key=None)
            return None
        self.store.event(
            "request_finished", phase, key, terminal, monotonic_ns=terminal["t_terminal_ns"]
        )
        if hasattr(self.sampler, "boundary"):
            self.sampler.boundary("request_end")
        self.sampler.set_phase("residual", key)
        idle = await self.idle("residual", key)
        if native(self.adapter) and terminal["execution_state"] == "failed":
            # Native observations cannot confirm release, but must not erase the model failure.
            raise PreflightError("native_generation_failed:" + terminal["error_category"])
        if not idle:
            raise PreflightError("service_stop_unconfirmed")
        clean_observed(self.lock, self.adapter, self.store, run_id=self.store.run_id, key=key)
        if (
            phase == "formal"
            and getattr(self.store, "duration_protocol", None)
            and self.next_formal % len(self.store.selected) == 0
            and hasattr(self.sampler, "idle_cycle_rss")
        ):
            self.sampler.idle_cycle_rss()
        if hasattr(self.sampler, "boundary"):
            self.sampler.boundary("idle")
        self.sampler.set_phase("scoring", key)
        score = None
        if case is not None and terminal["execution_state"] == "completed":
            if self.adapter.last_state.redacted or self.store.redactor.clean(case) != case:
                score = self.scoring.missing(case["category"], "redacted_scoring_input")
            else:
                try:
                    if self.builtin_scorer:
                        score = self.scoring.score(
                            case["case_id"], terminal["content"], self.bundle["answer_policy"]
                        )
                    else:
                        score = self.scorer(case, terminal["content"], self.bundle["answer_policy"])
                        validate_document("score", score)
                except EvidenceError, OSError:
                    raise
                except Exception:
                    score = self.scoring.missing(case["category"], "scorer_exception")
                if (
                    score["category"] != case["category"]
                    or score["scorer_sha256"] != self.store.scorer_sha256
                ):
                    raise EvidenceError("scorer_identity_mismatch")
            self.store.event("score", "scoring", key, score)
        if (
            getattr(self.adapter, "halt_on_failed", False)
            and terminal["execution_state"] == "failed"
        ):
            raise PreflightError("lab_generation_failed")
        if score and score["quality_state"] == "unscorable":
            self.unscorable = True
        return terminal
