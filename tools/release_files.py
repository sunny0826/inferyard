"""Pure semver, version-file, and changelog helpers for InferYard releases."""

from __future__ import annotations

import re
import tomllib
from pathlib import Path

CLI_PYPROJECT = Path("cli/pyproject.toml")
CLI_INIT = Path("cli/src/inferyard/__init__.py")
CLI_LOCK = Path("cli/uv.lock")
CLI_CHANGELOG = Path("cli/CHANGELOG.md")
STUDIO_CONFIG = Path("studio/mygo.config.ts")
SEMVER_RE = re.compile(r"^(\d+)\.(\d+)\.(\d+)$")
_INIT_VERSION_RE = re.compile(r'^(__version__\s*=\s*)(["\'])([^"\']+)\2(\s*(?:#.*)?)$', re.M)
_STUDIO_VERSION_RE = re.compile(r'^(\s*version:\s*)(["\'])([^"\']+)\2(\s*,?\s*(?://.*)?)$', re.M)
_PROJECT_SECTION_RE = re.compile(r"^\[project\]\s*$", re.M)
_NEXT_TABLE_RE = re.compile(r"^\[[^\]]+\]\s*$", re.M)
_RELEASE_CHORE_RE = re.compile(r"^chore\(release\):", re.I)


class ReleaseError(RuntimeError):
    """Raised when a release preflight, edit, git/gh step, or check fails."""


def parse_semver(version: str) -> tuple[int, int, int]:
    """Parse strict ``X.Y.Z`` numeric semver required by Studio Windows Inno Setup."""
    match = SEMVER_RE.fullmatch(version)
    if not match:
        raise ReleaseError(
            f"Invalid version {version!r}: expected strict numeric X.Y.Z (for example 0.0.8)."
        )
    return int(match.group(1)), int(match.group(2)), int(match.group(3))


def compute_next_version(current: str, target: str) -> str:
    """Resolve ``patch``/``minor``/``major`` or validate an explicit ``X.Y.Z`` version."""
    major, minor, patch = parse_semver(current)
    if target == "patch":
        return f"{major}.{minor}.{patch + 1}"
    if target == "minor":
        return f"{major}.{minor + 1}.0"
    if target == "major":
        return f"{major + 1}.0.0"
    if parse_semver(target) <= (major, minor, patch):
        raise ReleaseError(
            f"Target version {target} must be greater than current version {current}."
        )
    return target


def read_versions(root: Path) -> dict[str, str]:
    """Read CLI and Studio version sources from ``root``."""
    for rel in (CLI_PYPROJECT, CLI_INIT, STUDIO_CONFIG):
        if not (root / rel).is_file():
            raise ReleaseError(f"Missing version file: {rel}")

    cli_ver = (
        tomllib.loads((root / CLI_PYPROJECT).read_text(encoding="utf-8"))
        .get("project", {})
        .get("version")
    )
    if not isinstance(cli_ver, str):
        raise ReleaseError(f"Missing [project].version in {CLI_PYPROJECT}")

    init_m = _INIT_VERSION_RE.search((root / CLI_INIT).read_text(encoding="utf-8"))
    if not init_m:
        raise ReleaseError(f"Missing __version__ in {CLI_INIT}")

    studio_m = _STUDIO_VERSION_RE.search((root / STUDIO_CONFIG).read_text(encoding="utf-8"))
    if not studio_m:
        raise ReleaseError(f"Missing version in {STUDIO_CONFIG}")

    return {
        str(CLI_PYPROJECT): cli_ver,
        str(CLI_INIT): init_m.group(3),
        str(STUDIO_CONFIG): studio_m.group(3),
    }


def resolve_current_version(root: Path) -> str:
    """Return the unified repository version or fail if any version file differs."""
    versions = read_versions(root)
    if len(set(versions.values())) != 1:
        summary = ", ".join(f"{name}={val}" for name, val in versions.items())
        raise ReleaseError(f"Version mismatch across repository files: {summary}")
    return next(iter(versions.values()))


def _replace_exact_once(text: str, pattern: re.Pattern[str], new: str, label: str) -> str:
    matches = list(pattern.finditer(text))
    if len(matches) != 1:
        raise ReleaseError(f"Expected exactly 1 {label}, found {len(matches)}.")
    return pattern.sub(rf"\g<1>\g<2>{new}\g<2>\g<3>", text, count=1)


def update_pyproject_version(text: str, old: str, new: str) -> str:
    """Update ``version = "<old>"`` strictly inside the ``[project]`` table."""
    section_match = _PROJECT_SECTION_RE.search(text)
    if not section_match:
        raise ReleaseError("Missing [project] table in cli/pyproject.toml")
    start = section_match.end()
    next_table = _NEXT_TABLE_RE.search(text, start)
    end = next_table.start() if next_table else len(text)
    pattern = re.compile(rf'^(\s*version\s*=\s*)(["\']){re.escape(old)}\2(\s*(?:#.*)?)$', re.M)
    updated = _replace_exact_once(
        text[start:end], pattern, new, f"version = {old!r} inside [project]"
    )
    return text[:start] + updated + text[end:]


def update_init_version(text: str, old: str, new: str) -> str:
    """Update ``__version__`` in ``cli/src/inferyard/__init__.py``."""
    pattern = re.compile(rf'^(__version__\s*=\s*)(["\']){re.escape(old)}\2(\s*(?:#.*)?)$', re.M)
    return _replace_exact_once(text, pattern, new, f"__version__ = {old!r} in {CLI_INIT}")


def update_studio_config_version(text: str, old: str, new: str) -> str:
    """Update ``version:`` in ``studio/mygo.config.ts``."""
    pattern = re.compile(rf'^(\s*version:\s*)(["\']){re.escape(old)}\2(\s*,?\s*(?://.*)?)$', re.M)
    return _replace_exact_once(text, pattern, new, f"version: {old!r} in {STUDIO_CONFIG}")


def format_changelog_bullets(
    subjects: list[str], custom_notes: list[str] | None = None
) -> list[str]:
    """Build normalized ``- ...`` changelog lines from custom notes or commit subjects."""
    if custom_notes is not None:
        cleaned = [re.sub(r"^[-*•]\s+", "", item.strip()) for item in custom_notes]
        return [f"- {line}" for line in cleaned if line]
    return [
        f"- {s.strip()}" for s in subjects if s.strip() and not _RELEASE_CHORE_RE.match(s.strip())
    ]


def prepend_changelog(text: str, version: str, bullets: list[str]) -> str:
    """Insert ``## <version>`` highlights under ``# Changelog``."""
    if re.search(rf"^##\s+v?{re.escape(version)}\s*$", text, re.M):
        raise ReleaseError(f"{CLI_CHANGELOG} already has a section for {version}.")
    if not bullets:
        raise ReleaseError(
            "No changelog entries found since latest tag; pass --notes '...' to provide highlights."
        )
    section = f"## {version}\n\n" + "\n".join(bullets) + "\n\n"
    header_match = re.match(r"^(#\s+Changelog\s*\n+)", text)
    if header_match:
        prefix = header_match.group(1).rstrip("\n") + "\n\n"
        return prefix + section + text[header_match.end() :].lstrip("\n")
    return "# Changelog\n\n" + section + text.lstrip("\n")
