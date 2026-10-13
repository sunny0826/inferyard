"""Offline, run-bound E01–E05 imports; a certificate hash is not physical calibration proof."""

import argparse
import html
import json
import math
import statistics
from datetime import datetime
from pathlib import Path

from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import (
    EvidenceError,
    atomic_bytes,
    json_bytes,
    local_file,
    read_json,
    sha256_file,
)

METRICS = {"E01": "bytes", "E02": "percent", "E03": "W", "E04": "J"}


def require(condition, reason):
    if not condition:
        raise EvidenceError(reason)


def finite(value):
    try:
        return type(value) in (int, float) and math.isfinite(value)
    except OverflowError:
        return False


def validate_source(source):
    require(type(source) is dict, "measurement_source_missing")
    for field in ("source_id", "instrument", "device_id", "capture_method", "clock_id"):
        require(
            type(source.get(field)) is str and 0 < len(source[field]) <= 512,
            "measurement_source_identity_missing",
        )
    require(source.get("scope") in ("component", "whole_host"), "measurement_scope_invalid")
    code = source.get("metric_id")
    require(code in METRICS and source.get("unit") == METRICS[code], "measurement_unit_invalid")
    if code in ("E01", "E02"):
        require(source["scope"] == "component", "gpu_import_requires_component_scope")
    calibration = source.get("calibration")
    require(type(calibration) is dict, "measurement_calibration_missing")
    require(
        calibration.get("status") in ("documented", "not_calibrated", "unavailable"),
        "measurement_calibration_status_invalid",
    )
    if calibration["status"] == "documented":
        require(
            type(calibration.get("performed_at")) is str and calibration["performed_at"],
            "measurement_calibration_date_missing",
        )
        try:
            date = datetime.fromisoformat(calibration["performed_at"])
            require(date.tzinfo is not None, "measurement_calibration_date_requires_timezone")
        except ValueError as exc:
            raise EvidenceError("measurement_calibration_date_invalid") from exc
        require(
            finite(calibration.get("uncertainty")) and calibration["uncertainty"] >= 0,
            "measurement_uncertainty_invalid",
        )
        require(
            calibration.get("uncertainty_unit") == METRICS[code],
            "measurement_uncertainty_unit_invalid",
        )
        digest = calibration.get("certificate_sha256")
        require(
            type(digest) is str
            and len(digest) == 64
            and all(c in "0123456789abcdef" for c in digest),
            "measurement_calibration_certificate_missing",
        )
    else:
        require(
            type(calibration.get("reason")) is str and calibration["reason"],
            "measurement_calibration_reason_missing",
        )


def reduce_channel(channel, window, clock_id, successful_tasks):
    require(type(channel) is dict, "measurement_channel_invalid")
    source = channel.get("source")
    validate_source(source)
    require(source["clock_id"] == clock_id, "measurement_clock_alignment_not_bound")
    samples = channel.get("samples")
    require(type(samples) is list and len(samples) >= 2, "measurement_samples_missing")
    maximum_gap = channel.get("max_gap_ns")
    require(type(maximum_gap) is int and maximum_gap > 0, "measurement_gap_policy_missing")
    code = source["metric_id"]
    previous = None
    for row in samples:
        require(type(row) is dict, "measurement_sample_invalid")
        timestamp = row.get("monotonic_ns")
        require(
            type(timestamp) is int and window[0] <= timestamp <= window[1],
            "measurement_clock_invalid",
        )
        require(previous is None or timestamp > previous, "measurement_samples_not_ordered")
        previous = timestamp
        value = row.get("value")
        if value is None:
            require(
                type(row.get("missing_reason")) is str and row["missing_reason"],
                "measurement_missing_reason_required",
            )
        else:
            require(finite(value) and value >= 0, "measurement_value_invalid")
            require(
                row.get("missing_reason") is None, "measurement_value_and_missing_reason_conflict"
            )
            require(code != "E02" or value <= 100, "measurement_gpu_utilization_invalid")
    missing = sum(row.get("value") is None for row in samples)
    known = [row["value"] for row in samples if row.get("value") is not None]
    gaps = [
        b["monotonic_ns"] - a["monotonic_ns"] for a, b in zip(samples, samples[1:], strict=False)
    ]
    reasons = []
    if samples[0]["monotonic_ns"] != window[0] or samples[-1]["monotonic_ns"] != window[1]:
        reasons.append("window_endpoints_not_observed")
    if missing:
        reasons.append("missing_samples")
    if max(gaps) > maximum_gap:
        reasons.append("sample_gap_exceeds_declared_limit")
    value = statistic = None
    if code == "E01":
        value = max(known) if known else None
        statistic = "observed_sample_peak_not_instantaneous_peak"
    elif code in ("E02", "E03"):
        value = statistics.mean(known) if known else None
        statistic = "arithmetic_sample_mean_not_time_average"
    elif code == "E04":
        statistic = "declared_counter_increment_over_bound_window"
        modulus = channel.get("counter_modulus")
        require(
            modulus is None or finite(modulus) and modulus > 0,
            "measurement_counter_modulus_invalid",
        )
        total = 0
        for before, after in zip(samples, samples[1:], strict=False):
            if before.get("value") is None or after.get("value") is None:
                continue
            wraps = after.get("wraps_since_previous")
            if type(wraps) is not int or wraps not in (0, 1):
                reasons.append("counter_wrap_or_reset_unknown")
                continue
            if wraps and modulus is None:
                reasons.append("counter_modulus_missing")
                continue
            if modulus is not None:
                require(
                    max(before["value"], after["value"]) < modulus,
                    "measurement_counter_out_of_range",
                )
            delta = after["value"] - before["value"] + (modulus or 0) * wraps
            if delta < 0:
                reasons.append("counter_reset_or_invalid_wrap")
            else:
                total += delta
        value = total if not reasons else None
    result = {
        "metric_id": code,
        "source": source,
        "value": value,
        "unit": METRICS[code],
        "statistic": statistic,
        "samples": len(samples),
        "missing_samples": missing,
        "window_ns": list(window),
        "reasons": sorted(set(reasons)),
        "physical_calibration_verified": False,
        "comparison_eligible": False,
    }
    if code == "E04":
        result["successful_tasks"] = successful_tasks
        result["energy_per_successful_task"] = (
            value / successful_tasks if value is not None and successful_tasks else None
        )
        result["E05_missing_reason"] = (
            "no_successful_tasks"
            if not successful_tasks
            else "energy_missing"
            if value is None
            else None
        )
    return result


