import csv
import io
import json

import pytest

from inferyard.reporting.export import csv_cell, formatted_files, markdown_cell


def test_all_formats_share_metric_values_without_rounding():
    analysis = {
        "analysis_id": "analysis-a",
        "metrics": [
            {
                "metric_id": "C03",
                "value": -123.456789,
                "denominator": 20,
                "status": "derived",
                "missing_reason": None,
                "group": {"category": None},
            },
            {
                "metric_id": "Q01",
                "value": None,
                "denominator": 40,
                "status": "missing",
                "missing_reason": "incomplete_trial",
                "group": {"category": "qa"},
            },
        ],
    }
    files = formatted_files(analysis)
    assert json.loads(files["analysis.json"]) == analysis
    rows = list(csv.DictReader(io.StringIO(files["metrics.csv"].decode())))
    assert rows[0]["value"] == "-123.456789"
    assert rows[1]["value"] == "" and rows[1]["denominator"] == "40"
    assert all(row["export_analysis_id"] == "analysis-a" for row in rows)
    assert "-123.456789" in files["metrics.md"].decode()
    assert "incomplete\\_trial" in files["metrics.md"].decode()
