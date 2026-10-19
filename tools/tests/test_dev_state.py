"""Behavior checks for source freshness and checkout-local state boundaries."""

import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from inferyard_dev.state import (
    DevError,
    inside,
    launcher_lock,
    package_sha,
    read_json,
    source,
    write_json,
)


class SourceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        subprocess.run(["git", "init", "-q", "-b", "main", str(self.root)], check=True)
        self.code = self.root / "cli/src/inferyard"
        self.code.mkdir(parents=True)
        (self.code / "__init__.py").write_text("VERSION = 1\n", encoding="utf-8")
        self.git("add", ".")
        self.git(
            "-c",
            "user.name=Dev Test",
            "-c",
            "user.email=dev@example.invalid",
            "commit",
            "-qm",
            "Fixture",
        )

    def git(self, *args):
        subprocess.run(["git", "-C", str(self.root), *args], check=True, capture_output=True)

    def test_source_and_untracked_resources_invalidate_build_identity(self):
        original = source(self.root)
        self.assertFalse(original.dirty)
        resource = self.code / "catalogue.json"
        resource.write_text('{"revision":2}', encoding="utf-8")
        changed = source(self.root)
        self.assertTrue(changed.dirty)
        self.assertNotEqual(changed.source_sha256, original.source_sha256)
        resource.write_text('{"revision":3}', encoding="utf-8")
        self.assertNotEqual(source(self.root).source_sha256, changed.source_sha256)
        resource.unlink()
        self.assertEqual(source(self.root), original)

    def test_branches_select_distinct_profiles_and_docs_do_not_change_code(self):
        before = source(self.root)
        docs = self.root / "studio/docs/plans"
        docs.mkdir(parents=True)
        (docs / "user-work.md").write_text("preserve me", encoding="utf-8")
        self.assertEqual(before, source(self.root))
        self.git("switch", "-qc", "feature/development")
        self.assertNotEqual(before.profile_key, source(self.root).profile_key)


class StateTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()

    def test_evidence_is_not_overwritten_and_duplicate_fields_rejected(self):
        path = self.root / "manifest.json"
        write_json(path, {"generation": 1}, replace=False)
        with self.assertRaises(DevError):
            write_json(path, {"generation": 2}, replace=False)
        self.assertEqual(read_json(path), {"generation": 1})
        path.write_text('{"generation":1,"generation":2}', encoding="utf-8")
        with self.assertRaises(DevError):
            read_json(path)

    def test_launcher_excludes_concurrent_writers_without_deleting_lock(self):
        with launcher_lock(self.root):
            with self.assertRaises(DevError):
                with launcher_lock(self.root):
                    self.fail("A concurrent launcher acquired the same lock")
        self.assertTrue((self.root / "launcher.lock").exists())
        with launcher_lock(self.root):
            pass

    @unittest.skipIf(sys.platform == "win32", "Symlink creation requires Windows privileges")
    def test_symlink_cannot_redirect_generated_outputs(self):
        state = self.root / "state"
        state.mkdir()
        outside = self.root / "outside"
        outside.mkdir()
        (state / "builds").symlink_to(outside, target_is_directory=True)
        with self.assertRaises(DevError):
            inside(state / "builds/new", state)
        self.assertEqual(list(outside.iterdir()), [])

    def test_package_digest_binds_resources_and_ignores_bytecode(self):
        package = self.root / "inferyard"
        package.mkdir()
        (package / "__init__.py").write_text("VERSION = 1", encoding="utf-8")
        initial = package_sha(package)
        cache = package / "__pycache__"
        cache.mkdir()
        (cache / "ignored.pyc").write_bytes(b"bytecode")
        self.assertEqual(package_sha(package), initial)
        (package / "bundle.json").write_text("{}", encoding="utf-8")
        self.assertNotEqual(package_sha(package), initial)


if __name__ == "__main__":
    unittest.main()
