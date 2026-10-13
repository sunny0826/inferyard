import csv
import io
import json

import pytest

from inferyard.reporting.export import csv_cell, formatted_files, markdown_cell


@pytest.mark.parametrize("value", ["=1+1", "+cmd", "-cmd", "@SUM(A1)", "  =1", "\t=1", "\n=1"])
def test_formula_like_strings_are_text(value):
    assert csv_cell(value) == "'" + value


def test_negative_numbers_null_and_boolean_keep_meaning():
    assert csv_cell(-1.25) == "-1.25"
    assert csv_cell(None) == ""
    assert csv_cell(False) == "false"
    assert csv_cell(0) == "0"


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


def test_markdown_cannot_inject_html_link_or_table_column():
    text = markdown_cell('<img onerror="x">[link](javascript:x)|`code`\nnext')
    assert "<img" not in text
    assert r"\[link\]" in text and r"\|" in text
    assert "<br>" in text and "\n" not in text
