"""Runtime and dependency metadata without loading execution backends."""

import importlib.metadata
import platform

from inferyard import SCHEMA_VERSION, __version__


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
        "schema_version": SCHEMA_VERSION,
        "supported_schema_versions": [SCHEMA_VERSION],
        "python": platform.python_version(),
        "implementation": platform.python_implementation(),
        "dependencies": dependencies,
    }
