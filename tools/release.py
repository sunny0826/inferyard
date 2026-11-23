#!/usr/bin/env python3
"""Fast PR-compliant release helper for InferYard CLI and Studio."""

from __future__ import annotations

import argparse
import json
import re
import shutil
import subprocess
import sys
import time
from dataclasses import dataclass
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from release_files import (  # noqa: E402
    CLI_CHANGELOG,
    CLI_INIT,
    CLI_LOCK,
    CLI_PYPROJECT,
    SEMVER_RE,
    STUDIO_CONFIG,
    ReleaseError,
    compute_next_version,
    format_changelog_bullets,
    parse_semver,
    prepend_changelog,
    read_versions,
    resolve_current_version,
    update_init_version,
    update_pyproject_version,
    update_studio_config_version,
)

ROOT = Path(__file__).resolve().parents[1]

__all__ = [
    "CLI_CHANGELOG",
    "CLI_INIT",
    "CLI_LOCK",
    "CLI_PYPROJECT",
    "ROOT",
    "SEMVER_RE",
    "STUDIO_CONFIG",
    "ReleaseContext",
    "ReleaseError",
    "build_parser",
    "collect_commit_subjects",
    "compute_next_version",
    "format_changelog_bullets",
    "latest_version_tag",
    "main",
    "parse_semver",
    "preflight_git_and_gh",
    "prepend_changelog",
    "read_versions",
    "refresh_uv_lock",
    "resolve_current_version",
    "run_local_checks",
    "run_release",
    "update_init_version",
    "update_pyproject_version",
    "update_studio_config_version",
]


@dataclass
class ReleaseContext:
    """Mutable progress tracker used for contextual error recovery hints."""

    branch_name: str | None = None
    tag_name: str | None = None
    pr_url: str | None = None
    merged: bool = False


