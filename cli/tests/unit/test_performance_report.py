from inferyard.reporting.report import _environment


def test_comparison_table_escapes_labels_and_keeps_refused_difference_empty():
    module = (
        _environment()
        .get_template("report_comparison.html")
        .make_module(
            {
                "index": {
                    "runs": [],
                    "filter_options": {"bundle": [], "recipe": []},
                    "comparison": None,
                    "generator_source_sha256": "test",
                },
            }
        )
    )
    row = {
        "metric_id": "L03",
        "statistic": "<script>alert(1)</script>",
        "category": "<img src=x onerror=alert(1)>",
        "case_id": None,
        "source": "<svg onload=alert(1)>",
        "unit": "ms",
        "eligible": False,
        "difference": None,
        "left": 1,
        "right": 2,
        "reasons": ["<script>bad()</script>"],
        "left_limits": [],
        "right_limits": [],
    }
    html = str(module.performance_table([row]))
    assert "<script>" not in html and "<img " not in html and "<svg " not in html
    assert "&lt;script&gt;" in html and "&lt;img " in html
    assert 'data-eligible="false"' in html
    assert "— ms" in html
