"""JSON output, exit codes and credential-safe error mapping for CLI requests."""

import argparse
import json
import sys
from collections.abc import Callable, Mapping, Sequence

from inferyard.application.types import CommandRequest, CommandResult, Handler
from inferyard.cli.arguments import ArgumentError, Parser
from inferyard.cli.device_output import format_result, presentation_context
from inferyard.cli.metadata import versions
from inferyard.contracts.schemas import export_schema
from inferyard.contracts.validation import ContractError
from inferyard.contracts.validation_cache import command_validation
from inferyard.evidence.error_reasons import safe_reason
from inferyard.evidence.formats import UnsupportedFormat
from inferyard.evidence.storage import EvidenceError
from inferyard.platforms.identity import PreflightError


def run(
    argv: Sequence[str] | None,
    *,
    handlers: Mapping[str, Handler] | None,
    parser_factory: Callable[[], Parser],
    request_factory: Callable[[argparse.Namespace], CommandRequest],
    backend: Handler,
) -> int:
    argv = list(sys.argv[1:] if argv is None else argv)
    command, output_format = presentation_context(argv)
    try:
        args = parser_factory().parse_args(argv)
        command = args.command or "unknown"
        output_format = getattr(args, "device_format", "json")
        if args.versions or args.schema:
            if args.command:
                raise ArgumentError("metadata options cannot be combined with a command")
            payload = versions() if args.versions else export_schema(args.schema)
            print(json.dumps(payload, ensure_ascii=False, allow_nan=False))
            return 0
        if args.command is None:
            raise ArgumentError("a command is required; use --help")
        with command_validation(enabled=command in ("run", "resume", "plan", "probe")):
            request = request_factory(args)
            handler = (handlers or {}).get(request.command, backend)
            code, result = handler(request)
        if code not in (0, 2, 3, 4, 130) or result.command != request.command:
            raise RuntimeError("invalid backend result")
    except UnsupportedFormat as exc:
        print("unsupported_format; use the original project for historical data", file=sys.stderr)
        code, result = (
            2,
            CommandResult(
                command,
                "blocked",
                limitations=("unsupported_format",),
                details={
                    "artifact": exc.artifact,
                    "saved_version": exc.saved,
                    "supported_versions": exc.supported,
                },
            ),
        )
    except (ContractError, ArgumentError) as exc:
        print(str(exc), file=sys.stderr)
        code, result = 2, CommandResult(command, "blocked", limitations=("invalid_input",))
    except PreflightError as exc:
        if str(exc) == "windows_phase2_live_not_supported":
            print("Windows live experiments are not supported yet", file=sys.stderr)
            limitation = "windows_phase2_live_not_supported"
        else:
            print(
                "preflight blocked; verify configuration, identity and service readiness",
                file=sys.stderr,
            )
            limitation = safe_reason(exc, "preflight_blocked")
        code, result = 2, CommandResult(command, "blocked", limitations=(limitation,))
    except EvidenceError as exc:
        print("evidence integrity or storage error", file=sys.stderr)
        code, result = (
            4,
            CommandResult(command, "error", limitations=(safe_reason(exc, "evidence_error"),)),
        )
    except OSError:
        print("local input/output error; check file accessibility", file=sys.stderr)
        code, result = 4, CommandResult(command, "error", limitations=("io_error",))
    except KeyboardInterrupt:
        print("cancelled", file=sys.stderr)
        code, result = 130, CommandResult(command, "interrupted", limitations=("cancelled",))
    except Exception:
        # Do not dump exception repr/traceback, which may include response credentials.
        print("internal tool error", file=sys.stderr)
        code, result = 4, CommandResult(command, "error", limitations=("internal_error",))
    try:
        output = format_result(result, output_format)
    except Exception:
        print("internal tool error", file=sys.stderr)
        code = 4
        output = format_result(
            CommandResult(command, "error", limitations=("internal_error",)), output_format
        )
    print(output)
    return code
