#!/usr/bin/env python3
"""Assemble GitHub Release notes from CHANGELOG highlights and generated PR notes."""

from __future__ import annotations

import argparse
import re
from pathlib import Path

MACOS_INSTALL_SECTION = """## macOS installation

Open the `.dmg`, drag `InferYard Studio.app` into `Applications`, and clear the quarantine attribute before first launch (the release build is unsigned):

```sh
xattr -dr com.apple.quarantine /Applications/InferYard\\ Studio.app
```"""

_NEXT_HEADING_RE = re.compile(r"^##\s+")
_RELEASE_CHORE_BULLET_RE = re.compile(r"^\s*[-*•]\s+chore\(release\):", re.IGNORECASE)


def extract_changelog_section(changelog_text: str, version: str) -> str:
    """Return the markdown body under ``## <version>`` (or ``## v<version>``), if present."""
    target_re = re.compile(rf"^##\s+v?{re.escape(version)}\s*$")
    capturing = False
    collected: list[str] = []

    for line in changelog_text.splitlines():
        if not capturing:
            if target_re.match(line):
                capturing = True
            continue
        if _NEXT_HEADING_RE.match(line):
            break
        collected.append(line)

    return "\n".join(collected).strip()


def filter_generated_notes(generated_text: str) -> str:
    """Drop ``chore(release):`` bullets from GitHub generated release notes."""
    kept: list[str] = []
    previous_blank = False

    for line in generated_text.splitlines():
        if _RELEASE_CHORE_BULLET_RE.match(line):
            continue
        is_blank = line.strip() == ""
        if is_blank:
            if not kept or previous_blank:
                continue
            kept.append("")
            previous_blank = True
        else:
            kept.append(line.rstrip())
            previous_blank = False

    return "\n".join(kept).strip()


def render_release_notes(*, version: str, changelog_text: str, generated_text: str) -> str:
    """Render combined GitHub Release notes ending with a single newline."""
    sections = [MACOS_INSTALL_SECTION.strip()]

    highlights = extract_changelog_section(changelog_text, version)
    if highlights:
        sections.append(f"## Highlights\n\n{highlights}")

    filtered_generated = filter_generated_notes(generated_text)
    if filtered_generated:
        sections.append(filtered_generated)

    return "\n\n".join(sections) + "\n"


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True, help="Release version without leading v")
    parser.add_argument("--changelog", type=Path, help="Path to CHANGELOG.md")
    parser.add_argument("--generated", type=Path, help="Path to GitHub generated release notes")
    parser.add_argument("--out", type=Path, required=True, help="Output markdown path")
    args = parser.parse_args(argv)

    changelog_text = ""
    if args.changelog is not None and args.changelog.is_file():
        changelog_text = args.changelog.read_text(encoding="utf-8")

    generated_text = ""
    if args.generated is not None and args.generated.is_file():
        generated_text = args.generated.read_text(encoding="utf-8")

    rendered = render_release_notes(
        version=args.version,
        changelog_text=changelog_text,
        generated_text=generated_text,
    )
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text(rendered, encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
