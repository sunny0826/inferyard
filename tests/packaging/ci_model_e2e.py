"""Full-chain model e2e: run the real CLI against a live Prism llama-server and
assert the workspace layout contract on macOS, Windows and Linux.

The script is the single entry used by both local pre-flight and CI: it never
downloads anything itself (assets arrive as arguments), starts the service the
way the operator guide prescribes (external process, exact frozen arguments),
then drives init -> device-check -> platform preparation -> bind -> run ->
report -> verify for two bundles against the same service: zh-smoke (40
quality-scored instruction/extraction cases) and zh-svg-pelican (the SVG
gallery case). It asserts the two-directory layout (bench-ops preparation,
bench-work holding only the runs and report), verifies the SVG animation
statistics line, verifies the real scoring pipeline end to end (pass/fail
quality states and the dashboard denominator), and finally re-verifies the
report after deleting both bench-ops directories.
"""

import argparse
import json
import platform
import shutil
import subprocess
import sys
import tarfile
import time
import tomllib
import urllib.request
import zipfile
from pathlib import Path

SMOKE_BUNDLE = "zh-smoke"
SVG_BUNDLE = "zh-svg-pelican"
SMOKE_CASES = 40
RUN_FILES = (
    "environment.jsonl",
    "events.jsonl",
    "inputs.json",
    "manifest.json",
    "memory.jsonl",
    "observations.jsonl",
    "request-bodies.jsonl",
    "schedule.jsonl",
)
REPORT_FILES = ("artifact-manifest.json", "index.json", "report.html")
EXPECTED_WORKSPACE_FILES = 2 * len(RUN_FILES) + len(REPORT_FILES)


def cli(args, *, check=True):
    """Run the inferyard CLI through the current interpreter and return parsed stdout."""
    completed = subprocess.run(
        [sys.executable, "-m", "inferyard", *args],
        capture_output=True,
        text=True,
        check=False,
    )
    if check and completed.returncode != 0:
        raise SystemExit(
            f"inferyard {' '.join(args)} failed with {completed.returncode}\n"
            f"stderr: {completed.stderr.strip()}"
        )
    payload = json.loads(completed.stdout) if completed.stdout.strip() else None
    return completed.returncode, payload


def wait_health(url, timeout_seconds=120.0):
    deadline = time.monotonic() + timeout_seconds
    while time.monotonic() < deadline:
        try:
            with urllib.request.urlopen(f"{url}/health", timeout=5) as response:
                if json.load(response).get("status") == "ok":
                    return
        except OSError, ValueError:
            pass
        time.sleep(1.0)
    raise SystemExit(f"service at {url} did not become healthy in {timeout_seconds}s")


def extract_engine(archive, destination):
    destination.mkdir(parents=True, exist_ok=True)
    if archive.suffix == ".zip":
        with zipfile.ZipFile(archive) as bundle:
            bundle.extractall(destination)
    else:
        with tarfile.open(archive, "r:gz") as bundle:
            bundle.extractall(destination)
    servers = sorted(destination.rglob("llama-server*"))
    servers = [path for path in servers if path.is_file() and path.stem == "llama-server"]
    if len(servers) != 1:
        raise SystemExit(f"expected exactly one llama-server binary, found {servers}")
    binary = servers[0].resolve()
    # First execution on macOS can take >10s while the signed binary plus its
    # dylib chain is validated, which would exceed the CLI's version-query
    # timeout; run one warm-up --version outside that timeout budget.
    subprocess.run(
        [str(binary), "--version"],
        cwd=binary.parent,
        capture_output=True,
        timeout=180,
        check=True,
    )
    return binary


def read_candidate(path):
    data = tomllib.loads(path.read_text(encoding="utf-8"))
    return data["engine"]["binary_path"], data["engine"]["startup_args"], data["endpoint"]["url"]


