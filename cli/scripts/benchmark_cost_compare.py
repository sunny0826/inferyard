"""Fail-closed software output comparison; the Studio JSON must be identical."""

import hashlib
import json


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
    """Never discard arbitrary sha256 keys, rows, metrics or text."""
    a = json.loads((left / "result.json").read_text())
    b = json.loads((right / "result.json").read_text())
    if operation != "studio_json":
        require_equal(a, b, operation)
        return []
    # The Studio projection has no producer identity and no presentation seal: it
    # must be byte-for-byte reproducible from the same inputs. The worker records
    # its own source identity separately in identity.json.
    require_equal(a, b, "studio_json")
    # Compare the saved document too: result.json must not mask a truncated artifact.
    require_equal(a, json.loads((left / "report/studio.json").read_text()), "baseline_disk_json")
    require_equal(b, json.loads((right / "report/studio.json").read_text()), "candidate_disk_json")
    return []


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
        }
    return {"result_bytes": (root / "result.json").stat().st_size}


def check_expected(root, corpus, operation):
    """Check independently specified workload size and answer bytes before accepting timings."""
    if operation not in ("read_trial", "studio_json"):
        return
    expected = json.loads((corpus / "corpus.json").read_text())
    result = json.loads((root / "result.json").read_text())
    runs = result if operation == "read_trial" else result["runs"]
    require_equal(expected["run_count"], len(runs), "run_count")
    ids = [r["run"]["run_id"] if operation == "read_trial" else r["source"]["run_id"] for r in runs]
    if len(set(ids)) != len(ids):
        raise ValueError("duplicate_run_id")
    require_equal(expected["cases"], sum(len(r["requests"]) for r in runs), "row_count")
    if operation == "read_trial":
        require_equal(expected["samples"], sum(len(r["samples"]) for r in runs), "sample_count")
        answers = [
            [
                hashlib.sha256(row.get("content", "").encode()).hexdigest()
                for row in run["requests"]
            ]
            for run in runs
        ]
        require_equal(expected["answer_sha256"], answers, "fixture_answer_completeness")
        return
    # The Studio projection must not carry raw answers, SVG galleries or sample rows;
    # its cost is the remaining JSON write, not the retired presentation work.
    if result.get("kind") != "studio-report.v1" or "contents" in result:
        raise ValueError("studio_json_scope")
    for run in runs:
        if "svg_gallery" in run or "samples" in run:
            raise ValueError("studio_json_scope")
        for row in run["requests"]:
            if "content" in row or "answer" in row:
                raise ValueError("studio_json_scope")
