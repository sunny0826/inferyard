"""External, synchronous observer/writer profiling; never upgrades ABBA eligibility.

Measure complete method calls, including nested serialization, redaction,
validation, flush and fsync. Nested wall intervals are unioned, not summed.
The caller writes this sidecar after the trial, so raw journals remain sealed.
"""

import functools
import time
from collections import Counter

from observer_read_costs import union_ns

from inferyard.evidence.journal import TrialJournal
from inferyard.evidence.storage import EvidenceError, EvidenceStore
from inferyard.platforms import identity
from inferyard.platforms.resources_linux import ResourceSampler
from inferyard.runtime import environment_schedule
from inferyard.runtime.boundary_observer import BoundaryGuard
from inferyard.runtime.environment_observer import EnvironmentObserver
from inferyard.runtime.safety_trace import SafetyTrace


class ObserverCostProfile:
    def __init__(self):
        self.records, self.originals = [], []
        self.depth = 0

    def wrap(self, owner, name, category):
        original = getattr(owner, name)
        owned = name in vars(owner)

        @functools.wraps(original)
        def measured(*args, **kwargs):
            started, cpu = time.monotonic_ns(), time.thread_time_ns()
            level = self.depth
            self.depth += 1
            outcome = "returned"
            try:
                return original(*args, **kwargs)
            except BaseException:
                outcome = "raised"
                raise
            finally:
                finished, cpu_end = time.monotonic_ns(), time.thread_time_ns()
                self.depth -= 1
                self.records.append(
                    {
                        "component": category,
                        "method": name,
                        "started_ns": started,
                        "finished_ns": finished,
                        "thread_cpu_ns": cpu_end - cpu,
                        "nesting_depth": level,
                        "outcome": outcome,
                    }
                )

        self.originals.append((owner, name, original, owned))
        setattr(owner, name, measured)

    def __enter__(self):
        for owner, methods, category in (
            (SafetyTrace, ("check",), "safety_guard_and_log"),
            (BoundaryGuard, ("__init__", "observe"), "boundary_environment_and_log"),
            (ResourceSampler, ("__init__", "collect", "boundary", "idle_cycle_rss"), "resources"),
            (EnvironmentObserver, ("external",), "external_cpu_and_log"),
            (environment_schedule, ("environment_snapshot",), "periodic_environment"),
            (identity, ("environment_snapshot",), "lifecycle_environment"),
            (TrialJournal, ("__init__", "event", "sample", "observation"), "journal"),
            (
                EvidenceStore,
                (
                    "event",
                    "sample",
                    "request_view",
                    "snapshot",
                    "text_snapshot",
                    "_append",
                    "observation",
                    "flush_due",
                    "flush",
                    "seal",
                    "close",
                ),
                "evidence_serialization_validation_and_persistence",
            ),
        ):
            for name in methods:
                self.wrap(owner, name, category)
        return self

    def __exit__(self, *_args):
        for owner, name, original, owned in reversed(self.originals):
            if owned:
                setattr(owner, name, original)
            else:
                delattr(owner, name)
        self.originals.clear()

    def summarize(self, requests):
        if not self.records or self.depth:
            raise EvidenceError("incomplete_observer_cost_profile")
        pairs = [(r["started_ns"], r["finished_ns"]) for r in self.records]
        wall = union_ns(pairs)
        outer = [r for r in self.records if r["nesting_depth"] == 0]
        if union_ns([(r["started_ns"], r["finished_ns"]) for r in outer]) != wall:
            raise EvidenceError("observer_cost_nesting_mismatch")
        values = []
        for row in requests:
            start, end = row["t_send_ns"], row["t_terminal_ns"]
            if type(start) is not int or type(end) is not int or end <= start:
                raise EvidenceError("observer_cost_requires_complete_request_windows")
            clipped = [(max(a, start), min(b, end)) for a, b in pairs if a < end and b > start]
            cost = union_ns(clipped)
            values.append(
                {
                    "request_id": row["request_id"],
                    "case_id": row["case_id"],
                    "request_duration_ns": end - start,
                    "profiled_wall_ns": cost,
                    "profiled_wall_fraction": cost / (end - start),
                }
            )
        return {
            "definition": "observer_writer_direct_method_cost.v1",
            "method_calls": len(self.records),
            "calls_by_component": dict(Counter(r["component"] for r in self.records)),
            "all_phases_union_wall_ns": wall,
            "all_phases_outer_thread_cpu_ns": sum(r["thread_cpu_ns"] for r in outer),
            "formal_requests": values,
            "max_formal_profiled_wall_fraction": max(r["profiled_wall_fraction"] for r in values),
            "direct_cost_accounted": True,
            "causal_total_perturbation_qualified": False,
            "limitations": [
                "profiled_execution_includes_reads_serialization_redaction_validation_flush_fsync",
                "nested_calls_counted_once_wall_union_and_outer_thread_cpu",
                "profiling_hooks_add_cost_no_unprofiled_control_arm",
                "outside_request_spacing_cache_and_kernel_deferred_writeback_effects_not_isolated",
                "network_scoring_and_offline_report_generation_are_not_observer_cost",
                "no_latency_or_resource_comparison_authorization_from_this_profile",
            ],
        }
