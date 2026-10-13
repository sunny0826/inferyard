from copy import deepcopy

import pytest

from inferyard.config.loader import load_config
from inferyard.reporting.report_facets import facet_options, run_facets


def data(config_path):
    loaded = load_config(config_path)
    return {"config": loaded.config.to_dict(), "bundle": loaded.bundle.to_dict()}


@pytest.mark.parametrize(
    "utc, expected",
    [
        ("2026-09-30T01:00:00+08:00", "2026-09-29"),
        ("2026-09-30T00:00:00Z", "2026-09-30"),
        ("invalid", None),
        ("2026-09-30T01:00:00", None),
    ],
)
def test_dates_use_first_event_and_explicit_timezone(tmp_path, config_path, utc, expected):
    assert run_facets(data(config_path), first_event_utc=utc)["date_utc"] == expected


def test_facets_preserve_content_identity_without_runtime_binding(tmp_path, config_path):
    original = data(config_path)
    first = run_facets(original, first_event_utc=None)
    changed = deepcopy(original)
    changed["config"]["endpoint"]["server_pid"] += 1
    changed["bundle"]["review_records"] = []
    assert run_facets(changed, first_event_utc=None) == first
    changed["bundle"]["cases"][0]["prompt"] += " changed"
    second = run_facets(changed, first_event_utc=None)
    assert second["bundle_id"] != first["bundle_id"]
    assert second["recipe_id"] == first["recipe_id"]
    changed["config"]["conditions"]["threads"] += 1
    third = run_facets(changed, first_event_utc=None)
    assert third["recipe_id"] != first["recipe_id"]
    options = facet_options([{"facets": f} for f in (first, first, second, third)])
    assert len(options["bundle"]) == len(options["recipe"]) == 2