def prepare_runtime(root, preflight, engine_archive, runtime_archive):
    """Windows only: prepare the frozen runtime once, shared by both candidates."""
    recommended = json.loads(preflight.read_text(encoding="utf-8"))["recommendation"]["selected"][
        "mode"
    ]
    if recommended == "cpu":
        profile = "prism-b10743-adfffbe-win-cpu-x64"
    elif recommended == "cuda":
        profile = "prism-b10743-adfffbe-win-cuda-12.4-x64"
        if runtime_archive is None:
            raise SystemExit(
                "preflight recommends cuda; pass --runtime-archive with the "
                "cudart-llama-bin-win-cuda-12.4-x64.zip archive"
            )
    else:
        raise SystemExit(f"unsupported recommended runtime mode: {recommended}")
    assets = root / "assets"
    # The preparation contract requires an existing, writable parent directory;
    # unlike the other platforms nothing else creates root/assets on Windows.
    assets.mkdir(parents=True, exist_ok=True)
    prepare_args = [
        "runtime",
        "prepare",
        "--profile",
        profile,
        "--archive",
        str(engine_archive.resolve()),
        "--out",
        str(assets / "runtime"),
    ]
    if recommended == "cuda":
        prepare_args += ["--runtime-archive", str(runtime_archive.resolve())]
    cli(prepare_args)
    return assets / "runtime" / "runtime-receipt.json"


def fill_linux_template(template, out_dir, *, bundle, assets, results):
    details = json.loads((assets / "assets.json").read_text(encoding="utf-8"))
    startup_args = [
        "-m",
        str(details["model"]["path"]),
        "-ngl",
        "0",
        "-t",
        "4",
        "-tb",
        "4",
        "-c",
        "4096",
        "-np",
        "1",
        "--host",
        "127.0.0.1",
        "--port",
        "48857",
        "--no-webui",
        "--reasoning",
        "off",
        "--jinja",
        "--chat-template-kwargs",
        '{"enable_thinking":false}',
        "--no-cache-prompt",
        "--no-cache-idle-slots",
        "--cache-ram",
        "0",
        "--slots",
    ]
    # The Linux environment admission is strict by design: "unknown" literals
    # never pass (the allowed-unknown exemption covers Windows and Darwin only)
    # and missing sysfs values are blockers, so freeze the observed values the
    # same way the CLI's own snapshot does.
    from inferyard.platforms.identity import environment_snapshot

    observed = environment_snapshot()
    required = {
        "profile": observed.get("profile"),
        "governor": observed.get("governor"),
        "ac_online": observed.get("ac_online"),
    }
    unavailable = sorted(key for key, value in required.items() if value is None)
    if unavailable:
        raise SystemExit(
            f"linux environment fields unavailable: {unavailable}; "
            "this machine does not satisfy the strict Linux environment admission"
        )
    epp = observed.get("epp")
    replacements = {
        "REPLACE_DEVICE_ID": "ci-linux-x64",
        "REPLACE_MODEL_NAME": "SmolLM2 135M Instruct",
        "REPLACE_MODEL_REVISION": details["model"]["sha256"],
        "REPLACE_MODEL_PATH": str(details["model"]["path"]),
        "REPLACE_MODEL_SHA256": details["model"]["sha256"],
        "REPLACE_ASSET_DIR": assets.name,
        "REPLACE_TEMPLATE_SHA256": details["template"]["sha256"],
        "REPLACE_ENGINE_PATH": str(details["engine"]["path"]),
        "REPLACE_ENGINE_SHA256": details["engine"]["sha256"],
        '"REPLACE_EXACT_STARTUP_ARGUMENTS"': ", ".join(json.dumps(item) for item in startup_args),
        "REPLACE_SELECTED_BUNDLE": bundle,
        "REPLACE_BUNDLE_VERSION": json.loads(
            (out_dir / "bundles" / f"{bundle}.json").read_text(encoding="utf-8")
        )["version"],
        "REPLACE_ACTUAL_BACKGROUND_LOAD": "ci disposable runner",
        "\nthreads = 1": "\nthreads = 4",
        "\nthreads_batch = 1": "\nthreads_batch = 4",
        '"../results"': json.dumps(str(results)),
        '"REPLACE_MEMORY_BUDGET_BYTES"': "1073741824",
        '"REPLACE_DISK_BUDGET_BYTES"': "5368709120",
        'profile = "unknown"': f'profile = "{required["profile"]}"',
        'governor = "unknown"': f'governor = "{required["governor"]}"',
        'epp = "unknown"': (
            # TOML has no null literal; keep the string and skip the match.
            f'epp = "{epp}"' if epp is not None else 'epp = "unknown"\nrequire_epp_match = false'
        ),
        "\nac_online = true": f"\nac_online = {str(required['ac_online']).lower()}",
        # seed 42 makes this model's CPU-quantized Linux path emit output with
        # an invalid UTF-8 byte that the engine's peg-native chat parser
        # rejects with HTTP 500; verified seeds 1/7/123/2024 all return 200.
        "\nseed = 42": "\nseed = 1",
        # The lab contract freezes seed only when the config declares
        # supported; macOS/Windows candidates write this explicitly.
        'seed_support = "unknown"': 'seed_support = "supported"',
    }
    text = template.read_text(encoding="utf-8")
    for old, new in replacements.items():
        if old not in text:
            raise SystemExit(f"linux template lost placeholder {old}")
        text = text.replace(old, new)
    candidate_dir = out_dir / "candidate"
    candidate_dir.mkdir(parents=True)
    path = candidate_dir / "candidate.toml"
    path.write_text(text, encoding="utf-8")
    return path


