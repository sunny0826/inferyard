"""Bounded synthetic CLI runs for layout checks; owns only a fresh temporary directory."""

import argparse
import hashlib
import json
import resource
import sys
import time
from dataclasses import replace
from pathlib import Path


def dump(path, value):
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2, allow_nan=False))


def inventory(root):
    return {
        path.relative_to(root).as_posix(): {
            "sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
            "bytes": path.stat().st_size,
        }
        for path in sorted(root.rglob("*"))
        if path.is_file()
    }


def run(args):
    import httpx
    import pytest

    import inferyard.cli as cli
    import inferyard.runtime.lock as locking
    import inferyard.runtime.runner as running
    from inferyard.analysis.scoring import score_case
    from inferyard.contracts.validation import Document
    from inferyard.evidence.ledger import read_trial
    from inferyard.registry import adapter_factory
    from tests.integration.test_lab_cli import write_config
    from tests.integration.test_runner import scenario
    from tests.lab_service import LabService, lab_config
    from tests.native_service import NativeService

    with pytest.MonkeyPatch.context() as patch:
        request, deps, calls, settings = scenario.__wrapped__(
            args.out, patch, args.tree / "tests/fixtures/config/valid.toml"
        )
        settings["stream_delay"] = 0.002
        deps.scorer = score_case
        config = request.config.config.to_dict()
        bundle = request.config.bundle.to_dict()
        if args.scope != "synthetic":
            bundle = json.loads((args.tree / "bundles" / f"{args.scope}.json").read_text())
        if args.scope == "zh-svg-pelican":
            settings["answer"] = (
                '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">'
                '<path d="M20 50 Q50 10 80 50 L55 70 Z" fill="orange"/></svg>'
            )
        service = None
        if args.adapter != "prism":
            config = lab_config(config, args.adapter)
            if args.mode == "native":
                config["engine"]["observation_mode"] = "auto"
                service = NativeService(config)
            else:
                service = LabService(config)
            deps.adapter = lambda origin, secret: adapter_factory(args.adapter)(
                origin, secret=secret, transport=httpx.MockTransport(service)
            )
        config["bundle"]["path"] = str(args.out / "bundle.json")
        config["bundle"]["version"] = bundle["version"]
        config["engine"]["startup_args"] += ["-m", config["model"]["local_path"]]
        request = replace(
            request,
            config=replace(
                request.config,
                config=Document.parse("config", config),
                bundle=Document.parse("bundle", bundle),
            ),
        )
        path = args.out / "synthetic.toml"
        write_config(request, path)
        patch.setattr(running, "Dependencies", lambda **options: deps)
        start = time.perf_counter_ns()
        code = cli.main(["run", "--config", str(path)])
        elapsed = (time.perf_counter_ns() - start) / 1e9
        dump(args.out / "cli-exit.json", {"exit_code": code, "run_wall_seconds": elapsed})
        if code != 0:
            raise ValueError(f"synthetic_cli_failed:{code}")
        roots = sorted((args.out / "runs").iterdir())
        if len(roots) != 1:
            raise ValueError("fixture_run_count_mismatch")
        root = roots[0]
        data = read_trial(root)
        sent = service.requests if service is not None else calls
        expected = len(bundle["cases"]) + config["execution"].get("warmup_count", 3) + 2
        if code != 0 or len(sent) != expected:
            raise ValueError(f"synthetic_run_failed:{code}:{len(sent)}:{expected}")
        before = inventory(root)
        packed = None
        if args.pack_layout2:
            from tests.evidence_layout_helpers import pack_layout2

            packed = pack_layout2(root, args.out / "layout2")
            if inventory(root) != before:
                raise ValueError("fixture_source_changed")
        dump(
            args.out / "receipt.json",
            {
                "synthetic": True,
                "transport": "httpx.MockTransport; no TCP or model service",
                "preflight": "injected fixture; not native identity acceptance",
                "tree": str(args.tree),
                "python": sys.executable,
                "scope": args.scope,
                "fixed_response": settings.get("answer"),
                "adapter": args.adapter,
                "mode": args.mode,
                "run": str(root),
                "packed": str(packed) if packed else None,
                "exit_code": code,
                "requests_received": len(sent),
                "expected_requests": expected,
                "request_body_hashes": [
                    hashlib.sha256(
                        json.dumps(body, ensure_ascii=False, sort_keys=True).encode()
                    ).hexdigest()
                    for body in sent
                ],
                "counts": data["summary"]["counts"],
                "host_state": json.loads(locking.STATE_PATH.read_text()),
                "files": before,
                "packed_files": inventory(packed) if packed else None,
                "run_wall_seconds": elapsed,
                "timing_boundary": "immediately before CLI main through its complete return",
                "peak_rss_bytes": resource.getrusage(resource.RUSAGE_SELF).ru_maxrss
                * (1 if sys.platform == "darwin" else 1024),
                "rss_scope": "whole worker including fixture setup and readback",
            },
        )
    return code


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tree", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument(
        "--scope", choices=("synthetic", "zh-core", "zh-svg-pelican"), default="synthetic"
    )
    parser.add_argument("--adapter", choices=("prism", "kvmem", "ninfer"), default="prism")
    parser.add_argument("--mode", choices=("lab", "native"), default="lab")
    parser.add_argument("--pack-layout2", action="store_true")
    args = parser.parse_args()
    args.tree, args.out = args.tree.resolve(), args.out.resolve()
    if not args.out.is_relative_to(Path("/tmp").resolve()):
        parser.error("--out must be a fresh /tmp directory")
    if args.adapter == "prism" and args.mode == "native":
        parser.error("native fixture requires kvmem or ninfer")
    args.out.mkdir(parents=True, exist_ok=False)
    sys.path[:0] = [str(args.tree / "src"), str(args.tree)]
    import inferyard

    if not Path(inferyard.__file__).resolve().is_relative_to(args.tree / "src"):
        raise ValueError("wrong_source_tree_imported")
    try:
        return run(args)
    except Exception as exc:
        dump(args.out / "failure.json", {"type": type(exc).__name__, "reason": str(exc)})
        raise


if __name__ == "__main__":
    raise SystemExit(main())
