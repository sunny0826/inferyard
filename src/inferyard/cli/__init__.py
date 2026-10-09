"""Public CLI facade and compatibility hooks for application request construction."""

from __future__ import annotations

import argparse
from collections.abc import Mapping, Sequence

from inferyard.application.dispatch import default_backend
from inferyard.application.types import CommandRequest, CommandResult, Handler
from inferyard.cli.arguments import ArgumentError, Parser, parser
from inferyard.cli.entrypoint import run
from inferyard.cli.metadata import versions
from inferyard.cli.request import build_request
from inferyard.config.loader import LoadedConfig, load_config
from inferyard.config.review_cache import command_reviews

__all__ = [
    "ArgumentError",
    "CommandRequest",
    "CommandResult",
    "Handler",
    "LoadedConfig",
    "Parser",
    "default_backend",
    "load_config",
    "main",
    "parser",
    "versions",
]


def _request(args: argparse.Namespace) -> CommandRequest:
    # Resolve the public hook at call time for existing load_config injection.
    return build_request(args, config_loader=load_config)


def main(
    argv: Sequence[str] | None = None, *, handlers: Mapping[str, Handler] | None = None
) -> int:
    with command_reviews():
        return run(
            argv,
            handlers=handlers,
            parser_factory=parser,
            request_factory=_request,
            backend=default_backend,
        )
