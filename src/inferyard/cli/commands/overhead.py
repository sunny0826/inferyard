"""Argument registration for overhead commands."""

from pathlib import Path


def add_commands(commands):
    overhead = commands.add_parser(
        "overhead", help="run a frozen diagnostic ABBA collector preflight"
    )
    overhead.add_argument("--plan", dest="frozen_plan", type=Path, required=True)
    overhead.add_argument("--trial", dest="trial_id", required=True)
    overhead.add_argument("--tolerance-ratio", type=float, required=True)
    overhead.add_argument("--first-event-tolerance-ratio", type=float)
    overhead.add_argument("--engine-rate-tolerance-ratio", type=float)
    overhead.add_argument("--block-gap-tolerance-ms", type=float)
    overhead.add_argument("--max-wall-seconds", type=float, required=True)
    overhead.add_argument("--output-root", type=Path, required=True)
    observer_mode = overhead.add_mutually_exclusive_group()
    observer_mode.add_argument(
        "--common-observer",
        action="store_true",
        help="keep environment observer in both arms; measures incremental resource cost only",
    )
    observer_mode.add_argument(
        "--boundary-observer",
        action="store_true",
        help="use outside-request guards in both arms; disable all in-request sampling in off arms",
    )
