"""Evidence-derived display facets; recipe equality is not comparison eligibility."""

import hashlib
from datetime import UTC, datetime

from inferyard.config.bundle import content_hash
from inferyard.evidence.storage import json_bytes


def run_facets(data, *, first_event_utc):
    date = None
    # First recorded event is the date anchor; never infer time from directory names.
    if first_event_utc is not None:
        try:
            value = datetime.fromisoformat(first_event_utc.replace("Z", "+00:00"))
            if value.tzinfo is not None:
                date = value.astimezone(UTC).date().isoformat()
        except KeyError, TypeError, ValueError, AttributeError:
            pass
    config, bundle = data["config"], data["bundle"]
    recipe = {key: config[key] for key in ("conditions", "generation", "execution", "telemetry")}
    recipe_id = hashlib.sha256(json_bytes(recipe)).hexdigest()
    return {
        "date_utc": date,
        "date_source": "first_recorded_event_utc" if date else "unavailable",
        "bundle_id": content_hash(bundle),
        "bundle_label": f"{bundle['bundle_id']} · {bundle['version']}",
        "recipe_id": recipe_id,
        "recipe_label": f"{config['conditions']['threads']} 线程 · "
        f"上下文 {config['conditions']['context_size']} · "
        f"输出预算 {config['generation']['max_tokens']} · {recipe_id[:12]}",
        "recipe": recipe,
    }


def facet_options(runs):
    result = {}
    for kind in ("bundle", "recipe"):
        options = {run["facets"][kind + "_id"]: run["facets"][kind + "_label"] for run in runs}
        result[kind] = [
            {"id": key, "label": label}
            for key, label in sorted(options.items(), key=lambda p: (p[1], p[0]))
        ]
    return result
