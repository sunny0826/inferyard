"""Runtime and dependency metadata without loading execution backends."""

import importlib.metadata
import json
import platform

from inferyard import SCHEMA_VERSION, __version__


def install_source() -> str:
    """checkout for a PEP 660 editable install, otherwise release."""
    try:
        dist = importlib.metadata.distribution("inferyard")
        raw = dist.read_text("direct_url.json")
    except importlib.metadata.PackageNotFoundError, OSError, TypeError:
        return "release"
    if not raw:
        return "release"
    try:
        data = json.loads(raw)
    except json.JSONDecodeError:
        return "release"
    editable = isinstance(data, dict) and (data.get("dir_info") or {}).get("editable") is True
    return "checkout" if editable else "release"


def version_line() -> str:
    return f"inferyard {__version__} source={install_source()}"


def versions() -> dict:
    dependencies = {}
    for name in (
        "inferyard",
        "httpx",
        "jinja2",
        "pytest",
        "jsonschema",
        "ruff",
        "psutil",
        "anyio",
        "certifi",
        "h11",
        "httpcore",
        "idna",
        "markupsafe",
        "attrs",
        "jsonschema-specifications",
        "referencing",
        "rpds-py",
        "typing-extensions",
    ):
        try:
            dependencies[name] = importlib.metadata.version(name)
        except importlib.metadata.PackageNotFoundError:
            dependencies[name] = None
    return {
        "tool_version": __version__,
        "source": install_source(),
        "schema_version": SCHEMA_VERSION,
        "supported_schema_versions": [SCHEMA_VERSION],
        "python": platform.python_version(),
        "implementation": platform.python_implementation(),
        "dependencies": dependencies,
    }
