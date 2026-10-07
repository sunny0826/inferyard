"""Strict v2 observer record validation; no live backend or model requests."""

from __future__ import annotations

import math
import re

MAX_BYTES = 512 * 1024 * 1024
MAX_LINE = 1024 * 1024
ENVELOPE = {"schema_version", "definition", "kind", "session_id"}
SHA = re.compile(r"[0-9a-f]{64}")
DELTA_SOURCE = "counter_delta_over_monotonic_interval"


class ObserverError(ValueError):
    """Fixed diagnostic category without raw input or credentials."""


def require(condition: bool, reason: str = "observer_log_invalid") -> None:
    if not condition:
        raise ObserverError(reason)


def fields(value: object, expected: set[str]) -> dict:
    require(type(value) is dict and set(value) == expected)
    return value


def integer(value: object, minimum: int = 0, maximum: int = 2**63 - 1) -> int:
    require(type(value) is int and minimum <= value <= maximum)
    return value


def text(value: object, limit: int = MAX_LINE) -> str:
    require(type(value) is str and 0 < len(value) <= limit)
    return value


def metric(item: object, kind: str) -> object:
    item = fields(item, {"value", "reason", "source"})
    text(item["source"], 256)
    value = item["value"]
    if value is None:
        text(item["reason"], 256)
        return None
    require(item["reason"] is None)
    if kind == "int":
        integer(value, maximum=2**64 - 1)
    elif kind == "float":
        require(type(value) in (int, float) and math.isfinite(value) and value >= 0)
    elif kind == "sha":
        require(type(value) is str and SHA.fullmatch(value) is not None)
    elif kind == "list":
        require(type(value) is list and len(value) <= 16)
        for label in value:
            text(label, 4096)
    else:
        text(value, 4096)
    return value


def target(item: object) -> tuple[int, int, str]:
    item = fields(item, {"pid", "process_start_ticks", "name"})
    integer(item["pid"], 1, 2_147_483_647)
    integer(item["process_start_ticks"], 1, 2**64 - 1)
    require(type(item["name"]) is str and len(item["name"]) <= 1024)
    return item["pid"], item["process_start_ticks"], item["name"]


def inventory(system: object) -> None:
    system = fields(
        system,
        {
            "os",
            "arch",
            "os_version",
            "cpu_model",
            "logical_cpus",
            "memory_total_bytes",
            "graphics_devices",
            "limitations",
        },
    )
    require(system["os"] in ("darwin", "linux", "windows"))
    require(system["arch"] in ("arm64", "amd64"))
    integer(system["logical_cpus"], 1, 1_000_000)
    metric(system["os_version"], "str")
    metric(system["cpu_model"], "str")
    metric(system["memory_total_bytes"], "int")
    metric(system["graphics_devices"], "list")
    require(type(system["limitations"]) is list and len(system["limitations"]) <= 32)
    for value in system["limitations"]:
        text(value, 256)


def header(item: dict) -> None:
    fields(
        item,
        ENVELOPE
        | {
            "tool_version",
            "tool_source_sha256",
            "binary_sha256",
            "started_utc",
            "system",
            "interval_ns",
            "duration_ns",
            "targets",
            "observer_target",
            "benchmark_binding",
            "clock",
        }
        | {"disk_scope"},
    )
    require(item["kind"] == "observer_header")
    require(item["clock"] == "session_monotonic_ns_not_benchmark_clock")
    text(item["tool_version"], 64)
    text(item["started_utc"], 64)
    metric(item["tool_source_sha256"], "sha")
    metric(item["binary_sha256"], "sha")
    inventory(item["system"])
    integer(item["interval_ns"], 100_000_000, 60_000_000_000)
    integer(item["duration_ns"], 1_000_000_000, 86_400_000_000_000)
    require(type(item["targets"]) is list and len(item["targets"]) == 1)
    target(item["targets"][0])
    target(item["observer_target"])
    binding = item["benchmark_binding"]
    require(
        item["disk_scope"]
        == ("benchmark_run_filesystem" if binding is not None else "observer_cwd_filesystem")
    )
    if binding is not None:
        fields(
            binding,
            {
                "run_id",
                "run_file_sha256",
                "config_file_sha256",
                "model_label_declared",
                "backend_declared",
            },
        )
        text(binding["run_id"])
        for key in ("run_file_sha256", "config_file_sha256"):
            require(type(binding[key]) is str and SHA.fullmatch(binding[key]) is not None)
        for key in ("model_label_declared", "backend_declared"):
            require(type(binding[key]) is str)


