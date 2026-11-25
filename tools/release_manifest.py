#!/usr/bin/env python3
"""Generate a static latest.json update manifest compatible with InferYard Studio."""

from __future__ import annotations

import argparse
import json
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import quote


def build_release_manifest(
    *,
    tag: str,
    repo: str,
    notes: str,
    assets_dir: Path,
    published_at: str | None = None,
) -> dict[str, object]:
    """Build a GitHub-Release-compatible JSON manifest for static hosting."""
    clean_tag = tag.strip()
    if not clean_tag:
        raise ValueError("tag must not be empty")
    clean_repo = repo.strip().strip("/")
    if "/" not in clean_repo:
        raise ValueError(f"invalid repository slug: {repo!r}")

    asset_files = sorted(
        (p for p in assets_dir.iterdir() if p.is_file()),
        key=lambda p: p.name,
    )
    if not asset_files:
        raise ValueError(f"no release assets found in {assets_dir}")

    names = {p.name for p in asset_files}
    if "SHA256SUMS" not in names:
        raise ValueError(f"SHA256SUMS missing from {assets_dir}")

    timestamp = (published_at or "").strip()
    if not timestamp:
        timestamp = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")

    encoded_tag = quote(clean_tag, safe="")
    download_prefix = f"https://github.com/{clean_repo}/releases/download/{encoded_tag}"

    assets = [
        {
            "name": path.name,
            "browser_download_url": f"{download_prefix}/{quote(path.name, safe='')}",
            "size": path.stat().st_size,
        }
        for path in asset_files
    ]

    return {
        "tag_name": clean_tag,
        "name": clean_tag,
        "body": notes.strip(),
        "html_url": f"https://github.com/{clean_repo}/releases/tag/{encoded_tag}",
        "draft": False,
        "prerelease": False,
        "published_at": timestamp,
        "assets": assets,
    }


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tag", required=True, help="Release tag, e.g. v0.10.0")
    parser.add_argument("--repo", required=True, help="GitHub repository slug, e.g. owner/repo")
    parser.add_argument("--notes-file", type=Path, required=True, help="Path to release notes markdown")
    parser.add_argument("--assets-dir", type=Path, required=True, help="Directory containing release assets")
    parser.add_argument("--published-at", help="Optional ISO-8601 UTC timestamp")
    parser.add_argument("--out", type=Path, required=True, help="Output JSON path")
    args = parser.parse_args(argv)

    notes = args.notes_file.read_text(encoding="utf-8") if args.notes_file.is_file() else ""
    manifest = build_release_manifest(
        tag=args.tag,
        repo=args.repo,
        notes=notes,
        assets_dir=args.assets_dir,
        published_at=args.published_at,
    )
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text(json.dumps(manifest, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
