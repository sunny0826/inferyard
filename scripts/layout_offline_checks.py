"""Retain CLI receipts for fresh synthetic layout originals and independently damaged copies."""

import argparse
import hashlib
import json
import shutil
import subprocess
import sys
from pathlib import Path

BOOT = (
    "import sys,runpy; sys.path[:0]=[sys.argv.pop(1),sys.argv.pop(1)]; "
    "runpy.run_module('inferyard',run_name='__main__')"
)


def dump(path, value):
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2, allow_nan=False))


def inventory(root):
    return {
        path.relative_to(root).as_posix(): hashlib.sha256(path.read_bytes()).hexdigest()
        for path in sorted(root.rglob("*"))
        if path.is_file()
    }


def invoke(tree, arguments, out, expected=0):
    out.mkdir(parents=True, exist_ok=False)
    argv = [
        sys.executable,
        "-I",
        "-B",
        "-c",
        BOOT,
        str(tree / "src"),
        str(tree),
        *map(str, arguments),
    ]
    result = subprocess.run(argv, capture_output=True, timeout=60)
    (out / "stdout.txt").write_bytes(result.stdout)
    (out / "stderr.txt").write_bytes(result.stderr)
    record = {"argv": argv, "exit_code": result.returncode, "expected": expected}
    dump(out / "process.json", record)
    if result.returncode != expected:
        raise ValueError(f"unexpected_cli_exit:{result.returncode}:{expected}:{out}")
    value = json.loads(result.stdout)
    dump(out / "result.json", value)
    return value


def reseal(root):
    path = root / "manifest.json"
    manifest = json.loads(path.read_text())
    for name, info in manifest["files"].items():
        source = root / name
        if source.exists():
            raw = source.read_bytes()
            info.update(bytes=len(raw), sha256=hashlib.sha256(raw).hexdigest())
    dump(path, manifest)


def change_json(root, name, modify):
    path = root / name
    data = json.loads(path.read_text())
    modify(data)
    dump(path, data)


def change_rows(root, name, modify):
    path = root / name
    rows = [json.loads(line) for line in path.read_text().splitlines()]
    modify(rows)
    path.write_text(
        "".join(json.dumps(row, ensure_ascii=False, sort_keys=True) + "\n" for row in rows)
    )


def digest(value):
    from inferyard.evidence.storage import json_bytes

    return hashlib.sha256(json_bytes(value)).hexdigest()


def matrix(args, source, lane, cases):
    out = args.out / f"lane-{lane}"
    out.mkdir()
    results = []
    for name, expected, mutate, update_seal in cases:
        copy = out / name
        shutil.copytree(source, copy)
        mutate(copy)
        if update_seal and (copy / "manifest.json").exists():
            reseal(copy)
        before = inventory(copy)
        result = invoke(args.candidate, ["verify", "--path", copy], out / f"{name}-cli", expected)
        if inventory(copy) != before:
            raise ValueError("verification_changed_input")
        results.append({"case": name, "expected": expected, "result": result, "files": before})
    dump(out / "rejections.json", results)


def json_corrupt(root):
    path = root / "inputs.json"
    path.write_text(path.read_text().replace("{", '{"schema_version":3,', 1))


def version(root, number):
    for name in ("inputs.json", "manifest.json"):
        change_json(root, name, lambda data: data.update(evidence_layout_version=number))


def token_phase(root):
    def mutate(rows):
        row = next(row for row in rows if row["kind"] == "token_budgets")
        row["payload"]["entries"][0]["phase"] = "formal"
        row["payload_sha256"] = digest(row["payload"])

    change_rows(root, "observations.jsonl", mutate)


def append(root, name, raw):
    with (root / name).open("ab") as stream:
        stream.write(raw)


def truncate(root):
    path = root / "request-bodies.jsonl"
    path.write_bytes(path.read_bytes()[:-4])


