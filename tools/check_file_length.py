#!/usr/bin/env python3
"""Incremental code file length gate (default cap: 500 lines).

Blocks:
- New code files exceeding ``max_lines``.
- Existing code files crossing ``max_lines``.
- Pre-existing legacy files (> ``max_lines``) growing by even 1 line.
"""

from __future__ import annotations

import argparse
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CODE_SUFFIXES = frozenset({".py", ".go", ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".sh"})
EXCLUDED_PARTS = frozenset({".git", ".venv", "__pycache__", "node_modules", "build", "dist", ".inferyard-dev"})


def count_lines(text: str) -> int:
    """Return total line count matching ``wc -l`` / editor line numbers."""
    return len(text.splitlines())


def evaluate_file_growth(
    rel_path: str, current_lines: int, base_lines: int | None, *, max_lines: int = 500
) -> str | None:
    """Return an error message if ``rel_path`` violates the line cap, else ``None``."""
    if current_lines <= max_lines:
        return None
    if base_lines is None:
        return f"{rel_path}: new file has {current_lines} lines (limit {max_lines})"
    if base_lines <= max_lines:
        return f"{rel_path}: crossed {max_lines}-line limit ({base_lines} -> {current_lines} lines)"
    if current_lines > base_lines:
        return (
            f"{rel_path}: legacy >{max_lines}-line file grew ({base_lines} -> {current_lines} lines); "
            "extract/split instead of adding lines"
        )
    return None


def _is_code_path(rel_path: str) -> bool:
    p = Path(rel_path)
    if p.suffix not in CODE_SUFFIXES:
        return False
    return not any(part in EXCLUDED_PARTS for part in p.parts)


def _git_changed_files(root: Path, base_ref: str) -> list[str]:
    committed = subprocess.run(
        ["git", "diff", "--name-only", "--diff-filter=ACMR", base_ref],
        cwd=root, check=True, text=True, capture_output=True,
    ).stdout.splitlines()
    untracked = subprocess.run(
        ["git", "ls-files", "--others", "--exclude-standard"],
        cwd=root, check=True, text=True, capture_output=True,
    ).stdout.splitlines()
    return sorted({line.strip() for line in (*committed, *untracked) if line.strip()})


def _git_base_line_count(root: Path, base_ref: str, rel_path: str) -> int | None:
    res = subprocess.run(
        ["git", "show", f"{base_ref}:{rel_path}"],
        cwd=root, check=False, text=True, encoding="utf-8", errors="replace", capture_output=True,
    )
    if res.returncode != 0:
        return None
    return count_lines(res.stdout)


def check_repo(root: Path, base_ref: str, *, max_lines: int = 500) -> list[str]:
    """Check all code files changed relative to ``base_ref``."""
    verify = subprocess.run(
        ["git", "rev-parse", "--verify", "--quiet", f"{base_ref}^{{commit}}"],
        cwd=root, check=False, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
    )
    if verify.returncode != 0:
        raise RuntimeError(f"Base commit not found in local git history: {base_ref}")

    violations: list[str] = []
    for rel in _git_changed_files(root, base_ref):
        if not _is_code_path(rel):
            continue
        full = root / rel
        if not full.is_file():
            continue
        curr = count_lines(full.read_text(encoding="utf-8", errors="replace"))
        base = _git_base_line_count(root, base_ref, rel)
        err = evaluate_file_growth(rel, curr, base, max_lines=max_lines)
        if err is not None:
            violations.append(err)
    return violations


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base", default="HEAD", help="Git ref to compare against (default: HEAD)")
    parser.add_argument("--max-lines", type=int, default=500, help="Maximum lines per code file")
    parser.add_argument("--root", type=Path, default=ROOT, help="Repository root")
    args = parser.parse_args(argv)

    try:
        violations = check_repo(args.root.resolve(), args.base, max_lines=args.max_lines)
    except RuntimeError as exc:
        print(f"check_file_length error: {exc}", file=sys.stderr)
        return 2

    if violations:
        print(f"Code file length gate failed (max {args.max_lines} lines):", file=sys.stderr)
        for item in violations:
            print(f"  - {item}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