def prepare_candidate(root, bench_ops, bundle, *, preflight, model, engine_binary):
    bundle_path = bench_ops / "bundles" / f"{bundle}.json"
    results = root / "bench-work" / "results"
    system = platform.system()
    out = bench_ops / "candidate"
    if system == "Darwin":
        cli(
            [
                "config",
                "create",
                "--preflight",
                str(preflight),
                "--engine",
                str(engine_binary),
                "--bundle",
                str(bundle_path),
                "--results",
                str(results),
                "--out",
                str(out),
            ]
        )
        return out / "candidate.toml"
    if system == "Windows":
        # The CLI requires the runtime profile to match the preflight's
        # recommended mode; a CUDA machine must not fall back to CPU.
        cli(
            [
                "config",
                "create",
                "--preflight",
                str(preflight),
                "--runtime-receipt",
                str(root / "assets" / "runtime" / "runtime-receipt.json"),
                "--bundle",
                str(bundle_path),
                "--results",
                str(results),
                "--out",
                str(out),
            ]
        )
        return out / "candidate.toml"
    if system == "Linux":
        assets = bench_ops / "assets" / "local-model"
        cli(
            [
                "config",
                "assets",
                "--model",
                str(model),
                "--engine",
                str(engine_binary),
                "--out",
                str(assets),
            ]
        )
        return fill_linux_template(
            bench_ops / "configs" / "linux.example.toml",
            bench_ops,
            bundle=bundle,
            assets=assets,
            results=results,
        )
    raise SystemExit(f"unsupported platform {system}")


def assert_workspace(bench_work, run_dirs):
    files = sorted(
        path.relative_to(bench_work).as_posix() for path in bench_work.rglob("*") if path.is_file()
    )
    expected = sorted(
        [f"results/{run_dir.name}/{name}" for run_dir in run_dirs for name in RUN_FILES]
        + [f"report/{name}" for name in REPORT_FILES]
    )
    if files != expected:
        raise SystemExit(
            "workspace layout mismatch\n"
            f"expected {EXPECTED_WORKSPACE_FILES} files: {expected}\nfound: {files}"
        )


def assert_scoring(report):
    """Prove the real scoring pipeline ran on real model output, not just wiring."""
    index = json.loads((report / "index.json").read_text(encoding="utf-8"))
    smoke = next(run for run in index["runs"] if len(run["requests"]) == SMOKE_CASES)
    svg = next(run for run in index["runs"] if len(run["requests"]) == 1)
    quality = smoke["dashboard"]["quality"]
    if quality["denominator"] != SMOKE_CASES or quality["complete"] is not True:
        raise SystemExit(f"smoke quality denominator incomplete: {quality}")
    states = [row["quality"] for row in smoke["requests"]]
    if any(state not in ("pass", "fail") for state in states):
        raise SystemExit(f"smoke cases were not quality-scored: {sorted(set(states))}")
    if any(row["state"] != "completed" for row in smoke["requests"]):
        raise SystemExit("smoke run contains requests that did not complete")
    if quality["numerator"] != states.count("pass"):
        raise SystemExit(
            f"dashboard numerator {quality['numerator']} != pass count {states.count('pass')}"
        )
    if svg["requests"][0]["quality"] != "not_applicable" or len(svg["svg_gallery"]) != 1:
        raise SystemExit("svg run lost its not_applicable score or gallery entry")
    if index.get("comparison") is None:
        raise SystemExit("two-run report is missing the side-by-side comparison")
    html = (report / "report.html").read_text(encoding="utf-8")
    for marker in ("答案通过数", "动画元素统计"):
        if marker not in html:
            raise SystemExit(f"report.html is missing the {marker} line")
    return {
        "pass": states.count("pass"),
        "fail": states.count("fail"),
        "denominator": quality["denominator"],
    }


