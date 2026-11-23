"""Unit and temp-git integration tests for tools/release.py and tools/release_notes.py."""

from __future__ import annotations

import argparse
import contextlib
import io
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parent))
import release  # noqa: E402
import release_notes  # noqa: E402


def _git(cwd: Path, *args: str) -> str:
    res = subprocess.run(
        ["git", *args], cwd=cwd, check=True, text=True, capture_output=True
    )
    return res.stdout.strip()


def _init_repo_with_remote(root: Path) -> tuple[Path, Path]:
    origin = root / "origin.git"
    work = root / "work"
    subprocess.run(["git", "init", "--bare", "-b", "main", str(origin)], check=True, capture_output=True)
    subprocess.run(["git", "clone", str(origin), str(work)], check=True, capture_output=True)
    _git(work, "config", "user.name", "Tester")
    _git(work, "config", "user.email", "tester@example.invalid")

    (work / "cli/src/inferyard").mkdir(parents=True)
    (work / "studio").mkdir(parents=True)
    (work / "cli/pyproject.toml").write_text(
        '[project]\nname = "inferyard"\nversion = "0.0.7"\n\n'
        '[tool.demo]\nversion = "0.0.7"\n',
        encoding="utf-8",
    )
    (work / "cli/src/inferyard/__init__.py").write_text('__version__ = "0.0.7"\n', encoding="utf-8")
    (work / "cli/uv.lock").write_text('version = 1\nname = "inferyard"\nver = "0.0.7"\n', encoding="utf-8")
    (work / "cli/CHANGELOG.md").write_text("# Changelog\n\n## 0.0.7\n\n- Initial\n", encoding="utf-8")
    (work / "studio/mygo.config.ts").write_text(
        "export default defineConfig({\n  version: '0.0.7',\n});\n", encoding="utf-8"
    )

    _git(work, "add", ".")
    _git(work, "commit", "-m", "chore(release): prepare v0.0.7")
    _git(work, "tag", "-a", "v0.0.7", "-m", "Release v0.0.7")
    _git(work, "push", "-u", "origin", "main", "--tags")

    (work / "README.md").write_text("hello\n", encoding="utf-8")
    _git(work, "add", "README.md")
    _git(work, "commit", "-m", "feat: add fast widget (#171)")
    _git(work, "push", "origin", "main")
    return work, origin


class SemverAndFileUpdateTests(unittest.TestCase):
    def test_compute_next_version_bumps_and_rejects_invalid(self) -> None:
        self.assertEqual(release.compute_next_version("0.0.7", "patch"), "0.0.8")
        self.assertEqual(release.compute_next_version("0.0.7", "minor"), "0.1.0")
        self.assertEqual(release.compute_next_version("0.0.7", "major"), "1.0.0")
        self.assertEqual(release.compute_next_version("0.0.7", "0.2.3"), "0.2.3")

        for bad in ("v0.0.8", "0.0.8-rc.1", "0.0", "0.0.7", "0.0.6", "abc"):
            with self.subTest(bad=bad), self.assertRaises(release.ReleaseError):
                release.compute_next_version("0.0.7", bad)

    def test_resolve_current_version_detects_mismatch(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "cli/src/inferyard").mkdir(parents=True)
            (root / "studio").mkdir(parents=True)
            (root / "cli/pyproject.toml").write_text('[project]\nversion = "0.0.7"\n', encoding="utf-8")
            (root / "cli/src/inferyard/__init__.py").write_text('__version__ = "0.0.6"\n', encoding="utf-8")
            (root / "studio/mygo.config.ts").write_text("version: '0.0.7',\n", encoding="utf-8")
            with self.assertRaises(release.ReleaseError) as ctx:
                release.resolve_current_version(root)
            msg = str(ctx.exception)
            self.assertIn("pyproject.toml=0.0.7", msg)
            self.assertIn("__init__.py=0.0.6", msg)
            self.assertIn("mygo.config.ts=0.0.7", msg)

    def test_update_pyproject_scoped_to_project_table(self) -> None:
        raw = (
            '[build-system]\nrequires = ["hatchling"]\n\n'
            '[project]\nname = "inferyard"\nversion = "0.0.7"\n\n'
            '[tool.custom]\nversion = "0.0.7"\n'
        )
        updated = release.update_pyproject_version(raw, "0.0.7", "0.0.8")
        self.assertIn('[project]\nname = "inferyard"\nversion = "0.0.8"', updated)
        self.assertIn('[tool.custom]\nversion = "0.0.7"', updated)

    def test_update_init_and_studio_preserve_quotes(self) -> None:
        self.assertEqual(
            release.update_init_version('__version__ = "0.0.7"  # ver\n', "0.0.7", "0.0.8"),
            '__version__ = "0.0.8"  # ver\n',
        )
        self.assertEqual(
            release.update_studio_config_version("  version: '0.0.7',\n", "0.0.7", "0.0.8"),
            "  version: '0.0.8',\n",
        )
        self.assertEqual(
            release.update_studio_config_version('  version: "0.0.7",\n', "0.0.7", "0.0.8"),
            '  version: "0.0.8",\n',
        )

    def test_changelog_formatting_and_prepending(self) -> None:
        bullets = release.format_changelog_bullets(
            ["chore(release): prepare v0.0.7", "  ", "feat: alpha", "fix: beta"]
        )
        self.assertEqual(bullets, ["- feat: alpha", "- fix: beta"])
        self.assertEqual(
            release.format_changelog_bullets(["ignored"], [" - custom one ", "• custom two"]),
            ["- custom one", "- custom two"],
        )

        updated = release.prepend_changelog("# Changelog\n\n## 0.0.7\n\n- Old\n", "0.0.8", bullets)
        self.assertTrue(updated.startswith("# Changelog\n\n## 0.0.8\n\n- feat: alpha\n- fix: beta\n\n## 0.0.7"))

        with self.assertRaises(release.ReleaseError):
            release.prepend_changelog(updated, "0.0.8", bullets)
        with self.assertRaises(release.ReleaseError):
            release.prepend_changelog("# Changelog\n", "0.0.9", [])


