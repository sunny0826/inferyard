"""Recovery proof validation, legacy handoff and retry ownership boundaries."""

import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from inferyard_dev.build import studio_path
from inferyard_dev.lifecycle import stop_manifest, stop_recorded
from inferyard_dev.state import DevError, file_sha, read_json, write_json


class RecoveryTests(unittest.TestCase):
    def setUp(self):
        temporary = self.enterContext(tempfile.TemporaryDirectory())
        self.state = Path(temporary).resolve() / ".inferyard-dev"
        self.target = self.manifest("old")
        self.controller = self.manifest("new")
        self.child = self.target.parent / "child.json"
        write_json(self.child, {"pid": 123})
        write_json(self.state / "pending.json", {"manifest_path": str(self.target)})
        self.commands = []
        self.proof = {
            "kind": "studio_dev_recovery.v1",
            "outcome": "recovered_missing_session",
            "controller_manifest": str(self.controller),
            "target_manifest": str(self.target),
            "controller_sha256": file_sha(self.controller),
            "target_sha256": file_sha(self.target),
            "child_sha256": file_sha(self.child),
            "pid": 123,
        }

    def manifest(self, name):
        build = self.state / "builds" / name
        binary = studio_path(build)
        binary.parent.mkdir(parents=True)
        binary.write_bytes(name.encode())
        manifest = self.state / "launches" / name / "manifest.json"
        write_json(
            manifest,
            {
                "kind": "studio_dev.v1",
                "checkout": str(self.state.parent),
                "build_root": str(build),
                "studio_sha256": file_sha(binary),
            },
        )
        return manifest

    def command(self, args, **kwargs):
        self.commands.append((args, kwargs))
        return subprocess.CompletedProcess(args, 0, json.dumps(self.proof), "")

    def test_recovery_uses_new_controller_and_retains_original_bytes(self):
        before = {path: path.read_bytes() for path in (self.target, self.child)}
        with patch("inferyard_dev.lifecycle.run", side_effect=self.command):
            stop_recorded(self.state, controller=self.controller)
        self.assertFalse((self.state / "pending.json").exists())
        self.assertEqual(len(self.commands), 1)
        args = self.commands[0][0]
        self.assertEqual(Path(args[0]), studio_path(self.state / "builds/new"))
        self.assertIn("--dev-recovery-target", args)
        for path, raw in before.items():
            self.assertEqual(path.read_bytes(), raw)

    def test_session_handoff_runs_original_controller_with_remaining_deadline(self):
        self.proof["outcome"] = "session_present"
        self.proof["child_sha256"] = ""
        with (
            patch("inferyard_dev.lifecycle.run", side_effect=self.command),
            patch("inferyard_dev.lifecycle.time.monotonic", side_effect=[0, 10, 30]),
        ):
            stop_manifest(self.state, self.target, controller=self.controller)
        self.assertEqual(self.commands[0][1]["timeout"], 65)
        self.assertEqual(self.commands[1][1]["timeout"], 45)
        self.assertEqual(
            self.commands[1][0],
            [str(studio_path(self.state / "builds/old")), "--dev-stop", str(self.target)],
        )

    def test_handoff_failure_never_downgrades_to_missing_recovery(self):
        self.proof["outcome"] = "session_present"

        def command(args, **kwargs):
            if "--dev-stop" in args:
                raise DevError("session disappeared or shutdown failed")
            return self.command(args, **kwargs)

        with patch("inferyard_dev.lifecycle.run", side_effect=command):
            with self.assertRaises(DevError):
                stop_recorded(self.state, controller=self.controller)
        self.assertEqual(len(self.commands), 1)
        self.assertTrue((self.state / "pending.json").exists())

    def test_no_controller_keeps_original_stop_without_building(self):
        with patch("inferyard_dev.lifecycle.run", side_effect=self.command):
            stop_manifest(self.state, self.target)
        self.assertEqual(len(self.commands), 1)
        self.assertIn("--dev-stop", self.commands[0][0])

    def test_invalid_proofs_do_not_release_pending(self):
        original = dict(self.proof)
        variants = [
            "",
            "null",
            "{}",
            '{"pid":1,"pid":2}',
            json.dumps(original).replace('"pid": 123', '"pid": NaN'),
        ]
        for key, bad in (
            ("kind", "wrong"),
            ("outcome", "unknown"),
            ("pid", True),
            ("pid", 1.0),
            ("pid", 0),
            ("pid", 2**32),
            ("controller_manifest", str(self.target)),
            ("target_sha256", "0" * 64),
            ("child_sha256", ""),
            ("child_sha256", []),
        ):
            variant = dict(original)
            variant[key] = bad
            variants.append(json.dumps(variant))
        for output in variants:
            with (
                self.subTest(output=output),
                patch(
                    "inferyard_dev.lifecycle.run",
                    return_value=subprocess.CompletedProcess([], 0, output, ""),
                ),
            ):
                with self.assertRaises(DevError):
                    stop_recorded(self.state, controller=self.controller)
                self.assertTrue((self.state / "pending.json").exists())

    def test_pending_active_order_dedup_and_retry_after_interruption(self):
        write_json(self.state / "active.json", {"manifest_path": str(self.target)})
        with patch("inferyard_dev.lifecycle.run", side_effect=self.command):
            # Interruption after success but before unlink leaves a safely retryable pointer.
            with patch.object(Path, "unlink", side_effect=KeyboardInterrupt):
                with self.assertRaises(KeyboardInterrupt):
                    stop_recorded(self.state, controller=self.controller)
            self.assertTrue((self.state / "pending.json").exists())
            stop_recorded(self.state, controller=self.controller)
        self.assertEqual(len(self.commands), 2)
        self.assertEqual(read_json(self.state / "active.json")["manifest_path"], str(self.target))
        self.commands.clear()
        write_json(self.state / "pending.json", {"manifest_path": str(self.target)})
        write_json(self.state / "active.json", {"manifest_path": str(self.controller)})
        calls = []
        with patch(
            "inferyard_dev.lifecycle.stop_manifest", side_effect=lambda s, p, **k: calls.append(p)
        ):
            stop_recorded(self.state, controller=self.controller)
        self.assertEqual(calls, [self.target, self.controller])


if __name__ == "__main__":
    unittest.main()