def build(spec, data):
    require(type(spec) is dict, "measurement_spec_invalid")
    require(spec.get("definition") == "external_measurements.v1", "measurement_definition_invalid")
    require(spec.get("run_id") == data["run"]["run_id"], "measurement_run_binding_mismatch")
    planned = data["requests"]
    requests = [
        r for r in planned if type(r["t_send_ns"]) is int and type(r["t_terminal_ns"]) is int
    ]
    require(
        bool(requests)
        and all(type(r["t_send_ns"]) is int and type(r["t_terminal_ns"]) is int for r in requests),
        "measurement_run_window_unavailable",
    )
    window = (min(r["t_send_ns"] for r in requests), max(r["t_terminal_ns"] for r in requests))
    require(list(window) == spec.get("window_ns"), "measurement_window_binding_mismatch")
    clocks = {r["clock_id"] for r in requests}
    require(len(clocks) == 1, "measurement_run_clock_ambiguous")
    channels = spec.get("channels")
    require(type(channels) is list and bool(channels), "measurement_channels_missing")
    require(
        all(type(c) is dict and type(c.get("source")) is dict for c in channels),
        "measurement_channel_invalid",
    )
    keys = [
        (c.get("source", {}).get("metric_id"), c.get("source", {}).get("source_id"))
        for c in channels
    ]
    require(len(keys) == len(set(keys)), "measurement_channel_duplicate")
    successful = sum(
        r["execution_state"] == "completed" and r["quality_state"] == "pass" for r in requests
    )
    return {
        "definition": "external_measurement_analysis.v1",
        "run_id": spec["run_id"],
        "window_ns": list(window),
        "successful_formal_tasks": successful,
        "planned_requests": len(planned),
        "requests_with_bound_windows": len(requests),
        "channels": [reduce_channel(c, window, next(iter(clocks)), successful) for c in channels],
        "physical_measurement_qualification": False,
        "limitations": [
            "imported_claims_not_independently_verified_instrument_or_calibration",
            "component_energy_not_whole_host_energy",
            "sample_peak_and_mean_not_continuous_bounds_or_time_average",
            "missing_unknown_and_unqualified_values_never_become_zero_or_comparable",
            "observed_formal_interval_not_unexecuted_or_unclosed_requests",
        ],
    }


def report(result):
    document = "<!doctype html><meta charset=utf-8><title>外部测量导入</title><h1>外部测量导入</h1>"
    document += "<p>来源与声明校准可追溯；实际设备计量和比较资格未验证。</p><pre>"
    document += (
        html.escape(json.dumps(result, ensure_ascii=False, indent=2, sort_keys=True)) + "</pre>"
    )
    return document.encode()


