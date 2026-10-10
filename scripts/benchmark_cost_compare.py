"""Fail-closed software output comparison with narrow, recorded provenance exceptions."""

import hashlib
import html
import json
from copy import deepcopy
from urllib.parse import unquote


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def inventory(root):
    return {
        p.relative_to(root).as_posix(): {"sha256": digest(p), "bytes": p.stat().st_size}
        for p in sorted(root.rglob("*"))
        if p.is_file() and "__pycache__" not in p.parts
    }


def canonical(value):
    return json.dumps(
        value, ensure_ascii=False, sort_keys=True, separators=(",", ":"), allow_nan=False
    )


def require_equal(left, right, label):
    if canonical(left) != canonical(right):
        raise ValueError(f"content_difference:{label}")


def compare_outputs(left, right, operation):
    """Never discard arbitrary sha256 keys, rows, metrics, text, or HTML elements."""
    a = json.loads((left / "result.json").read_text())
    b = json.loads((right / "result.json").read_text())
    changes = []
    if operation != "report":
        require_equal(a, b, operation)
    else:
        original = deepcopy(b)
        a_identity = json.loads((left / "identity.json").read_text())["source_sha256"]
        b_identity = json.loads((right / "identity.json").read_text())["source_sha256"]
        if a["generator_source_sha256"] != a_identity or b["generator_source_sha256"] != b_identity:
            raise ValueError("producer_identity_mismatch")
        changes.append(
            {
                "field": "/generator_source_sha256",
                "baseline": a_identity,
                "candidate": b_identity,
                "reason": "actual loaded producer source identity",
            }
        )
        b["generator_source_sha256"] = a_identity
        replacements = []
        if len(a["runs"]) != len(b["runs"]):
            raise ValueError("content_difference:report_run_count")
        for i, (ar, br) in enumerate(zip(a["runs"], b["runs"], strict=True)):
            for key in ["source", "evidence"]:
                fields = ["path"] if key == "source" else list(ar[key])
                for field in fields:
                    av, bv = ar[key][field], br[key][field]
                    ap = (left / "report" / unquote(av)).resolve()
                    bp = (right / "report" / unquote(bv)).resolve()
                    if ap != bp:
                        raise ValueError("report_source_target_changed")
                    if av != bv:
                        changes.append(
                            {
                                "field": f"/runs/{i}/{key}/{field}",
                                "baseline": av,
                                "candidate": bv,
                                "reason": "same resolved input location",
                            }
                        )
                        if key == "evidence":
                            replacements.append((bv, av))
                    br[key][field] = av
        require_equal(a, b, "report_index")
        # Compare the saved index too: result.json must not mask a truncated on-disk artifact.
        require_equal(
            a, json.loads((left / "report/index.json").read_text()), "baseline_disk_index"
        )
        require_equal(
            original, json.loads((right / "report/index.json").read_text()), "candidate_disk_index"
        )
        ah = (left / "report/report.html").read_text()
        bh = (right / "report/report.html").read_text()
        if a_identity != b_identity:
            old = f"报告生成源码<br>{b_identity}</p></footer>"
            new = f"报告生成源码<br>{a_identity}</p></footer>"
            if bh.count(old) != 1:
                raise ValueError("unrecognized_html_producer_location")
            bh = bh.replace(old, new, 1)
        for old, new in sorted(set(replacements), key=lambda pair: len(pair[0]), reverse=True):
            if old != new:
                bh = bh.replace(
                    f'href="{html.escape(old, quote=True)}"',
                    f'href="{html.escape(new, quote=True)}"',
                )
        if ah != bh:
            raise ValueError("content_difference:complete_html")
        for root in (left, right):
            seal = json.loads((root / "report/artifact-manifest.json").read_text())
            require_equal(
                seal,
                {
                    "definition": "presentation-seal.v1",
                    "files": {
                        name: digest(root / "report" / name)
                        for name in ("index.json", "report.html")
                    },
                },
                "report_seal",
            )
    # verify CLI diagnostics are part of its result, not just an exit-code check.
    if operation in ("verify", "rerender", "verify_multirun", "rerender_multirun"):
        require_equal(
            json.loads((left / "stdout.txt").read_text()),
            json.loads((right / "stdout.txt").read_text()),
            "verify_stdout",
        )
    return changes


def counts(root):
    result = json.loads((root / "result.json").read_text())
    if isinstance(result, list) and result and "requests" in result[0]:
        return {
            "runs": len(result),
            "rows": sum(len(d["requests"]) for d in result),
            "samples": sum(len(d.get("samples", [])) for d in result),
            "answer_utf8_bytes": sum(
                len(r.get("content", "").encode()) for d in result for r in d["requests"]
            ),
        }
    if isinstance(result, dict) and "runs" in result:
        return {
            "runs": len(result["runs"]),
            "rows": sum(len(d["requests"]) for d in result["runs"]),
            "svg_gallery": sum(len(d["svg_gallery"]) for d in result["runs"]),
            "answer_utf8_bytes": sum(
                len(result["contents"][r["content_ref"]].encode())
                if "content_ref" in r
                else len(r["content"].encode())
                for d in result["runs"]
                for r in d["requests"]
            ),
        }
    return {"result_bytes": (root / "result.json").stat().st_size}


def check_expected(root, corpus, operation):
    """Check independently specified workload size and answer bytes before accepting timings."""
    if operation not in ("read_trial", "report"):
        return
    expected = json.loads((corpus / "corpus.json").read_text())
    result = json.loads((root / "result.json").read_text())
    runs = result if operation == "read_trial" else result["runs"]
    require_equal(expected["run_count"], len(runs), "run_count")
    ids = [r["run"]["run_id"] if operation == "read_trial" else r["source"]["run_id"] for r in runs]
    if len(set(ids)) != len(ids):
        raise ValueError("duplicate_run_id")
    answers = [
        [
            hashlib.sha256(
                (
                    result["contents"][row["content_ref"]]
                    if operation == "report" and "content_ref" in row
                    else row.get("content", "")
                ).encode()
            ).hexdigest()
            for row in run["requests"]
        ]
        for run in runs
    ]
    require_equal(expected["answer_sha256"], answers, "fixture_answer_completeness")
    require_equal(expected["cases"], sum(len(r["requests"]) for r in runs), "row_count")
    if operation == "read_trial":
        require_equal(expected["samples"], sum(len(r["samples"]) for r in runs), "sample_count")
    else:
        require_equal(
            expected["svg_gallery_count"],
            sum(len(r["svg_gallery"]) for r in runs),
            "svg_gallery_count",
        )
        if expected["run_count"] > 2 and result["comparison"] is not None:
            raise ValueError("multirun_report_unexpected_comparison")
