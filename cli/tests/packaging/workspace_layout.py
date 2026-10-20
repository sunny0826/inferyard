import platform

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
RESOURCE_FILES = ("external-cpu.jsonl", "request-environment.jsonl")
REPORT_FILES = ("artifact-manifest.json", "index.json", "report.html")


def assert_workspace(bench_work, run_dirs):
    resource_files = {
        "Darwin": RESOURCE_FILES,
        "Linux": RESOURCE_FILES,
        "Windows": (),
    }[platform.system()]
    run_files = RUN_FILES + resource_files
    files = sorted(
        path.relative_to(bench_work).as_posix() for path in bench_work.rglob("*") if path.is_file()
    )
    expected = sorted(
        [f"results/{run_dir.name}/{name}" for run_dir in run_dirs for name in run_files]
        + [f"report/{name}" for name in REPORT_FILES]
    )
    if files != expected:
        raise SystemExit(
            f"workspace layout mismatch\nexpected {len(expected)} files: {expected}\nfound: {files}"
        )
    return len(expected)