def verify_import(out):
    metadata = (
        "artifact-hashes.json" if (out / "artifact-hashes.json").exists() else "verification.json"
    )
    verification = read_json(local_file(out, metadata))
    saved = read_json(local_file(out, "analysis.json"))
    spec = read_json(local_file(out, "input.json"))
    require(
        sha256_file(local_file(out, "input.json")) == saved["import_spec_sha256"],
        "measurement_import_input_changed",
    )
    run = Path(saved["run_path"])
    require(
        sha256_file(run / "manifest.json") == spec["manifest_sha256"],
        "measurement_manifest_binding_mismatch",
    )
    expected = build(spec, read_trial(run))
    certificates = []
    for channel in spec["channels"]:
        calibration = channel["source"]["calibration"]
        if calibration["status"] == "documented":
            name = f"calibration-{len(certificates)}.cert"
            require(
                sha256_file(local_file(out, name)) == calibration["certificate_sha256"],
                "measurement_certificate_hash_mismatch",
            )
            certificates.append({"path": name, "sha256": calibration["certificate_sha256"]})
    expected.update(
        certificate_files_verified=certificates,
        source_manifest_sha256=spec["manifest_sha256"],
        import_spec_sha256=saved["import_spec_sha256"],
        run_path=str(run.resolve()),
    )
    require(saved == expected, "measurement_recomputation_mismatch")
    require(
        local_file(out, "report.html").read_bytes() == report(expected),
        "measurement_report_recomputation_mismatch",
    )
    require(
        sha256_file(local_file(out, "analysis.json")) == verification["analysis_sha256"],
        "measurement_analysis_changed",
    )
    require(
        sha256_file(local_file(out, "report.html")) == verification["report_sha256"],
        "measurement_report_changed",
    )
    return {"verified": True, "model_requests": 0, "physical_measurement_qualification": False}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--spec", type=Path)
    parser.add_argument("--run", type=Path)
    parser.add_argument("--out", type=Path)
    parser.add_argument("--verify", type=Path)
    args = parser.parse_args()
    if args.verify:
        require(not any((args.spec, args.run, args.out)), "measurement_import_arguments_conflict")
        print(json.dumps(verify_import(args.verify)))
        return
    require(all((args.spec, args.run, args.out)), "measurement_import_arguments_missing")
    spec = read_json(args.spec)
    require(type(spec) is dict, "measurement_spec_invalid")
    manifest = args.run / "manifest.json"
    before = sha256_file(manifest)
    require(spec.get("manifest_sha256") == before, "measurement_manifest_binding_mismatch")
    data = read_trial(args.run)
    result = build(spec, data)
    certificates = []
    for channel in spec["channels"]:
        calibration = channel["source"]["calibration"]
        if calibration["status"] != "documented":
            continue
        name = calibration.get("certificate_file")
        require(
            type(name) is str and not Path(name).is_absolute(),
            "measurement_certificate_path_invalid",
        )
        root = args.spec.parent.resolve()
        certificate = (root / name).resolve()
        require(
            certificate.is_relative_to(root) and certificate.is_file(),
            "measurement_certificate_path_invalid",
        )
        require(
            sha256_file(certificate) == calibration["certificate_sha256"],
            "measurement_certificate_hash_mismatch",
        )
        certificates.append((certificate, calibration["certificate_sha256"]))
    result["certificate_files_verified"] = [
        {"path": f"calibration-{index}.cert", "sha256": digest}
        for index, (_, digest) in enumerate(certificates)
    ]
    result["source_manifest_sha256"] = before
    result["import_spec_sha256"] = sha256_file(args.spec)
    result["run_path"] = str(args.run.resolve())
    args.out.mkdir(parents=True, exist_ok=False)
    for index, (certificate, _) in enumerate(certificates):
        atomic_bytes(args.out / f"calibration-{index}.cert", certificate.read_bytes())
    atomic_bytes(args.out / "input.json", args.spec.read_bytes())
    atomic_bytes(args.out / "analysis.json", json_bytes(result))
    atomic_bytes(args.out / "report.html", report(result))
    require(sha256_file(manifest) == before, "measurement_source_changed_during_import")
    hashes = {
        "physical_measurement_qualification": False,
        "source_manifest_unchanged": True,
        "model_requests": 0,
        "analysis_sha256": sha256_file(args.out / "analysis.json"),
        "report_sha256": sha256_file(args.out / "report.html"),
        "importer_sha256": sha256_file(Path(__file__)),
    }
    atomic_bytes(args.out / "artifact-hashes.json", json_bytes(hashes))
    verify_import(args.out)
    atomic_bytes(
        args.out / "verification.json", json_bytes({**hashes, "software_import_verified": True})
    )
    print(
        json.dumps(
            {
                "software_import_verified": True,
                "channels": len(result["channels"]),
                "physical_measurement_qualification": False,
            }
        )
    )


if __name__ == "__main__":
    main()
