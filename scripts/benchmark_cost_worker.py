"""Isolated software-cost workloads; imports only the explicitly selected source tree."""

import argparse
import hashlib
import json
import resource
import sys
import time
from pathlib import Path


def dump(path, value):
    path.write_text(json.dumps(value, ensure_ascii=False, sort_keys=True, allow_nan=False))


def prepare(out, cases=24, samples=32, svg_chars=65536, svg_cases=4):
    from types import SimpleNamespace
    from unittest.mock import patch

    from inferyard.evidence.journal import TrialJournal
    from tests import helpers

    original = helpers.load_config(Path(helpers.__file__).parent / "fixtures/config/valid.toml")
    bundle = original.bundle.to_dict()
    bundle["cases"][0].update(
        category="svg",
        rules={},
        reference_answer="<svg><text>" + "鹈" * svg_chars + "</text></svg>",
    )

    class DenseJournal(TrialJournal):
        def __init__(self, *args, **kwargs):
            super().__init__(*args, **kwargs, diagnostic=True)

        def sample(self, value):
            for offset in range(samples):
                super().sample(
                    {
                        **value,
                        "read_started_ns": value["read_started_ns"] + offset * 1000,
                        "read_finished_ns": value["read_finished_ns"] + offset * 1000,
                    }
                )

    runs = []
    fixed_reference = original.bundle.to_dict()["cases"][0]["reference_answer"]
    extra_cases = 2
    with patch.object(helpers, "TrialJournal", DenseJournal):
        runs.append(helpers.fixture_run(out / "fixed", states=["completed"] * cases))
        loaded = SimpleNamespace(
            config=original.config, bundle=SimpleNamespace(to_dict=lambda: bundle)
        )
        with patch.object(helpers, "load_config", return_value=loaded):
            runs.append(
                helpers.fixture_run(
                    out / "svg", states=["completed"] * svg_cases, passes=range(svg_cases)
                )
            )
        runs.append(helpers.fixture_run(out / "fixed-extra", states=["completed"] * extra_cases))
    # Original journal hashes are retained. These are diagnostic synthetic records,
    # never real-device or human-reviewed benchmark evidence.
    dump(
        out / "corpus.json",
        {
            "synthetic": True,
            "model_requests_sent": 0,
            "runs": [str(p) for p in runs],
            "comparison_run_indices": [0, 1],
            "run_count": len(runs),
            "cases": cases + svg_cases + extra_cases,
            "samples": (cases + svg_cases + extra_cases) * 2 * samples,
            "svg_gallery_count": svg_cases,
            "svg_characters_per_answer": svg_chars + 24,
            "answer_sha256": [
                [hashlib.sha256(answer.encode()).hexdigest() for answer in answers]
                for answers in (
                    [fixed_reference] * 2
                    + ["<img src=x onerror=alert(1)> <script>alert(2)</script> {{7*7}}"]
                    * (cases - 2),
                    [bundle["cases"][0]["reference_answer"]] * svg_cases,
                    [fixed_reference] * extra_cases,
                )
            ],
        },
    )
    return 0


