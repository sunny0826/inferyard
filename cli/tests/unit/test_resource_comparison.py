from copy import deepcopy

import pytest

from inferyard.analysis.comparison import compare_trials
from inferyard.analysis.resource_comparison import SOURCES
from inferyard.analysis.resource_observations import build_resource_observations
from inferyard.config.planning import compile_plan
from inferyard.runtime.overhead_binding import workload_identity
from tests.unit.test_observations import EVIDENCE, RUN
from tests.unit.test_performance_comparison import inputs
from tests.unit.phase2_contract_helpers import experiment
from tests.unit.test_resources import ENDPOINT, ROW, cpu_sample

POLICY = dict(
    min_coverage_ratio=0.8,
    max_window_offset_difference=0.05,
    max_sample_gap_intervals=2,
    min_baseline_samples=2,
)


def fixture():
    left, right, proofs = inputs()
    for data in (left, right):
        data["plan"]["experiment"]["resource_comparison"] = deepcopy(POLICY)
        data["config"]["endpoint"] = deepcopy(ENDPOINT)
        data["config"]["telemetry"].update(interval_ms=500, baseline_seconds=1)
        r = {
            **ROW,
            "request_id": data["requests"][0]["request_id"],
            "case_id": "case",
            "t_send_ns": 2_000_000_000,
            "t_terminal_ns": 4_000_000_000,
        }
        data["requests"] = [r]
        samples = []
        for at in (2, 2.5, 3, 3.5, 4):
            cpu = cpu_sample(at, int(at * 10))
            cpu["request_id"] = r["request_id"]
            samples.append(cpu)
            for name in (
                "system_mem_available",
                "service_rss",
                "system_swap_in",
                "system_swap_out",
                "temperature",
                "frequency",
            ):
                s = {
                    **cpu,
                    "metric_name": name,
                    "value": 100,
                    "source": SOURCES.get(name, "sensor/" + name),
                    "unit": "pages"
                    if name.startswith("system_swap")
                    else "celsius"
                    if name == "temperature"
                    else "Hz"
                    if name == "frequency"
                    else "bytes",
                }
                if name.startswith("system_swap"):
                    s.update(
                        page_size_bytes=4096,
                        clock_ticks_per_second=None,
                        raw_cpu=None,
                        server_pid=None,
                        process_start_ticks=None,
                    )
                if name in ("temperature", "frequency"):
                    s["semantics"] = "reported"
                samples.append(s)
        for at in (0, 0.5, 1):
            s = {
                **cpu_sample(at, 0),
                "request_id": None,
                "phase": "baseline",
                "metric_name": "service_rss",
                "source": SOURCES["service_rss"],
                "unit": "bytes",
                "value": 90,
            }
            samples.append(s)
        data["samples"] = samples
        data["resource_collector"] = {
            "collector": "linux-resource.v2",
            "interval_ms": 500,
            "boot_id": "boot-1",
            "clock_ticks_per_second": 100,
            "page_size_bytes": 4096,
            "cpu_scope": "bound_pid_all_threads_excludes_child_processes",
            "swap_scope": "host_including_swap_and_zram_not_model_attribution",
            "sensors": {
                "sources": [
                    {
                        "source": "sensor/" + name,
                        "metric_name": name,
                        "semantics": "reported",
                        "unit": "celsius" if name == "temperature" else "Hz",
                        "identity": {"path": name, "attributes": {"label": "known"}},
                        "scale": 1,
                        "missing_reason": None,
                    }
                    for name in ("temperature", "frequency")
                ]
            },
        }
        refresh(data)
    return left, right, proofs


def refresh(data):
    _, data["summary"]["metric_observations"] = build_resource_observations(
        RUN, "w", data["requests"], data["samples"], data["config"], EVIDENCE, complete=True
    )


def compare(left, right, proofs):
    return compare_trials(left, right, performance_evidence=proofs)["performance_analysis"]


@pytest.mark.parametrize(
    "change",
    [
        "policy",
        "unsealed",
        "source",
        "missing",
        "gap",
        "coverage",
        "offset",
        "pid",
        "scale",
        "scope",
        "sensor",
        "baseline",
        "count",
    ],
)
def test_bad_resource_window_is_not_authorized_by_e2e(change):
    left, right, proofs = fixture()
    code = "C02"
    if change == "policy":
        right["plan"]["experiment"]["resource_comparison"]["min_coverage_ratio"] = 0.9
    elif change == "unsealed":
        right.pop("resource_collector")
    elif change == "baseline":
        right["samples"] = [s for s in right["samples"] if s["phase"] != "baseline"]
        code = "C03"
    elif change == "sensor":
        right["resource_collector"]["sensors"]["sources"][0]["identity"]["attributes"]["label"] = (
            "other"
        )
        code = "C07"
    elif change == "scope":
        right["resource_collector"]["cpu_scope"] = "includes_children"
        code = "C04"
    elif change == "scale":
        right["resource_collector"]["clock_ticks_per_second"] = 200
        code = "C04"
    elif change == "gap":
        right["samples"] = [
            s
            for s in right["samples"]
            if s["metric_name"] != "service_rss"
            or s["phase"] == "baseline"
            or s["read_started_ns"] in (2_000_000_000, 4_000_000_000)
        ]
    elif change in ("coverage", "offset"):
        for data in (left, right):
            data["plan"]["experiment"]["resource_comparison"]["min_coverage_ratio"] = 0.7
        if change == "coverage":
            right["requests"][0]["t_terminal_ns"] = 5_000_000_000
        else:
            right["samples"] = [
                s
                for s in right["samples"]
                if not (
                    s["metric_name"] == "service_rss"
                    and s["phase"] == "formal"
                    and s["read_started_ns"] == 2_000_000_000
                )
            ]
    elif change == "count":
        next(m for m in right["summary"]["metric_observations"] if m["metric_id"] == code)[
            "sample_count"
        ] = 1
    else:
        s = next(
            s
            for s in right["samples"]
            if s["metric_name"] == "service_rss" and s["phase"] == "formal"
        )
        if change == "source":
            s["source"] = "other"
        elif change == "pid":
            s["server_pid"] += 1
        else:
            s.update(value=None, missing_reason="permission_denied")
    if change != "count":
        refresh(right)
    rows = [r for r in compare(left, right, proofs)["differences"] if r["metric_id"] == code]
    assert rows and not any(r["eligible"] for r in rows)
