"""Instance identity, failed-build preservation and profile reset behaviors."""

import io
import subprocess
import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path
from unittest.mock import Mock, patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from inferyard_dev.build import Build, studio_path
from inferyard_dev.lifecycle import executable, launch, ready_session
from inferyard_dev.main import develop
from inferyard_dev.state import DevError, Source, file_sha, read_json, write_json


class DevelopmentTests(unittest.TestCase):
    def setUp(self):
        self.enterContext(redirect_stdout(io.StringIO()))
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        self.state = self.root / ".inferyard-dev"
        self.info = Source(str(self.root), "feature/dev", "a" * 40, True, "b" * 64)
        self.events = []
        self.builds = []
        self.manifests = []

    def build(self, root, output, info, tools):
        output.mkdir(parents=True)
        binary = studio_path(output)
        binary.parent.mkdir(parents=True, exist_ok=True)
        binary.write_bytes(b"built studio")
        cli = output / "cli-env/bin/inferyard"
        python = output / "cli-env/bin/python"
        package = output / "cli-env/lib/inferyard"
        package.mkdir(parents=True)
        cli.parent.mkdir(parents=True)
        cli.write_bytes(b"local CLI")
        hashes = {
            "studio_sha256": file_sha(binary),
            "cli_sha256": file_sha(cli),
            "package_sha256": "c" * 64,
        }
        build = Build(output, binary, python, cli, package, output / "wheel.whl", hashes)
        self.builds.append(build)
        return build

    def command(self, args, **kwargs):
        if "init" in args:
            workspace = Path(args[-1])
            write_json(workspace / "preparation.json", {"kind": "community_init.v1"})
        self.events.append("check" if "--dev-check" in args else "init")
        return subprocess.CompletedProcess(args, 0, "", "")

    def launched(self, state, manifest, cli):
        self.events.append("launch")
        self.manifests.append(manifest)
        return {"pid": 123}

    def started(self, action="start", **kwargs):
        with (
            patch("inferyard_dev.main.source", return_value=self.info),
            patch("inferyard_dev.main.Toolchain.load"),
            patch("inferyard_dev.main.prepare", side_effect=self.build),
            patch("inferyard_dev.main.run", side_effect=self.command),
            patch(
                "inferyard_dev.main.stop_recorded", side_effect=lambda _: self.events.append("stop")
            ),
            patch("inferyard_dev.main.launch", side_effect=self.launched),
        ):
            return develop(self.root, action, **kwargs)

    def test_regular_start_reuses_profile_rebuild_keeps_history_and_assets(self):
        first = self.started()
        old = Path(read_json(first)["profile_root"])
        evidence = old / "workspace/results/original.json"
        write_json(evidence, {"original": True}, replace=False)
        asset = self.state / "assets/models/model.bin"
        asset.write_bytes(b"cached model")
        second = self.started()
        self.assertEqual(read_json(second)["profile_root"], str(old))
        third = self.started("rebuild")
        self.assertNotEqual(read_json(third)["profile_root"], str(old))
        self.assertEqual(read_json(evidence), {"original": True})
        self.assertEqual(asset.read_bytes(), b"cached model")
        self.assertEqual(read_json(self.state / "active.json")["manifest_path"], str(third))
        self.assertEqual(self.events[-3:], ["check", "stop", "launch"])

    def test_build_failure_does_not_stop_or_switch_previous_instance(self):
        first = self.started()
        previous = read_json(self.state / "active.json")
        with (
            patch("inferyard_dev.main.source", return_value=self.info),
            patch("inferyard_dev.main.Toolchain.load"),
            patch("inferyard_dev.main.prepare", side_effect=DevError("build failed")),
            patch("inferyard_dev.main.stop_recorded") as stop,
        ):
            with self.assertRaisesRegex(DevError, "build failed"):
                develop(self.root, "rebuild")
        stop.assert_not_called()
        self.assertEqual(read_json(self.state / "active.json"), previous)
        self.assertTrue(first.exists())

    def test_prepare_only_does_not_activate_or_stop_any_instance(self):
        manifest = self.started(prepare_only=True)
        self.assertTrue(manifest.is_file())
        self.assertNotIn("stop", self.events)
        self.assertNotIn("launch", self.events)
        self.assertFalse((self.state / "active.json").exists())

    def test_another_checkout_and_modified_binary_cannot_control_instance(self):
        manifest = self.started(prepare_only=True)
        self.assertTrue(executable(self.state, manifest).is_file())
        build = self.builds[-1]
        build.studio.write_bytes(b"stale executable")
        with self.assertRaisesRegex(DevError, "changed"):
            executable(self.state, manifest)
        data = read_json(manifest)
        data["checkout"] = str(self.root / "other-checkout")
        write_json(manifest, data)
        with self.assertRaisesRegex(DevError, "another checkout"):
            executable(self.state, manifest)

    def test_ready_marker_must_match_child_and_fixed_cli(self):
        path = self.root / "session.json"
        cli = self.root / "prepared/cli"
        value = {
            "kind": "studio_dev_session.v1",
            "session_id": "launch",
            "pid": 123,
            "state": "ready",
            "cli_path": str(cli),
        }
        write_json(path, value)
        self.assertIsNotNone(ready_session(path, "launch", cli, 123))
        with self.assertRaises(DevError):
            ready_session(path, "launch", Path("/wrong/cli"), 123)
        with self.assertRaises(DevError):
            ready_session(path, "launch", cli, 456)

    def test_explicit_stop_releases_pointer_but_preserves_profile_and_manifest(self):
        manifest = self.started()
        profile = Path(read_json(manifest)["profile_root"])
        with patch("inferyard_dev.main.stop_recorded") as stop:
            develop(self.root, "stop")
        stop.assert_called_once_with(self.state)
        self.assertFalse((self.state / "active.json").exists())
        self.assertTrue(manifest.is_file())
        self.assertTrue((profile / "workspace/preparation.json").is_file())

    def test_failed_launch_retains_pending_ownership_when_shutdown_fails(self):
        manifest = self.started(prepare_only=True)
        process = Mock(pid=321)
        process.poll.return_value = None
        with (
            patch("inferyard_dev.lifecycle.subprocess.Popen", return_value=process),
            patch("inferyard_dev.lifecycle.stop_manifest", side_effect=DevError("not stopped")),
        ):
            with self.assertRaisesRegex(DevError, "retained"):
                launch(self.state, manifest, self.builds[-1].cli, timeout=0)
        self.assertEqual(read_json(self.state / "pending.json")["manifest_path"], str(manifest))
        self.assertEqual(
            read_json(manifest.parent / "stop.json")["session_id"], manifest.parent.name
        )


if __name__ == "__main__":
    unittest.main()