def workload(operation, corpus, out):
    from inferyard.analysis.resource_metrics import reduce_resources
    from inferyard.cli import main
    from inferyard.contracts.validation import strict_json_loads
    from inferyard.evidence.ledger import read_trial
    from inferyard.evidence.storage import Redactor, json_bytes
    from inferyard.reporting.report import write_report

    if operation == "reject":
        return main(["verify", "--path", str(corpus)])
    roots = [Path(p) for p in json.loads((corpus / "corpus.json").read_text())["runs"]]
    # Setup for microbenchmarks is explicitly outside their operation timer.
    loaded = (
        [read_trial(p) for p in roots]
        if operation in ("resources", "redact", "redact_empty", "json")
        else None
    )
    payload = {"trials": loaded, "credential_probe": "synthetic-secret"} if loaded else None
    encoded = json_bytes(payload).decode() if operation == "json" else None
    stream_text = "鹈" * 65536 + "synthetic-secret" + "尾" * 4096
    start = time.perf_counter_ns()
    code = 0
    if operation == "read_trial":
        value = [read_trial(p) for p in roots]
    elif operation == "report":
        value = write_report(roots, out / "report")
    elif operation in ("verify", "rerender", "verify_multirun", "rerender_multirun"):
        code = main(
            [
                "verify",
                "--path",
                str(
                    corpus
                    / (
                        "reference-multirun-report"
                        if operation.endswith("_multirun")
                        else "reference-report"
                    )
                ),
                *(["--rerender"] if operation.startswith("rerender") else []),
            ]
        )
        value = {"exit_code": code}
    elif operation == "resources":
        for _ in range(10):
            value = [reduce_resources(d["requests"], d["samples"], d["config"]) for d in loaded]
    elif operation in ("redact", "redact_empty"):
        secrets = ("synthetic-secret",) if operation == "redact" else ()
        for _ in range(10):
            value = Redactor(secrets).clean(payload)
    elif operation == "json":
        for _ in range(10):
            value = strict_json_loads(json_bytes(strict_json_loads(encoded)).decode())
    elif operation in ("redact_stream", "redact_stream_empty"):
        secrets = ("synthetic-secret",) if operation == "redact_stream" else ()
        for _ in range(10):
            stream = Redactor(secrets).stream()
            value = "".join(
                stream.feed(stream_text[i : i + 127]) for i in range(0, len(stream_text), 127)
            )
            value += stream.feed("", final=True)
        expected = Redactor(secrets).text(stream_text)
        if value != expected:
            raise ValueError("stream_redaction_content_loss")
    else:
        raise ValueError(operation)
    elapsed = (time.perf_counter_ns() - start) / 1e9
    if operation == "redact" and value["credential_probe"] == "synthetic-secret":
        raise ValueError("redaction_did_not_remove_secret")
    if operation == "redact_empty" and json_bytes(value) != json_bytes(payload):
        raise ValueError("empty_redaction_content_loss")
    dump(out / "result.json", value)
    dump(
        out / "timing.json",
        {
            "operation_wall_seconds": elapsed,
            "iterations": 10 if loaded or operation.startswith("redact_stream") else 1,
            "boundary": "after imports/setup through complete API/CLI return; excludes result dump",
            "peak_rss_bytes": resource.getrusage(resource.RUSAGE_SELF).ru_maxrss
            * (1 if sys.platform == "darwin" else 1024),
            "rss_scope": "whole isolated worker including imports/setup/output serialization",
        },
    )
    return code


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tree", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--corpus", type=Path)
    parser.add_argument("operation")
    args = parser.parse_args()
    tree = args.tree.resolve()
    sys.path[:0] = [str(tree / "src"), str(tree)]
    import inferyard

    if not Path(inferyard.__file__).resolve().is_relative_to(tree / "src"):
        raise RuntimeError("wrong_source_tree_imported")
    args.out.mkdir(parents=True, exist_ok=False)
    from inferyard.provenance import tool_source_hash

    dump(
        args.out / "identity.json",
        {
            "module": inferyard.__file__,
            "source_sha256": tool_source_hash(),
            "python": sys.executable,
            "version": sys.version,
            "synthetic": True,
        },
    )
    if args.operation == "prepare":
        code = prepare(args.out)
        from inferyard.reporting.report import write_report

        declaration = json.loads((args.out / "corpus.json").read_text())
        roots = [Path(p) for p in declaration["runs"]]
        write_report(
            [roots[i] for i in declaration["comparison_run_indices"]], args.out / "reference-report"
        )
        write_report(roots, args.out / "reference-multirun-report")
        return code
    return workload(args.operation, args.corpus, args.out)


if __name__ == "__main__":
    raise SystemExit(main())
