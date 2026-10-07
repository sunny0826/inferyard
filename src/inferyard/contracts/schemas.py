"""One active wire contract, exported as JSON Schema Draft 2020-12."""

from copy import deepcopy

from inferyard import SCHEMA_VERSION
from inferyard.contracts.schemas_common import *  # noqa: F403
from inferyard.contracts.schemas_common import VERSION, obj
from inferyard.contracts.schemas_config import CONFIG, CONFIG_INPUT
from inferyard.contracts.schemas_config import CONFIG_DEFAULTS as CONFIG_DEFAULTS
from inferyard.contracts.schemas_tasks import BUNDLE, SCORE
from inferyard.contracts.schemas_tasks import CASE as CASE
from inferyard.contracts.schemas_tasks import CASE_BASE as CASE_BASE


def _wire_spec(protocol):
    """Attach the common envelope without changing a protocol definition ID."""
    return obj(
        {**protocol["properties"], "schema_version": VERSION},
        list(dict.fromkeys(["schema_version", *protocol["required"]])),
    )


def schemas_for():
    """Return the unique current registry; legacy wire revisions need migration."""
    from inferyard.contracts.schemas_events import EVENT, SAMPLE, SELECTION
    from inferyard.contracts.schemas_experiment import (
        ANALYSIS,
        EXPERIMENT,
        METRIC_DEFINITION,
        METRIC_OBSERVATION,
        PLAN,
        RUN,
    )
    from inferyard.contracts.schemas_extensions import (
        CLOSED_CONCURRENCY,
        NATIVE_TOOLS,
        TOTAL_OBSERVER_CONTROL,
        TOTAL_TRIAL_CONTROL,
    )
    from inferyard.contracts.schemas_storage import MANIFEST, SUMMARY

    return {
        "config": CONFIG,
        "config_input": CONFIG_INPUT,
        "bundle": BUNDLE,
        "score": SCORE,
        "event": EVENT,
        "sample": SAMPLE,
        "selection": SELECTION,
        "experiment": EXPERIMENT,
        "plan": PLAN,
        "run": RUN,
        "metric_definition": METRIC_DEFINITION,
        "metric_observation": METRIC_OBSERVATION,
        "analysis": ANALYSIS,
        "manifest": MANIFEST,
        "summary": SUMMARY,
        "closed_concurrency": _wire_spec(CLOSED_CONCURRENCY),
        "native_tools": _wire_spec(NATIVE_TOOLS),
        "total_observer_control": {
            "oneOf": [_wire_spec(TOTAL_OBSERVER_CONTROL), _wire_spec(TOTAL_TRIAL_CONTROL)]
        },
    }


def export_schema(kind):
    """Return a detached schema for the sole active wire revision."""

    return {
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "$id": f"urn:local-ai-bench:schema:v{SCHEMA_VERSION}:{kind}",
        "title": f"InferYard {kind} v{SCHEMA_VERSION}",
        **deepcopy(schemas_for()[kind]),
    }
