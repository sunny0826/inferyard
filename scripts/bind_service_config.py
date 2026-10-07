"""Bind a candidate configuration to an already running, externally owned service."""

import argparse
import json
import os
import platform
import re
from pathlib import Path

from inferyard.platforms.identity import process_start_ticks


def toml_key(key):
    return key if re.fullmatch(r"[A-Za-z0-9_-]+", key) else toml_value(key)


def toml_value(value):
    if isinstance(value, dict):
        return "{" + ", ".join(f"{toml_key(k)} = {toml_value(v)}" for k, v in value.items()) + "}"
    if isinstance(value, list):
        return "[" + ", ".join(toml_value(item) for item in value) + "]"
    return json.dumps(value, ensure_ascii=False, allow_nan=False).replace("\x7f", r"\u007f")


def table_lines(path, values):
    lines = ["", "[" + ".".join(toml_key(key) for key in path) + "]"]
    lines.extend(
        f"{toml_key(key)} = {toml_value(value)}"
        for key, value in values.items()
        if not isinstance(value, dict)
    )
    for key, value in values.items():
        if isinstance(value, dict):
            lines.extend(table_lines((*path, key), value))
    return lines


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--candidate", type=Path, required=True)
    parser.add_argument("--pid", type=int, required=True)
    parser.add_argument("--endpoint", required=True)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    from inferyard.config.preparation_io import PreparationError
    from inferyard.config.service_binding import bind
    from inferyard.config.toml_writer import render

    def observed_arguments(pid):
        if os.name == "nt":
            from inferyard.platforms.windows_identity import process_arguments

            return process_arguments(pid)
        if platform.system() == "Darwin":
            from inferyard.platforms.macos_identity import process_arguments

            return process_arguments(pid)
        return (Path("/proc") / str(pid) / "cmdline").read_bytes().decode().rstrip("\0").split("\0")

    try:
        config, start = bind(
            args.candidate,
            args.pid,
            args.endpoint,
            ticks=process_start_ticks,
            argv=observed_arguments,
        )
    except PreparationError as exc:
        messages = {
            "service_identity_changed": "service identity changed",
            "startup_arguments_mismatch": "startup arguments differ from candidate",
        }
        raise SystemExit(messages.get(exc.reason, exc.reason)) from None
    content = render(
        config,
        comment="Bound to an external service. Run performs current probes.",
        writer=table_lines,
    )
    # Exclusive creation: never overwrite a previously frozen config.
    with args.out.open("x", encoding="utf-8", newline="\n") as stream:
        stream.write(content)
    print(json.dumps({"config": str(args.out), "pid": args.pid, "start_ticks": start}))


if __name__ == "__main__":
    main()
