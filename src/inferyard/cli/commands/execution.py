"""Argument registration for execution commands."""

from pathlib import Path


def add_commands(commands):
    probe = commands.add_parser("probe", help="probe the service and save diagnostic evidence")
    probe.add_argument("--config", required=True, type=Path)
    run = commands.add_parser("run", help="execute a serial measurement")
    origin = run.add_mutually_exclusive_group(required=True)
    origin.add_argument("--config", type=Path)
    origin.add_argument("--rerun-from", dest="from_run", type=Path)
    origin.add_argument("--plan", dest="frozen_plan", type=Path)
    run.add_argument("--workload", dest="workload_id")
    run.add_argument("--handoff-note")
    run.add_argument("--diagnostic", action="store_true")
    run.add_argument("--endpoint-url")
    run.add_argument("--server-pid", type=int)
    run.add_argument("--output-root", type=Path)
    run.add_argument("--api-key-env")
    resume = commands.add_parser("resume", help="execute only unexecuted cases from a saved run")
    resume.add_argument("--from-run", type=Path, required=True)
    resume.add_argument("--endpoint-url")
    resume.add_argument("--server-pid", type=int)
    resume.add_argument("--api-key-env")
    resume.add_argument("--handoff-note")
    for command in (probe, run, resume):
        command.add_argument("--recovery-confirm")
        command.add_argument("--recovery-note")
