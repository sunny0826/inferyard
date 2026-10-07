"""Register read-only artifact verification without importing its validators."""

from pathlib import Path


def add_commands(commands):
    verify = commands.add_parser("verify", help="verify an existing offline artifact")
    verify.add_argument("--path", dest="run", type=Path, required=True)
    verify.add_argument("--source-run", dest="from_run", type=Path)
    verify.add_argument("--config", type=Path)
    verify.add_argument("--target-run", type=Path)
    verify.add_argument("--source-root", dest="source_roots", action="append", metavar="OLD=NEW")
    verify.add_argument("--rerender", action="store_true")
