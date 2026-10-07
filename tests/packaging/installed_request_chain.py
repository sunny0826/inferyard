"""Disposable CI: installed CLI + real host lock + injected synthetic transport/identity.

This checks packaging of execution/dirty paths, not real engine or hardware admission.
Reuse the established repository scenario; do not change product lock paths or safety rules.
"""

import argparse
import asyncio
import contextlib
import io
import json
import os
import sys
from dataclasses import replace
from pathlib import Path


def main():
    if not (
        os.environ.get("GITHUB_ACTIONS") == "true"
        and os.environ.get("RUNNER_ENVIRONMENT") == "github-hosted"
        and os.environ.get("LAB_DISPOSABLE_HOST_TEST") == "yes"
    ):
        raise RuntimeError("requires_explicit_disposable_github_hosted_runner")
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--fixtures", type=Path, required=True)
    parser.add_argument("--mode", choices=("success", "dirty"), required=True)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    import pytest

    import inferyard
    import inferyard.runtime.lock as locking
    from inferyard.cli import main as cli_main
    from inferyard.evidence.ledger import read_trial
    from inferyard.runtime.runner import execute_async

    assert "site-packages" in Path(inferyard.__file__).parts
    fixed = {name: getattr(locking, name) for name in ("LOCK_PATH", "STATE_PATH", "LEGACY_ROOT")}
    sys.path.insert(0, str(args.fixtures / "ci-tests"))
    from tests.integration.test_runner import scenario

    work = args.out.with_suffix("")
    work.mkdir()
    config = args.fixtures / "ci-tests/tests/fixtures/config/valid.toml"
    with pytest.MonkeyPatch.context() as patch:
        request, deps, calls, _settings = scenario.__wrapped__(work, patch, config)
        for name, value in fixed.items():
            patch.setattr(locking, name, value)
        capture = io.StringIO()
        with contextlib.redirect_stdout(capture):
            code = cli_main(
                ["run", "--config", str(config)],
                handlers={
                    "run": lambda parsed: asyncio.run(
                        execute_async(replace(parsed, config=request.config), deps)
                    )
                },
            )
        result = json.loads(capture.getvalue())
        if args.mode == "success":
            assert code == 0 and len(calls) == 8, result
            run = read_trial(Path(result["evidence_dir"]))
            assert run["summary"]["counts"]["planned"] == 3
            with locking.HostLock() as lock:
                assert lock.state["dirty"] is False
        else:
            assert code == 2 and calls == [], result
            assert "dirty_service_requires_bound_recovery" in result["limitations"]
            with locking.HostLock() as lock:
                assert lock.state["dirty"] is True
    with args.out.open("x", encoding="utf-8") as stream:
        json.dump(
            {
                "kind": "installed_request_chain.v1",
                "synthetic": True,
                "mode": args.mode,
                "injected_identity_transport": True,
                "fixed_host_paths": True,
                "code": code,
                "synthetic_requests": len(calls),
                "real_model_requests_sent": 0,
                "result": result,
            },
            stream,
            indent=2,
        )


if __name__ == "__main__":
    main()