class ReleaseNotesTests(unittest.TestCase):
    def test_extract_filter_and_render_notes(self) -> None:
        changelog = "# Changelog\n\n## 0.0.8\n\n- Highlight A\n- Highlight B\n\n## 0.0.7\n\n- Older\n"
        self.assertEqual(
            release_notes.extract_changelog_section(changelog, "0.0.8"),
            "- Highlight A\n- Highlight B",
        )
        self.assertEqual(release_notes.extract_changelog_section("## v0.0.8\n\n- A\n", "0.0.8"), "- A")
        self.assertEqual(release_notes.extract_changelog_section(changelog, "0.0.9"), "")

        generated = (
            "## What's Changed\n"
            "* feat: fast release (#171)\n"
            "* chore(release): prepare v0.0.8 (#172)\n\n\n"
            "**Full Changelog**: https://example.com/compare/v0.0.7...v0.0.8\n"
        )
        filtered = release_notes.filter_generated_notes(generated)
        self.assertNotIn("chore(release)", filtered)
        self.assertIn("* feat: fast release (#171)", filtered)
        self.assertIn("**Full Changelog**:", filtered)

        rendered = release_notes.render_release_notes(
            version="0.0.8", changelog_text=changelog, generated_text=generated
        )
        self.assertIn("## macOS installation", rendered)
        self.assertIn("xattr -dr com.apple.quarantine", rendered)
        self.assertIn("## Highlights\n\n- Highlight A\n- Highlight B", rendered)
        self.assertNotIn("chore(release)", rendered)
        self.assertTrue(rendered.endswith("\n"))

        rendered_no_cl = release_notes.render_release_notes(
            version="0.0.9", changelog_text=changelog, generated_text=generated
        )
        self.assertNotIn("## Highlights", rendered_no_cl)

    def test_cli_writes_output_file(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            cl = root / "CHANGELOG.md"
            gen = root / "generated.md"
            out = root / "sub/notes.md"
            cl.write_text("# Changelog\n\n## 0.0.8\n\n- Item 1\n", encoding="utf-8")
            gen.write_text("## What's Changed\n* chore(release): prepare v0.0.8\n* fix: bug\n", encoding="utf-8")
            rc = release_notes.main(
                ["--version", "0.0.8", "--changelog", str(cl), "--generated", str(gen), "--out", str(out)]
            )
            self.assertEqual(rc, 0)
            text = out.read_text(encoding="utf-8")
            self.assertIn("## Highlights\n\n- Item 1", text)
            self.assertIn("* fix: bug", text)
            self.assertNotIn("chore(release)", text)


class ReleaseWorkflowIntegrationTests(unittest.TestCase):
    def _args(self, target: str | None, **overrides: object) -> argparse.Namespace:
        defaults: dict[str, object] = {
            "target": target,
            "dry_run": False,
            "prepare_only": False,
            "notes": None,
            "no_check": False,
            "wait_pr_checks": False,
            "no_watch": False,
        }
        defaults.update(overrides)
        return argparse.Namespace(**defaults)

    def test_no_args_and_dry_run_do_not_mutate_repo(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            work, _ = _init_repo_with_remote(Path(tmp))
            before_head = _git(work, "rev-parse", "HEAD")

            buf = io.StringIO()
            with contextlib.redirect_stdout(buf):
                rc = release.run_release(self._args(None), root=work)
            self.assertEqual(rc, 0)
            self.assertIn("Current version: 0.0.7", buf.getvalue())
            self.assertIn("Pending commits since v0.0.7: 1", buf.getvalue())

            buf = io.StringIO()
            with mock.patch("shutil.which", return_value="/usr/bin/mock"), contextlib.redirect_stdout(buf):
                rc = release.run_release(self._args("patch", dry_run=True), root=work)
            self.assertEqual(rc, 0)
            self.assertIn("Dry run: 0.0.7 -> 0.0.8", buf.getvalue())
            self.assertIn("- feat: add fast widget (#171)", buf.getvalue())
            self.assertEqual(_git(work, "rev-parse", "HEAD"), before_head)
            self.assertEqual(release.resolve_current_version(work), "0.0.7")

    def test_prepare_only_commits_on_release_branch_without_pushing(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            work, origin = _init_repo_with_remote(Path(tmp))
            main_head = _git(work, "rev-parse", "HEAD")

            with (
                mock.patch("shutil.which", return_value="/usr/bin/mock"),
                mock.patch.object(release, "refresh_uv_lock") as mock_lock,
                mock.patch.object(release, "run_local_checks") as mock_checks,
                contextlib.redirect_stdout(io.StringIO()),
            ):
                rc = release.run_release(self._args("patch", prepare_only=True), root=work)

            self.assertEqual(rc, 0)
            mock_lock.assert_called_once_with(work)
            mock_checks.assert_called_once_with(work)
            self.assertEqual(_git(work, "rev-parse", "--abbrev-ref", "HEAD"), "chore/release-v0.0.8")
            self.assertEqual(_git(work, "log", "-1", "--pretty=format:%s"), "chore(release): prepare v0.0.8")
            self.assertEqual(release.resolve_current_version(work), "0.0.8")
            self.assertIn("## 0.0.8", (work / "cli/CHANGELOG.md").read_text(encoding="utf-8"))
            self.assertEqual(_git(work, "rev-parse", "main"), main_head)
            self.assertEqual(_git(origin, "ls-remote", "--heads", ".", "chore/release-v0.0.8"), "")

    def test_full_pr_merge_tag_push_flow(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            work, origin = _init_repo_with_remote(tmp_path)
            real_run = subprocess.run
            state: dict[str, str] = {}

            def fake_subprocess_run(args: list[str], **kwargs: object) -> subprocess.CompletedProcess[str]:
                if args and args[0] == "gh":
                    if args[1:3] == ["auth", "status"]:
                        return subprocess.CompletedProcess(args, 0, stdout="", stderr="")
                    if args[1:3] == ["pr", "create"]:
                        return subprocess.CompletedProcess(
                            args, 0, stdout="https://github.com/sunny0826/inferyard/pull/172\n", stderr=""
                        )
                    if args[1:3] == ["pr", "merge"]:
                        sim = tmp_path / "sim"
                        real_run(["git", "clone", "-b", "main", str(origin), str(sim)], check=True, capture_output=True)
                        _git(sim, "config", "user.name", "GitHub")
                        _git(sim, "config", "user.email", "noreply@github.com")
                        _git(sim, "merge", "--squash", "origin/chore/release-v0.0.8")
                        _git(sim, "commit", "-m", "chore(release): prepare v0.0.8 (#172)")
                        _git(sim, "push", "origin", "main")
                        _git(sim, "push", "origin", "--delete", "chore/release-v0.0.8")
                        state["merge_sha"] = _git(sim, "rev-parse", "HEAD")
                        return subprocess.CompletedProcess(args, 0, stdout="", stderr="")
                    if args[1:3] == ["pr", "view"]:
                        return subprocess.CompletedProcess(args, 0, stdout=state["merge_sha"] + "\n", stderr="")
                    if args[1:3] == ["run", "list"]:
                        payload = json.dumps([
                            {"databaseId": 999, "headSha": state["merge_sha"], "event": "push", "url": "https://example.invalid/999"}
                        ])
                        return subprocess.CompletedProcess(args, 0, stdout=payload, stderr="")
                    if args[1:3] == ["run", "watch"]:
                        state["watched"] = args[3]
                        return subprocess.CompletedProcess(args, 0, stdout="", stderr="")
                return real_run(args, **kwargs)

            with (
                mock.patch("shutil.which", return_value="/usr/bin/mock"),
                mock.patch("subprocess.run", side_effect=fake_subprocess_run),
                mock.patch.object(release, "refresh_uv_lock"),
                mock.patch.object(release, "run_local_checks"),
                contextlib.redirect_stdout(io.StringIO()),
            ):
                rc = release.run_release(self._args("patch"), root=work)

            self.assertEqual(rc, 0)
            self.assertEqual(state.get("watched"), "999")
            self.assertEqual(_git(work, "rev-parse", "--abbrev-ref", "HEAD"), "main")
            self.assertEqual(_git(work, "rev-parse", "HEAD"), state["merge_sha"])
            self.assertEqual(_git(origin, "rev-parse", "v0.0.8^{commit}"), state["merge_sha"])
            self.assertEqual(release.resolve_current_version(work), "0.0.8")
            self.assertEqual(_git(work, "branch", "--list", "chore/release-v0.0.8"), "")

    def test_guardrails_reject_dirty_non_main_existing_tag_and_merge_mismatch(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            work, _ = _init_repo_with_remote(tmp_path)

            # Dirty worktree
            (work / "dirty.txt").write_text("x\n", encoding="utf-8")
            with mock.patch("shutil.which", return_value="/usr/bin/mock"):
                with self.assertRaises(release.ReleaseError):
                    release.preflight_git_and_gh(work, "0.0.8", require_gh=False, dry_run=False)
            (work / "dirty.txt").unlink()

            # Non-main branch
            _git(work, "checkout", "-b", "other")
            with mock.patch("shutil.which", return_value="/usr/bin/mock"):
                with self.assertRaises(release.ReleaseError):
                    release.preflight_git_and_gh(work, "0.0.8", require_gh=False, dry_run=False)
            _git(work, "checkout", "main")

            # Existing tag
            _git(work, "tag", "v0.0.8")
            with mock.patch("shutil.which", return_value="/usr/bin/mock"):
                with self.assertRaises(release.ReleaseError):
                    release.preflight_git_and_gh(work, "0.0.8", require_gh=False, dry_run=False)
            _git(work, "tag", "-d", "v0.0.8")

            # Merge SHA mismatch aborts before tagging
            real_run = subprocess.run

            def bad_merge_gh(args: list[str], **kwargs: object) -> subprocess.CompletedProcess[str]:
                if args and args[0] == "gh":
                    if args[1:3] == ["auth", "status"] or args[1:3] == ["pr", "merge"]:
                        return subprocess.CompletedProcess(args, 0, stdout="", stderr="")
                    if args[1:3] == ["pr", "create"]:
                        return subprocess.CompletedProcess(args, 0, stdout="https://example.invalid/pr/1\n", stderr="")
                    if args[1:3] == ["pr", "view"]:
                        return subprocess.CompletedProcess(args, 0, stdout="0" * 40 + "\n", stderr="")
                return real_run(args, **kwargs)

            with (
                mock.patch("shutil.which", return_value="/usr/bin/mock"),
                mock.patch("subprocess.run", side_effect=bad_merge_gh),
                mock.patch.object(release, "refresh_uv_lock"),
                mock.patch.object(release, "run_local_checks"),
                contextlib.redirect_stdout(io.StringIO()),
            ):
                with self.assertRaises(release.ReleaseError) as ctx:
                    release.run_release(self._args("patch"), root=work)
            self.assertIn("Merged commit mismatch", str(ctx.exception))
            self.assertEqual(_git(work, "tag", "--list", "v0.0.8"), "")


if __name__ == "__main__":
    unittest.main()
