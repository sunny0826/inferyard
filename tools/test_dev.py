"""Unit tests for shared development asset resolution in tools/dev.py."""

from __future__ import annotations

import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parent))
import dev  # noqa: E402


def _git(cwd: Path, *args: str) -> str:
    result = subprocess.run(
        ["git", *args], cwd=cwd, check=True, capture_output=True, text=True
    )
    return result.stdout.strip()


def _init_repo(root: Path) -> None:
    _git(root, "init", "-q", "-b", "main")
    _git(root, "config", "user.name", "Tester")
    _git(root, "config", "user.email", "tester@example.invalid")
    (root / "README.md").write_text("init\n", encoding="utf-8")
    _git(root, "add", "README.md")
    _git(root, "commit", "-q", "-m", "init")


class AssetsDirTests(unittest.TestCase):
    def setUp(self) -> None:
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.root = Path(tmp.name).resolve()
        self.state_dir = self.root / dev.STATE_DIR_NAME
        patcher = mock.patch.dict(os.environ, {}, clear=False)
        patcher.start()
        self.addCleanup(patcher.stop)
        os.environ.pop("INFERYARD_DEV_ASSETS", None)

    def test_defaults_to_worktree_state(self) -> None:
        assets = dev.resolve_assets_dir(self.root, self.state_dir)
        self.assertEqual(assets, self.state_dir / "assets")
        for name in ("models", "engines"):
            self.assertTrue((assets / name).is_dir())

    def test_explicit_assets_created_outside_state(self) -> None:
        target = self.root / "elsewhere" / "assets"
        assets = dev.resolve_assets_dir(self.root, self.state_dir, str(target))
        self.assertEqual(assets, target)
        for name in ("models", "engines"):
            self.assertTrue((assets / name).is_dir())
        self.assertFalse(self.state_dir.exists())

    def test_env_assets_override_default(self) -> None:
        target = self.root / "env-assets"
        with mock.patch.dict(os.environ, {"INFERYARD_DEV_ASSETS": str(target)}):
            assets = dev.resolve_assets_dir(self.root, self.state_dir)
        self.assertEqual(assets, target)
        self.assertTrue((target / "engines").is_dir())

    def test_explicit_assets_take_priority_over_env(self) -> None:
        env_target = self.root / "env-assets"
        cli_target = self.root / "cli-assets"
        with mock.patch.dict(os.environ, {"INFERYARD_DEV_ASSETS": str(env_target)}):
            assets = dev.resolve_assets_dir(self.root, self.state_dir, str(cli_target))
        self.assertEqual(assets, cli_target)
        self.assertFalse(env_target.exists())

    def test_rejects_symlink_assets_root(self) -> None:
        target = self.root / "real"
        target.mkdir()
        link = self.root / "link"
        link.symlink_to(target, target_is_directory=True)
        with self.assertRaises(dev.DevError):
            dev.resolve_assets_dir(self.root, self.state_dir, str(link))

    def test_rejects_symlinked_parent(self) -> None:
        target = self.root / "real"
        target.mkdir()
        link = self.root / "link"
        link.symlink_to(target, target_is_directory=True)
        with self.assertRaises(dev.DevError):
            dev.resolve_assets_dir(self.root, self.state_dir, str(link / "nested" / "assets"))

    def test_rejects_symlink_models_subdir(self) -> None:
        assets = self.root / "assets"
        assets.mkdir()
        elsewhere = self.root / "elsewhere"
        elsewhere.mkdir()
        (assets / "models").symlink_to(elsewhere, target_is_directory=True)
        with self.assertRaises(dev.DevError):
            dev.resolve_assets_dir(self.root, self.state_dir, str(assets))


class LinkedWorktreeAssetsTests(unittest.TestCase):
    def setUp(self) -> None:
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        base = Path(tmp.name).resolve()
        self.main = base / "main"
        self.main.mkdir()
        _init_repo(self.main)
        self.linked = base / "linked"
        _git(self.main, "worktree", "add", "-q", "-b", "feature", str(self.linked))
        self.linked_state = self.linked / dev.STATE_DIR_NAME
        self.shared_assets = self.main / dev.STATE_DIR_NAME / "assets"
        patcher = mock.patch.dict(os.environ, {}, clear=False)
        patcher.start()
        self.addCleanup(patcher.stop)
        os.environ.pop("INFERYARD_DEV_ASSETS", None)

    def test_reuses_main_worktree_assets(self) -> None:
        for name in ("models", "engines"):
            (self.shared_assets / name).mkdir(parents=True)
        assets = dev.resolve_assets_dir(self.linked, self.linked_state)
        self.assertEqual(assets, self.shared_assets)
        self.assertFalse((self.linked_state / "assets").exists())

    def test_creates_missing_shared_subdirs(self) -> None:
        self.shared_assets.mkdir(parents=True)
        assets = dev.resolve_assets_dir(self.linked, self.linked_state)
        self.assertEqual(assets, self.shared_assets)
        for name in ("models", "engines"):
            self.assertTrue((assets / name).is_dir())

    def test_falls_back_when_main_assets_missing(self) -> None:
        assets = dev.resolve_assets_dir(self.linked, self.linked_state)
        self.assertEqual(assets, self.linked_state / "assets")

    def test_ignores_symlinked_main_state(self) -> None:
        external = Path(self.main.parent) / "external"
        (external / "assets").mkdir(parents=True)
        (self.main / dev.STATE_DIR_NAME).symlink_to(external, target_is_directory=True)
        assets = dev.resolve_assets_dir(self.linked, self.linked_state)
        self.assertEqual(assets, self.linked_state / "assets")

    def test_main_checkout_keeps_its_own_assets(self) -> None:
        assets = dev.resolve_assets_dir(self.main, self.main / dev.STATE_DIR_NAME)
        self.assertEqual(assets, self.shared_assets)


if __name__ == "__main__":
    unittest.main()
