"""Pure rules for declared root and subprocess environments, live and offline."""

import re
from pathlib import PurePosixPath, PureWindowsPath

from inferyard.errors import ContractError

_ENV_NAME = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*\Z")
_SECRET_KEY_PATTERN = re.compile(
    r"(?:PASSWORD|PASSWD|SECRET|API_?KEY|ACCESS_KEY|PRIVATE_KEY|AUTH_KEY|BEARER|MYSQL_PWD)"
    r"|(?:^|_)TOKEN(?:$|_)"
)
_URL_AUTHORITY_PATTERN = re.compile(r"[A-Za-z][A-Za-z0-9+.-]*://([^\s/,;]+)")


def has_embedded_url_credentials(value: str) -> bool:
    return any("@" in match.group(1) for match in _URL_AUTHORITY_PATTERN.finditer(value))


def _require(condition, message="invalid declared process environment"):
    if not condition:
        raise ContractError("installation.environment", message)


def parse_absolute_path(value: str) -> PurePosixPath | PureWindowsPath | None:
    if not isinstance(value, str) or not value:
        return None
    if value.startswith("/"):
        path = PurePosixPath(value)
        if path.is_absolute() and str(path) == value and ".." not in path.parts:
            return path
        return None
    win = PureWindowsPath(value)
    if win.is_absolute() and bool(win.drive) and ".." not in win.parts:
        return win
    return None


def is_absolute_path(value: str) -> bool:
    return parse_absolute_path(value) is not None


def _validate_env_map(environment):
    _require(isinstance(environment, dict))
    for key, value in environment.items():
        _require(
            isinstance(key, str)
            and _ENV_NAME.fullmatch(key) is not None
            and isinstance(value, str)
            and "\x00" not in value
        )
        upper = key.upper()
        if (
            _SECRET_KEY_PATTERN.search(upper) is not None and value.strip()
        ) or has_embedded_url_credentials(value):
            raise ContractError(
                "installation.environment",
                "plaintext credentials are not allowed in declared process environment",
            )
    if "PROMETHEUS_MULTIPROC_DIR" in environment:
        _require(parse_absolute_path(environment["PROMETHEUS_MULTIPROC_DIR"]) is not None)


def validate_root_environment(family, environment, working_directory):
    _ = family, working_directory
    _validate_env_map(environment)


def validate_installation_environment(declaration):
    root = declaration["environment"]
    validate_root_environment(declaration["family"], root, declaration["working_directory"])
    worker = declaration.get("worker_environment", root)
    if "worker_environment" in declaration:
        _validate_env_map(worker)
    return worker