def run(args):
    from inferyard.evidence.ledger import read_trial
    from inferyard.evidence.run_projection import read_run_projection
    from inferyard.evidence.trial_evidence import TrialEvidence
    from tests.evidence_layout_helpers import pack_layout2

    original = args.source_run
    declaration = json.loads((original.parent.parent / "receipt.json").read_text())
    if declaration.get("synthetic") is not True or Path(declaration["run"]).resolve() != original:
        raise ValueError("source_must_be_fresh_layout_fixture_worker_output")
    original_before = inventory(original)
    packed = pack_layout2(original, args.out / "synthetic-layout2")
    if len(inventory(packed)) != 8:
        raise ValueError("unexpected_layout_file_count")
    lane1 = args.out / "lane-1"
    lane1.mkdir()
    for name, tree in (("baseline", args.baseline), ("candidate", args.candidate)):
        invoke(tree, ["verify", "--path", original], lane1 / f"{name}-verify")
        report = lane1 / f"{name}-report"
        invoke(tree, ["report", "--runs", original, "--out", report], lane1 / f"{name}-write")
        invoke(tree, ["verify", "--path", report], lane1 / f"{name}-report-verify")
    lane2 = args.out / "lane-2"
    lane2.mkdir()
    invoke(args.candidate, ["verify", "--path", packed], lane2 / "verify")
    invoke(args.baseline, ["verify", "--path", packed], lane2 / "baseline-forward", 4)
    report = lane2 / "report"
    invoke(args.candidate, ["report", "--runs", packed, "--out", report], lane2 / "write")
    invoke(args.candidate, ["verify", "--path", report, "--rerender"], lane2 / "rerender")
    if len(inventory(report)) != 3:
        raise ValueError("unexpected_report_file_count")
    matrix(
        args,
        packed,
        3,
        [
            ("duplicate_key", 4, json_corrupt, True),
            (
                "boolean_integer",
                4,
                lambda r: change_json(
                    r, "inputs.json", lambda d: d.update(evidence_layout_version=True)
                ),
                True,
            ),
            (
                "non_finite",
                4,
                lambda r: append(r, "request-bodies.jsonl", b'{"value":NaN}\n'),
                True,
            ),
            (
                "invalid_phase",
                4,
                lambda r: change_rows(
                    r, "request-bodies.jsonl", lambda rows: rows[0].update(phase="invalid")
                ),
                True,
            ),
        ],
    )
    matrix(
        args,
        packed,
        4,
        [
            ("unknown_version", 2, lambda r: version(r, 999), True),
            (
                "mixed_authority",
                4,
                lambda r: shutil.copyfile(original / "run.json", r / "run.json"),
                False,
            ),
            (
                "version_conflict",
                4,
                lambda r: change_json(
                    r, "manifest.json", lambda d: d.update(evidence_layout_version=1)
                ),
                False,
            ),
        ],
    )
    matrix(
        args,
        packed,
        5,
        [
            (
                "input_member_hash",
                4,
                lambda r: change_json(
                    r,
                    "inputs.json",
                    lambda d: d["documents"]["run"]["payload"].update(run_id="changed"),
                ),
                True,
            ),
            ("budget_phase", 4, token_phase, True),
            (
                "body_hash",
                4,
                lambda r: change_rows(
                    r, "request-bodies.jsonl", lambda rows: rows[0]["body"].update(model="changed")
                ),
                True,
            ),
            (
                "manifest_hash",
                4,
                lambda r: change_json(
                    r, "manifest.json", lambda d: d["files"]["inputs.json"].update(sha256="0" * 64)
                ),
                False,
            ),
        ],
    )

    def unsealed_tail(root):
        (root / "manifest.json").unlink()
        append(root, "request-bodies.jsonl", b'{"incomplete":')

    def missing_body(root):
        change_rows(root, "request-bodies.jsonl", lambda rows: rows.pop(0))

    matrix(
        args,
        packed,
        6,
        [
            ("unsealed_prefix", 3, lambda r: (r / "manifest.json").unlink(), False),
            ("unsealed_tail", 3, unsealed_tail, False),
            (
                "middle_corruption",
                4,
                lambda r: append(r, "request-bodies.jsonl", b"{}\n{}\n"),
                True,
            ),
            ("started_missing_body", 4, missing_body, True),
            ("sealed_tail", 4, truncate, True),
        ],
    )
    lane7 = args.out / "lane-7"
    lane7.mkdir()
    prepared = lane7 / "prepared-not-started"
    shutil.copytree(packed, prepared)

    def add_prepared(rows):
        row = dict(rows[-1])
        ordinal = row["ordinal"] + 1
        key = row["request_id"].rsplit("-", 1)[0] + "-" + str(ordinal)
        row.update(ordinal=ordinal, request_id=key)
        rows.append(row)

    change_rows(prepared, "request-bodies.jsonl", add_prepared)
    reseal(prepared)
    result = invoke(args.candidate, ["verify", "--path", prepared], lane7 / "verify")
    counts = read_trial(prepared)["summary"]["counts"]
    projection_counts = read_run_projection(prepared)["summary"]["counts"]
    if counts != read_trial(packed)["summary"]["counts"] or projection_counts != counts:
        raise ValueError("prepared_changed_execution_counts")
    evidence = TrialEvidence(prepared)
    for kind, stages in (
        ("effective_parameters", ("ordinary", "stream")),
        ("environment", ("start", "end")),
    ):
        if not all(evidence.has_observation(kind, stage) for stage in stages):
            raise ValueError("observation_slot_missing")
    dump(
        lane7 / "projection.json",
        {
            "verified": result,
            "full_counts": counts,
            "projection_counts": projection_counts,
        },
    )
    lane9 = args.out / "lane-9"
    lane9.mkdir()
    for label, source in (("old", original), ("new", packed)):
        for command, options in (
            ("export", []),
            ("rescore", ["--scorer", "phase2.v1", "--reason", "synthetic layout regression"]),
            ("public", []),
        ):
            out = lane9 / f"{label}-{command}"
            argv = ["public", "package"] if command == "public" else [command]
            invoke(
                args.candidate,
                [*argv, "--run", source, "--out", out, *options],
                lane9 / f"{label}-{command}-write",
            )
            invoke(args.candidate, ["verify", "--path", out], lane9 / f"{label}-{command}-verify")
    if inventory(original) != original_before:
        raise ValueError("original_source_changed")
    dump(
        args.out / "receipt.json",
        {
            "synthetic": True,
            "old_files": original_before,
            "new_files": inventory(packed),
            "not_covered": ["lane8_specialized_consumers", "lane9_rerun_resume", "lane10_browser"],
        },
    )


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("baseline", "candidate", "source-run", "out"):
        parser.add_argument("--" + name, type=Path, required=True)
    args = parser.parse_args()
    for name in ("baseline", "candidate", "source_run", "out"):
        setattr(args, name, getattr(args, name).resolve())
    if not args.out.is_relative_to(Path("/tmp").resolve()):
        parser.error("--out must be a new /tmp directory")
    args.out.mkdir(parents=True, exist_ok=False)
    sys.path[:0] = [str(args.candidate / "src"), str(args.candidate)]
    try:
        run(args)
    except Exception as exc:
        dump(args.out / "failure.json", {"type": type(exc).__name__, "reason": str(exc)})
        raise
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
