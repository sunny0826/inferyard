"""Map raw-resource reductions to the common observation contract."""

from inferyard.analysis.observations import Observations
from inferyard.analysis.resource_metrics import reduce_resources
from inferyard.analysis.sensor_observations import add_sensor_observations


def build_resource_observations(run, workload_id, requests, samples, config, evidence, *, complete):
    reduced = reduce_resources(requests, samples, config)
    output = Observations(run, workload_id, evidence, complete=complete)
    for row in reduced["requests"]:
        metrics = row["metrics"]

        def add(
            code, statistic, metric, *, value=None, use_value=False, reason=None, unit=None, row=row
        ):
            return output.add(
                code,
                statistic,
                value if use_value else metric["value"],
                category=row["category"],
                request=row["request_id"],
                count=metric["sample_count"],
                excluded=metric["excluded"],
                reason=reason or metric["reason"],
                limits=metric["limitations"],
                interval=metric.get("interval"),
                coverage=metric.get("coverage"),
                unit=unit,
            )

        for code, statistic in (
            ("C01", "observed_min"),
            ("C02", "observed_peak"),
            ("C03", "peak_minus_idle_baseline"),
            ("C04", "observed_cpu_seconds"),
            ("C05", "observed_single_core_percent"),
        ):
            add(code, statistic, metrics[code])
        cpu = metrics["C04"]
        add(
            "C04",
            "full_request_cpu_seconds",
            cpu,
            use_value=True,
            value=cpu["value"] if cpu["coverage"] == 1 else None,
            reason=cpu["reason"]
            or ("request_boundaries_not_covered" if cpu["coverage"] != 1 else None),
        )
        for name, metric in metrics["C06"].items():
            add("C06", name + "_pages", metric)
            add(
                "C06",
                name + "_bytes",
                metric,
                use_value=True,
                value=metric["value"] * metric["scale"] if metric["value"] is not None else None,
                unit="bytes",
            )
    add_sensor_observations(output, requests, samples)
    return reduced, output.items