def run_leg(bench_ops, candidate, *, pid, url):
    """Bind the shared live service and execute one bundle end to end."""
    _, bound = cli(
        [
            "config",
            "bind",
            "--candidate",
            str(candidate),
            "--pid",
            str(pid),
            "--endpoint",
            url,
            "--out",
            str(bench_ops / "bound"),
        ]
    )
    if bound["status"] != "prepared":
        raise SystemExit(f"bind failed: {bound['status']}")
    _, run = cli(["run", "--config", str(bench_ops / "bound" / "config.toml")])
    if run["status"] != "finished" or run["completeness"] != "complete":
        raise SystemExit(f"run failed: {run['status']} / {run['completeness']}")
    return Path(run["evidence_dir"])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--model", type=Path, required=True)
    parser.add_argument("--engine-archive", type=Path, required=True)
    parser.add_argument(
        "--runtime-archive",
        type=Path,
        help="cudart archive required when the Windows preflight recommends cuda",
    )
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()

    root = args.out.resolve()
    if root.exists():
        raise SystemExit(f"--out must not exist: {root}")
    root.mkdir(parents=True)
    bench_ops_smoke = root / "bench-ops-smoke"
    bench_ops_svg = root / "bench-ops-svg"
    bench_work = root / "bench-work"

    _, smoke_init = cli(["init", "--out", str(bench_ops_smoke), "--bundle", SMOKE_BUNDLE])
    _, _ = cli(["init", "--out", str(bench_ops_svg), "--bundle", SVG_BUNDLE])
    preflight = bench_ops_smoke / "preflight" / "device-preflight.json"
    _, _ = cli(
        [
            "device-check",
            "--model",
            str(args.model),
            "--out",
            str(bench_ops_smoke / "preflight"),
            "--json",
        ]
    )
    if smoke_init["status"] != "prepared":
        raise SystemExit(f"init failed: {smoke_init['status']}")

    engine_binary = (
        extract_engine(args.engine_archive, root / "assets" / "engine")
        if platform.system() != "Windows"
        else None
    )
    if platform.system() == "Windows":
        prepare_runtime(root, preflight, args.engine_archive, args.runtime_archive)
    smoke_candidate = prepare_candidate(
        root,
        bench_ops_smoke,
        SMOKE_BUNDLE,
        preflight=preflight,
        model=args.model,
        engine_binary=engine_binary,
    )
    svg_candidate = prepare_candidate(
        root,
        bench_ops_svg,
        SVG_BUNDLE,
        preflight=preflight,
        model=args.model,
        engine_binary=engine_binary,
    )
    identity = read_candidate(smoke_candidate)
    if identity != read_candidate(svg_candidate):
        raise SystemExit("candidates disagree on the frozen service identity")

    binary, startup_args, url = identity
    server_log = (root / "server.log").open("w", encoding="utf-8")
    try:
        service = subprocess.Popen(
            [binary, *startup_args], stdout=server_log, stderr=subprocess.STDOUT
        )
        try:
            wait_health(url)
            smoke_dir = run_leg(bench_ops_smoke, smoke_candidate, pid=service.pid, url=url)
            svg_dir = run_leg(bench_ops_svg, svg_candidate, pid=service.pid, url=url)
            cli(
                [
                    "report",
                    "--runs",
                    str(smoke_dir),
                    str(svg_dir),
                    "--out",
                    str(bench_work / "report"),
                ]
            )
            _, verified = cli(["verify", "--path", str(bench_work / "report"), "--rerender"])
            if verified["details"].get("verified") is not True:
                raise SystemExit(f"verify failed: {verified}")
            assert_workspace(bench_work, [smoke_dir, svg_dir])
            quality = assert_scoring(bench_work / "report")
        finally:
            service.terminate()
            try:
                service.wait(timeout=30)
            except subprocess.TimeoutExpired:
                service.kill()
    finally:
        server_log.close()

    shutil.rmtree(bench_ops_smoke)
    shutil.rmtree(bench_ops_svg)
    code, after = cli(["verify", "--path", str(bench_work / "report")], check=False)
    if code != 0 or after["details"].get("verified") is not True:
        raise SystemExit(f"verify after bench-ops cleanup failed: {after}")

    summary = {
        "platform": f"{platform.system()}-{platform.machine()}",
        "smoke_run_dir": smoke_dir.name,
        "svg_run_dir": svg_dir.name,
        "smoke_quality": quality,
        "workspace_files": EXPECTED_WORKSPACE_FILES,
        "verified_before_cleanup": True,
        "verified_after_cleanup": True,
        "animation_stats_line": True,
        "bench_ops_deleted": True,
    }
    print(json.dumps(summary, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main())
