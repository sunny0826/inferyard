"""Freeze the 199-item method catalogue and metric definitions.

Changing an established ID/title pair requires a reviewed version/mapping change;
reordering a Markdown table cannot silently reassign an existing identity.
"""

import argparse
import hashlib
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
OUTPUT = ROOT / "src/inferyard/data"
MATRIX = ROOT / "docs/reference/methods-matrix.md"
METRICS = ROOT / "docs/experiments/metrics.md"
IMPLEMENTED = {
    f"{prefix}{i:02}"
    for prefix, count in (("R", 4), ("Q", 8), ("L", 7), ("C", 8))
    for i in range(1, count + 1)
}
IMPLEMENTED |= {"S01", "S02", "S03", "S04", "X01", "X02", "X03"}
IMPLEMENTED |= {"E01", "E02", "E03", "E04", "E05", "N01", "N02", "N03", "U01"}
UNITS = {
    **{
        f"{prefix}{i:02}": "ratio"
        for prefix, count in (("R", 4), ("Q", 8), ("X", 3))
        for i in range(1, count + 1)
    },
    **{f"L{i:02}": "ms" for i in (1, 2, 3, 5)},
    **{f"L{i:02}": "token/s" for i in (4, 6, 7)},
    "C01": "bytes",
    "C02": "bytes",
    "C03": "bytes",
    "C04": "s",
    "C05": "percent",
    "C06": "pages",
    "C07": "celsius",
    "C08": "Hz",
    "S01": "ms",
    "S02": "bytes",
    "S03": "ratio",
    "S04": "ms",
    "E01": "bytes",
    "E02": "percent",
    "E03": "W",
    "E04": "J",
    "E05": "J/successful_task",
    "N01": "request/s",
    "N02": "request/s",
    "N03": "ms",
    "U01": "s",
}
LAYERS = {
    **{key: "process" for key in ("C02", "C03", "C04", "C05", "S02")},
    **{key: "host" for key in ("C01", "C06")},
    **{key: "device" for key in ("C07", "C08", "E01", "E02", "E03", "E04")},
    **{key: "engine" for key in ("L06", "L07")},
}


def reference(path):
    return {
        "path": path.relative_to(ROOT).as_posix(),
        "sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
    }


def methods():
    items, category = [], None
    for number, line in enumerate(MATRIX.read_text(encoding="utf-8").splitlines(), 1):
        match = re.match(r"^## (\d+)\. (.+)$", line)
        if match:
            category = (int(match[1]), match[2])
        if category and re.match(r"^\| \d+ \|", line):
            cells = [cell.strip() for cell in line.strip("|").split("|")]
            if len(cells) != 7:
                raise ValueError("method table column count changed")
            rank = int(cells[0])
            items.append(
                {
                    "method_id": f"method-{category[0]:02}-{rank:02}",
                    "category_id": category[0],
                    "category": category[1],
                    "original_rank": rank,
                    "title": cells[1],
                    "rationale": cells[2],
                    "procedure": cells[3],
                    "metric_rules": cells[4],
                    "evidence": cells[5],
                    "applicability": cells[6],
                    "source_line": number,
                }
            )
    if len(items) != 199 or len({i["method_id"] for i in items}) != 199:
        raise ValueError("expected 199 unique methods; review catalogue version before changing")
    return {"catalogue_version": "2026-09-29.v1", "source": reference(MATRIX), "items": items}


