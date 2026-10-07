"""Read-only CPUFreq policy inventory and conservative stability assessment."""

import re
from pathlib import Path

from inferyard.config.environment_binding import requires_match

FIELDS = (
    "scaling_driver",
    "scaling_governor",
    "energy_performance_preference",
    "scaling_min_freq",
    "scaling_max_freq",
)


def read(path):
    try:
        return path.read_text().strip() or None
    except OSError, UnicodeError:
        return None


def cpu_list(text):
    if not isinstance(text, str) or not text.strip():
        return None
    values = set()
    for part in re.split(r"[\s,]+", text.strip()):
        if not re.fullmatch(r"[0-9]+(?:-[0-9]+)?", part):
            return None
        edges = [int(v) for v in part.split("-")]
        first, last = edges[0], edges[-1]
        if first > last or last > 65535:
            return None
        values.update(range(first, last + 1))
    return sorted(values)


def snapshot(sys_root=Path("/sys")):
    root = sys_root / "devices/system/cpu"
    online = cpu_list(read(root / "online"))
    try:
        paths = sorted((root / "cpufreq").iterdir(), key=lambda p: p.name)
    except OSError:
        paths = []
    policies = []
    for path in paths:
        if not re.fullmatch(r"policy[0-9]+", path.name):
            continue
        item = {
            "policy_id": path.name,
            "affected_cpus": cpu_list(read(path / "affected_cpus")),
            "related_cpus": cpu_list(read(path / "related_cpus")),
        }
        for field in FIELDS:
            value = read(path / field)
            if field in ("scaling_min_freq", "scaling_max_freq"):
                value = int(value) if value and value.isascii() and value.isdecimal() else None
            item[field] = value
        policies.append(item)
    return {"source": "linux.sysfs.cpufreq.policy.v1", "online_cpus": online, "policies": policies}


def assess(snapshots, conditions):
    reasons = set()
    baseline = None
    for value in snapshots:
        if not isinstance(value, dict) or value.get("source") != "linux.sysfs.cpufreq.policy.v1":
            reasons.add("cpu_policy_inventory_missing")
            continue
        online, policies = value.get("online_cpus"), value.get("policies")

        def cpus(items):
            return (
                isinstance(items, list)
                and bool(items)
                and all(type(c) is int and 0 <= c <= 65535 for c in items)
                and items == sorted(set(items))
            )

        if not cpus(online) or not isinstance(policies, list) or not policies:
            reasons.add("cpu_policy_inventory_incomplete")
            continue
        seen, covered = set(), set()
        for item in policies:
            if not isinstance(item, dict):
                reasons.add("cpu_policy_record_invalid")
                continue
            name = item.get("policy_id")
            if not isinstance(name, str) or not re.fullmatch(r"policy[0-9]+", name) or name in seen:
                reasons.add("cpu_policy_identity_invalid")
                continue
            seen.add(name)
            affected, related = item.get("affected_cpus"), item.get("related_cpus")
            if not cpus(affected) or not cpus(related):
                reasons.add("cpu_policy_membership_unknown")
            else:
                if covered.intersection(affected):
                    reasons.add("cpu_policy_membership_overlap")
                covered.update(affected)
                if not set(affected).issubset(related):
                    reasons.add("cpu_policy_membership_inconsistent")
            for field in FIELDS:
                current = item.get(field)
                numeric = field in ("scaling_min_freq", "scaling_max_freq")
                if not (
                    type(current) is int and current > 0
                    if numeric
                    else isinstance(current, str) and bool(current)
                ):
                    reasons.add("cpu_policy_unknown:" + field)
            low, high = item.get("scaling_min_freq"), item.get("scaling_max_freq")
            if type(low) is int and type(high) is int and low > high:
                reasons.add("cpu_policy_frequency_limits_invalid")
            for field, frozen in (
                ("scaling_governor", "governor"),
                ("energy_performance_preference", "epp"),
            ):
                if (
                    frozen in conditions
                    and requires_match(conditions, frozen)
                    and item.get(field) != conditions[frozen]
                ):
                    reasons.add("cpu_policy_frozen_mismatch:" + frozen)
        if covered != set(online):
            reasons.add("cpu_policy_online_coverage_incomplete")
        canonical = {
            "online_cpus": online,
            "policies": sorted(
                [p for p in policies if isinstance(p, dict)], key=lambda p: str(p.get("policy_id"))
            ),
        }
        if baseline is None:
            baseline = canonical
        elif canonical != baseline:
            reasons.add("cpu_policy_changed")
    return {
        "complete_and_stable": bool(snapshots) and not reasons,
        "reasons": sorted(reasons),
        "snapshot_count": len(snapshots),
    }