class Series:
    def __init__(self, fixed: dict):
        self.fixed = target(fixed)
        self.previous: tuple[int, int] | None = None
        self.sources: dict[str, str] = {}
        self.cpu_ns = 0
        self.covered_ns = 0
        self.rss_max: int | None = None
        self.rss_missing = 0
        self.state = "running"
        self.stop_reason = None

    def read(self, item: dict, lower: int, upper: int, *, allow_incomplete: bool = False) -> None:
        fields(
            item,
            {
                "pid",
                "process_start_ticks",
                "name",
                "state",
                "read_started_ns",
                "read_finished_ns",
                "cpu_total_ns",
                "rss_bytes",
                "cpu_percent_one_core",
            },
        )
        require(
            target({key: item[key] for key in ("pid", "process_start_ticks", "name")})
            == self.fixed,
            "observer_process_identity_changed",
        )
        require(self.state == "running", "observer_samples_after_target_lost")
        start = integer(item["read_started_ns"], lower, upper)
        finish = integer(item["read_finished_ns"], start, upper)
        mid = start + (finish - start) // 2
        cpu = metric(item["cpu_total_ns"], "int")
        rss = metric(item["rss_bytes"], "int")
        percent = metric(item["cpu_percent_one_core"], "float")
        if item["state"] != "running":
            require(allow_incomplete, "observer_target_not_running")
            allowed = {
                "exited": {"process_exited"},
                "identity_changed": {"process_identity_changed"},
                "unavailable": {"permission_denied", "process_unavailable"},
            }
            require(type(item["state"]) is str and item["state"] in allowed)
            reason = item["cpu_total_ns"]["reason"]
            require(type(reason) is str and reason in allowed[item["state"]])
            require(cpu is None and rss is None and percent is None)
            for key in ("cpu_total_ns", "rss_bytes", "cpu_percent_one_core"):
                require(item[key]["reason"] == reason)
                require(
                    item[key]["source"]
                    == (DELTA_SOURCE if key == "cpu_percent_one_core" else "native_process")
                )
            self.state, self.stop_reason = item["state"], reason
            self.previous = None
            self.rss_missing += 1
            return
        for key in ("cpu_total_ns", "rss_bytes", "cpu_percent_one_core"):
            source = item[key]["source"]
            require(
                key not in self.sources or source == self.sources[key], "observer_source_changed"
            )
            self.sources[key] = source
        require(item["cpu_percent_one_core"]["source"] == DELTA_SOURCE)
        missing_reason = "cpu_counter_unavailable" if cpu is None else "first_observation"
        if cpu is not None and self.previous is not None:
            old_cpu, old_mid = self.previous
            if cpu < old_cpu or mid <= old_mid:
                missing_reason = "counter_or_clock_regressed"
            else:
                expected = 100 * (cpu - old_cpu) / (mid - old_mid)
                require(percent is not None and math.isclose(percent, expected, abs_tol=1e-8))
                self.cpu_ns += cpu - old_cpu
                self.covered_ns += mid - old_mid
        if cpu is None or self.previous is None or missing_reason == "counter_or_clock_regressed":
            require(percent is None and item["cpu_percent_one_core"]["reason"] == missing_reason)
        self.previous = None if cpu is None else (cpu, mid)
        if rss is None:
            self.rss_missing += 1
        else:
            self.rss_max = rss if self.rss_max is None else max(self.rss_max, rss)

    def summary(self) -> dict:
        return {
            "pid": self.fixed[0],
            "process_start_ticks": self.fixed[1],
            "sources": self.sources,
            "last_state": self.state,
            "stop_reason": self.stop_reason,
            "peak_sampled_rss_bytes": self.rss_max,
            "rss_missing_samples": self.rss_missing,
            "mean_cpu_percent_one_core": 100 * self.cpu_ns / self.covered_ns
            if self.covered_ns
            else None,
            "cpu_coverage_ns": self.covered_ns,
            "cpu_missing_reason": None if self.covered_ns else "insufficient_valid_counter_pairs",
            "rss_missing_reason": None if self.rss_max is not None else "no_valid_rss_samples",
        }


def health(item: object) -> None:
    item = fields(
        item, {"state", "reason", "status_code", "pid_ownership_verified", "checked_utc", "cached"}
    )
    require(item["pid_ownership_verified"] is False and type(item["cached"]) is bool)
    state = item["state"]
    require(state in ("not_observed", "reachable", "http_error", "unavailable"))
    if state == "not_observed":
        require(
            item["reason"] == "not_requested"
            and item["status_code"] is None
            and item["checked_utc"] is None
            and item["cached"] is False
        )
    else:
        text(item["checked_utc"], 64)
        if state == "unavailable":
            require(item["reason"] == "request_failed_or_timed_out" and item["status_code"] is None)
        else:
            status = integer(item["status_code"], 100, 599)
            require(
                (state == "reachable" and 200 <= status < 300 and item["reason"] is None)
                or (
                    state == "http_error"
                    and not 200 <= status < 300
                    and item["reason"] == "http_non_success"
                )
            )