def metrics():
    items = []
    for line in METRICS.read_text(encoding="utf-8").splitlines():
        if not re.match(r"^\| [RQLCX SEN U]\d{2} \|", line):
            continue
        cells = [cell.strip() for cell in line.strip("|").split("|")]
        key, name = cells[:2]
        conditional = key[0] in "ENU"
        if len(cells) != (3 if conditional else 4):
            raise ValueError("metric table column count changed")
        source = {
            "Q": "versioned_scorer",
            "L": "stream_events",
            "C": "platform_resource_collector",
            "R": "request_ledger",
            "S": "trial_and_window_ledger",
            "X": "length_case_ledger",
            "E": "verified_sensor_or_instrument",
            "N": "load_and_token_events",
            "U": "verified_lifecycle_events",
        }[key[0]]
        if key in ("L06", "L07"):
            source = "verified_engine_timings"
        items.append(
            {
                "schema_version": 3,
                "metric_id": key,
                "name": name.split(" / ")[0],
                "definition_version": "phase2.v1",
                "unit": UNITS[key],
                "layer": LAYERS.get(key, "request" if key.startswith("L") else "experiment"),
                "source": source,
                "window": "frozen_protocol_and_declared_statistic",
                "applicability": cells[-1],
                "required_capabilities": [source],
                "aggregation": cells[2],
                "comparison_policy": "same_definition_source_window_and_complete_frozen_protocol",
                "implementation_status": "implemented" if key in IMPLEMENTED else "planned",
                "scope": "conditional" if conditional else "core",
                "evidence_refs": [reference(METRICS)]
                + (
                    [
                        reference(ROOT / "src/inferyard/analysis/observations.py"),
                        reference(ROOT / "tests/unit/test_observations.py"),
                    ]
                    if key in IMPLEMENTED
                    else []
                )
                + (
                    [
                        reference(ROOT / "src/inferyard/analysis/engine_timing.py"),
                        reference(ROOT / "tests/unit/test_engine_timing.py"),
                    ]
                    if key in ("L06", "L07")
                    else []
                )
                + (
                    [
                        reference(ROOT / "src/inferyard/platforms/resources_linux.py"),
                        reference(ROOT / "src/inferyard/platforms/resources_macos.py"),
                        reference(ROOT / "src/inferyard/runtime/environment_observer.py"),
                        reference(ROOT / "src/inferyard/platforms/sensors_linux.py"),
                        reference(ROOT / "src/inferyard/analysis/sensor_observations.py"),
                        reference(ROOT / "tests/unit/test_sensors_linux.py"),
                        reference(ROOT / "src/inferyard/analysis/resource_metrics.py"),
                        reference(ROOT / "tests/unit/test_resources.py"),
                    ]
                    if key.startswith("C") and key in IMPLEMENTED
                    else []
                )
                + (
                    [
                        reference(ROOT / "src/inferyard/platforms/resources_windows.py"),
                        reference(ROOT / "tests/unit/test_windows_resources.py"),
                    ]
                    if key in ("C01", "C02", "C03")
                    else []
                )
                + (
                    [reference(ROOT / "tests/unit/test_sensor_observations.py")]
                    if key in ("C07", "C08")
                    else []
                )
                + (
                    [
                        reference(ROOT / "src/inferyard/analysis/duration_windows.py"),
                        reference(ROOT / "src/inferyard/analysis/stability_observations.py"),
                        reference(ROOT / "src/inferyard/analysis/idle_rss.py"),
                        reference(ROOT / "tests/unit/test_stability_observations.py"),
                    ]
                    if key in ("S01", "S02")
                    else []
                )
                + (
                    [
                        reference(ROOT / "src/inferyard/analysis/repetition_metrics.py"),
                        reference(ROOT / "src/inferyard/analysis/repetition_analysis.py"),
                        reference(ROOT / "tests/unit/test_repetition_metrics.py"),
                        reference(ROOT / "tests/unit/test_aggregate_observations.py"),
                    ]
                    if key in ("S03", "S04")
                    else []
                ),
            }
        )
    if len(items) != 43 or len({i["metric_id"] for i in items}) != 43:
        raise ValueError("expected 34 core and 9 conditional groups")
    return {"catalogue_version": "phase2.v1", "items": items}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    OUTPUT.mkdir(exist_ok=True)
    for filename, data, id_key, title_key in (
        ("methods.json", methods(), "method_id", "title"),
        ("metrics.json", metrics(), "metric_id", "name"),
    ):
        path = OUTPUT / filename
        text = json.dumps(data, ensure_ascii=False, indent=2) + "\n"
        if path.exists():
            old = {
                item[id_key]: item[title_key]
                for item in json.loads(path.read_text(encoding="utf-8"))["items"]
            }
            if any(
                old.get(item[id_key], item[title_key]) != item[title_key] for item in data["items"]
            ):
                raise SystemExit("catalogue identity changed; review stable IDs and version")
        if args.check:
            if not path.exists() or path.read_text(encoding="utf-8") != text:
                raise SystemExit("stale catalogue: " + filename)
        else:
            path.write_text(text, encoding="utf-8", newline="\n")
    for filename in ("methods-v1.json", "metrics-v2.json"):
        obsolete = OUTPUT / filename
        if obsolete.exists():
            if args.check:
                raise SystemExit("obsolete catalogue: " + filename)
            obsolete.unlink()


if __name__ == "__main__":
    main()