def _run(
    args: list[str], *, cwd: Path, capture: bool = False, timeout: int = 600
) -> subprocess.CompletedProcess[str]:
    try:
        result = subprocess.run(
            args, cwd=cwd, check=False, text=True, encoding="utf-8",
            errors="replace", capture_output=capture, timeout=timeout,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise ReleaseError(f"Command failed to run: {' '.join(args)}") from exc
    if result.returncode != 0:
        detail = (result.stderr or result.stdout or "").strip() if capture else ""
        raise ReleaseError(
            f"Command exited with {result.returncode}: {' '.join(args)}"
            + (f": {detail}" if detail else "")
        )
    return result


def _git_ref_exists(root: Path, ref: str) -> bool:
    return subprocess.run(
        ["git", "rev-parse", "--verify", "--quiet", ref],
        cwd=root, check=False, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
    ).returncode == 0


def latest_version_tag(root: Path, current_version: str) -> str | None:
    """Find ``v<current_version>`` or fall back to the newest ``v*`` tag."""
    expected = f"v{current_version}"
    if _git_ref_exists(root, f"refs/tags/{expected}"):
        return expected
    res = subprocess.run(
        ["git", "describe", "--tags", "--match", "v*", "--abbrev=0"],
        cwd=root, check=False, text=True, capture_output=True,
    )
    return (res.stdout.strip() or None) if res.returncode == 0 else None


def collect_commit_subjects(root: Path, since_tag: str | None) -> list[str]:
    """Return commit subject lines since ``since_tag`` (or all commits if none)."""
    rev_range = f"{since_tag}..HEAD" if since_tag else "HEAD"
    out = _run(
        ["git", "log", "--no-merges", "--pretty=format:%s", rev_range], cwd=root, capture=True
    ).stdout
    return [line.strip() for line in out.splitlines() if line.strip()]


def refresh_uv_lock(root: Path) -> None:
    """Update ``cli/uv.lock`` cleanly offline first, falling back to online ``uv lock``."""
    cli_dir = root / "cli"
    try:
        _run(["uv", "lock", "--offline"], cwd=cli_dir, capture=True)
    except ReleaseError:
        _run(["uv", "lock"], cwd=cli_dir, capture=True)
    _run(["uv", "lock", "--check"], cwd=cli_dir, capture=True)


def run_local_checks(root: Path) -> None:
    """Run fast local checks before committing or opening the release PR."""
    cli_dir = root / "cli"
    _run(["uv", "lock", "--check"], cwd=cli_dir)
    _run(["uv", "run", "--frozen", "ruff", "check", "src", "tests", "scripts"], cwd=cli_dir)
    _run(["uv", "run", "--frozen", "python", "scripts/export_schemas.py", "--check"], cwd=cli_dir)
    _run(
        [sys.executable, "-m", "unittest", "discover", "-s", "scripts", "-p", "test_*.py"],
        cwd=root / "studio",
    )
    _run(
        [sys.executable, "-m", "unittest", "discover", "-s", ".", "-p", "test_*.py"],
        cwd=root / "tools",
    )


def preflight_git_and_gh(
    root: Path, next_version: str, *, require_gh: bool, dry_run: bool
) -> list[str]:
    """Validate CLI tools, git tree/branch/sync state, and unused branch/tag names."""
    for tool in ["git", "uv"] + (["gh"] if require_gh else []):
        if shutil.which(tool) is None:
            raise ReleaseError(f"Required executable not found in PATH: {tool}")

    branch_name, tag_name = f"chore/release-v{next_version}", f"v{next_version}"
    status = _run(["git", "status", "--porcelain"], cwd=root, capture=True).stdout.strip()
    branch = _run(
        ["git", "rev-parse", "--abbrev-ref", "HEAD"], cwd=root, capture=True
    ).stdout.strip()

    if dry_run:
        warnings: list[str] = []
        if status:
            warnings.append("working tree has uncommitted changes (clean tree required for release)")
        if branch != "main":
            warnings.append(f"current branch is {branch!r} (release must start from 'main')")
        if _git_ref_exists(root, f"refs/tags/{tag_name}"):
            warnings.append(f"tag {tag_name} already exists locally")
        if _git_ref_exists(root, f"refs/heads/{branch_name}"):
            warnings.append(f"branch {branch_name} already exists locally")
        return warnings

    if require_gh:
        _run(["gh", "auth", "status"], cwd=root, capture=True)
    if status:
        raise ReleaseError("Working tree is not clean; commit or stash changes before releasing.")
    if branch != "main":
        raise ReleaseError(f"Release must start from 'main' (currently on {branch!r}).")

    _run(["git", "fetch", "origin", "main", "--tags"], cwd=root, capture=True)
    local_head = _run(["git", "rev-parse", "HEAD"], cwd=root, capture=True).stdout.strip()
    remote_head = _run(["git", "rev-parse", "origin/main"], cwd=root, capture=True).stdout.strip()
    if local_head != remote_head:
        raise ReleaseError(
            f"Local 'main' ({local_head[:7]}) is not aligned with 'origin/main' ({remote_head[:7]})."
        )
    if _git_ref_exists(root, f"refs/tags/{tag_name}"):
        raise ReleaseError(f"Tag {tag_name} already exists.")
    if _git_ref_exists(root, f"refs/heads/{branch_name}"):
        raise ReleaseError(f"Local branch {branch_name} already exists.")
    if _run(
        ["git", "ls-remote", "--heads", "origin", branch_name], cwd=root, capture=True
    ).stdout.strip():
        raise ReleaseError(f"Remote branch origin/{branch_name} already exists.")
    return []


def _apply_version_bump(root: Path, current: str, next_version: str, bullets: list[str]) -> None:
    edits = (
        (CLI_PYPROJECT, update_pyproject_version),
        (CLI_INIT, update_init_version),
        (STUDIO_CONFIG, update_studio_config_version),
    )
    for rel, updater in edits:
        path = root / rel
        path.write_text(
            updater(path.read_text(encoding="utf-8"), current, next_version), encoding="utf-8"
        )

    cl_path = root / CLI_CHANGELOG
    cl_text = cl_path.read_text(encoding="utf-8") if cl_path.is_file() else ""
    cl_path.write_text(prepend_changelog(cl_text, next_version, bullets), encoding="utf-8")
    refresh_uv_lock(root)
    actual = resolve_current_version(root)
    if actual != next_version:
        raise ReleaseError(f"Version verification failed: expected {next_version}, got {actual}")


def _repo_web_url(root: Path) -> str:
    res = subprocess.run(
        ["git", "remote", "get-url", "origin"], cwd=root, check=False, text=True, capture_output=True
    )
    raw = res.stdout.strip() if res.returncode == 0 else ""
    match = re.search(r"github\.com[:/]([^/]+/[^/.]+?)(?:\.git)?$", raw)
    return f"https://github.com/{match.group(1) if match else 'sunny0826/inferyard'}"


def _watch_release_workflow(root: Path, merge_sha: str, actions_url: str) -> None:
    run_id: int | None = None
    for _ in range(10):
        res = _run(
            ["gh", "run", "list", "--workflow", "release.yml", "--limit", "5", "--json", "databaseId,headSha,event,url"],
            cwd=root, capture=True,
        )
        for item in json.loads(res.stdout or "[]"):
            if item.get("event") == "push" and item.get("headSha") == merge_sha:
                run_id = int(item["databaseId"])
                break
        if run_id is not None:
            break
        time.sleep(3)

    if run_id is None:
        print(f"Release workflow not visible yet; monitor at: {actions_url}")
        return
    print(f"Watching release workflow run {run_id}...")
    _run(["gh", "run", "watch", str(run_id), "--exit-status"], cwd=root, capture=False, timeout=1800)


def _print_recovery_hint(ctx: ReleaseContext) -> None:
    if ctx.merged and ctx.tag_name:
        print(
            f"Recovery: PR {ctx.pr_url} is already merged on main.\n"
            f"  git checkout main && git pull --ff-only origin main\n"
            f"  git tag -a {ctx.tag_name} -m 'Release {ctx.tag_name}' && git push origin {ctx.tag_name}",
            file=sys.stderr,
        )
    elif ctx.pr_url and ctx.tag_name:
        print(
            f"Recovery: Release PR is open at {ctx.pr_url}.\n"
            f"  gh pr merge {ctx.pr_url} --squash --delete-branch\n"
            f"  git checkout main && git pull --ff-only origin main\n"
            f"  git tag -a {ctx.tag_name} -m 'Release {ctx.tag_name}' && git push origin {ctx.tag_name}",
            file=sys.stderr,
        )
    elif ctx.branch_name:
        print(
            f"Recovery: local branch {ctx.branch_name} may still exist.\n"
            f"  git status && git checkout main && git branch -D {ctx.branch_name}",
            file=sys.stderr,
        )


def _print_dry_run(
    current: str, next_version: str, bullets: list[str], warnings: list[str], args: argparse.Namespace
) -> None:
    tag_name = f"v{next_version}"
    print(f"Dry run: {current} -> {next_version} (tag {tag_name}, branch chore/release-{tag_name})")
    print("Files to update:")
    for rel in (CLI_PYPROJECT, CLI_INIT, CLI_LOCK, CLI_CHANGELOG, STUDIO_CONFIG):
        print(f"  - {rel}")
    print(f"\nChangelog preview:\n## {next_version}\n")
    print("\n".join(bullets) if bullets else "  (no commits since previous tag; pass --notes)")
    if warnings:
        print("\nPreflight warnings:\n" + "\n".join(f"  ! {w}" for w in warnings))
    mode = "prepare-only (local commit)" if args.prepare_only else "PR -> squash merge -> tag -> watch CI"
    print(f"\nMode: {mode}")


def _merge_release_pr(
    root: Path, branch_name: str, tag_name: str, next_version: str, bullets: list[str],
    wait_checks: bool, ctx: ReleaseContext,
) -> str:
    _run(["git", "push", "-u", "origin", branch_name], cwd=root, capture=True)
    pr_body = f"## Release {tag_name}\n\n" + "\n".join(bullets)
    pr_res = _run(
        ["gh", "pr", "create", "--base", "main", "--head", branch_name,
         "--title", f"chore(release): prepare {tag_name}", "--body", pr_body],
        cwd=root, capture=True,
    )
    ctx.pr_url = pr_res.stdout.strip().splitlines()[-1].strip()
    if wait_checks:
        _run(["gh", "pr", "checks", ctx.pr_url, "--watch"], cwd=root, capture=False)

    _run(["git", "checkout", "main"], cwd=root, capture=True)
    _run(["gh", "pr", "merge", ctx.pr_url, "--squash", "--delete-branch"], cwd=root, capture=True)
    ctx.merged = True
    _run(["git", "pull", "--ff-only", "origin", "main"], cwd=root, capture=True)
    if _git_ref_exists(root, f"refs/heads/{branch_name}"):
        _run(["git", "branch", "-D", branch_name], cwd=root, capture=True)

    merge_sha = _run(
        ["gh", "pr", "view", ctx.pr_url, "--json", "mergeCommit", "--jq", ".mergeCommit.oid"],
        cwd=root, capture=True,
    ).stdout.strip()
    local_sha = _run(["git", "rev-parse", "HEAD"], cwd=root, capture=True).stdout.strip()
    if not merge_sha or local_sha != merge_sha:
        raise ReleaseError(f"Merged commit mismatch: local main={local_sha}, PR mergeCommit={merge_sha}")
    if resolve_current_version(root) != next_version:
        raise ReleaseError("Version on merged main does not match release target.")
    return merge_sha


def run_release(args: argparse.Namespace, *, root: Path = ROOT, ctx: ReleaseContext | None = None) -> int:
    """Execute or preview the InferYard release flow."""
    ctx = ctx or ReleaseContext()
    current = resolve_current_version(root)
    tag = latest_version_tag(root, current)
    bullets = format_changelog_bullets(collect_commit_subjects(root, tag), args.notes)

    if args.target is None:
        print(f"Current version: {current} (latest tag: {tag or 'none'})")
        print(f"Pending commits since {tag or 'start'}: {len(bullets)}")
        print("\nUsage:\n  mise run release patch\n  mise run release patch --dry-run\n  mise run release 0.1.0 --prepare-only")
        return 0

    next_version = compute_next_version(current, args.target)
    warnings = preflight_git_and_gh(root, next_version, require_gh=not args.prepare_only, dry_run=args.dry_run)
    if args.dry_run:
        _print_dry_run(current, next_version, bullets, warnings, args)
        return 0
    if not bullets:
        raise ReleaseError("No changelog entries found since latest tag; pass --notes '...' to provide highlights.")

    tag_name, branch_name = f"v{next_version}", f"chore/release-v{next_version}"
    ctx.branch_name, ctx.tag_name = branch_name, tag_name
    head_sha = _run(["git", "rev-parse", "--short", "HEAD"], cwd=root, capture=True).stdout.strip()
    print(f"[1/7] Preflight ok (main @ {head_sha})")

    _run(["git", "checkout", "-b", branch_name], cwd=root, capture=True)
    print(f"[2/7] Created branch {branch_name}")

    _apply_version_bump(root, current, next_version, bullets)
    print(f"[3/7] Updated versions and CHANGELOG to {next_version}")

    if args.no_check:
        print("[4/7] Skipped local checks (--no-check)")
    else:
        run_local_checks(root)
        print("[4/7] Local checks passed")

    stage = [str(p) for p in (CLI_PYPROJECT, CLI_INIT, CLI_LOCK, CLI_CHANGELOG, STUDIO_CONFIG)]
    _run(["git", "add", "--", *stage], cwd=root, capture=True)
    _run(["git", "commit", "-m", f"chore(release): prepare {tag_name}"], cwd=root, capture=True)
    print(f"[5/7] Committed chore(release): prepare {tag_name}")

    if args.prepare_only:
        print(
            f"\nPrepared locally. Next steps:\n  git push -u origin {branch_name}\n"
            f"  gh pr create --base main --head {branch_name} --title 'chore(release): prepare {tag_name}'\n"
            f"  git checkout main && git pull --ff-only origin main\n"
            f"  git tag -a {tag_name} -m 'Release {tag_name}' && git push origin {tag_name}"
        )
        return 0

    merge_sha = _merge_release_pr(root, branch_name, tag_name, next_version, bullets, args.wait_pr_checks, ctx)
    print(f"[6/7] Merged {ctx.pr_url} onto main @ {merge_sha[:7]}")

    _run(["git", "tag", "-a", tag_name, "-m", f"Release {tag_name}"], cwd=root, capture=True)
    _run(["git", "push", "origin", tag_name], cwd=root, capture=True)
    repo_url = _repo_web_url(root)
    actions_url = f"{repo_url}/actions/workflows/release.yml"
    print(
        f"[7/7] Pushed tag {tag_name}\n  Actions: {actions_url}\n"
        f"  Release: {repo_url}/releases/tag/{tag_name}\n"
        f"  PyPI:    https://pypi.org/project/inferyard/{next_version}/"
    )
    if not args.no_watch:
        _watch_release_workflow(root, merge_sha, actions_url)
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("target", nargs="?", help="Version bump (patch, minor, major) or strict X.Y.Z")
    parser.add_argument("--dry-run", action="store_true", help="Preview changes without modifying anything")
    parser.add_argument("--prepare-only", action="store_true", help="Commit on release branch without pushing")
    parser.add_argument("--notes", action="append", default=None, help="Custom changelog bullet (repeatable)")
    parser.add_argument("--no-check", action="store_true", help="Skip local ruff/schema/unit checks")
    parser.add_argument("--wait-pr-checks", action="store_true", help="Wait for PR checks before merging")
    parser.add_argument("--no-watch", action="store_true", help="Do not watch release.yml after pushing tag")
    return parser


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    ctx = ReleaseContext()
    try:
        return run_release(args, ctx=ctx)
    except ReleaseError as exc:
        print(f"Error: {exc}", file=sys.stderr)
        _print_recovery_hint(ctx)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
