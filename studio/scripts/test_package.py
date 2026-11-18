import fnmatch
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import release_common as common
from package import package, package_target, require_release_build

SHA = "a" * 40
OTHER_SHA = "b" * 40


def fake_run(
    tags: str = "release",
    revision: str = SHA,
    modified: str = "false",
    goos: str = "darwin",
    goarch: str = "arm64",
) -> subprocess.CompletedProcess[str]:
    settings = ", ".join(
        f'{{"Key": "{key}", "Value": "{value}"}}'
        for key, value in [
            ("-tags", tags),
            ("GOOS", goos),
            ("GOARCH", goarch),
            ("vcs.revision", revision),
            ("vcs.modified", modified),
            ("vcs.time", "2026-11-16T00:00:00Z"),
        ]
    )
    stdout = f'{{"GoVersion": "go1.27.3", "Settings": [{settings}]}}'
    return subprocess.CompletedProcess([], 0, stdout=stdout)


class SingleTargetPackageTests(unittest.TestCase):
    def fake_packaging_run(
        self, command: list[str], target: str, out: Path
    ) -> subprocess.CompletedProcess[str]:
        if command[0] == "hdiutil":
            distribution = Path(command[command.index("-srcfolder") + 1])
            self.assertTrue((distribution / "Applications").is_symlink())
            self.assertEqual((distribution / "Applications").readlink(), Path("/Applications"))
            Path(command[-1]).write_bytes(b"test-dmg")
        elif command[0].endswith("ISCC.exe"):
            archive = out / common.archive_name(common.studio_version(), target)
            archive.write_bytes(b"test-installer")
        goos, goarch = common.GOOS_GOARCH[target]
        return fake_run(goos=goos, goarch=goarch)

    def make_build(self, directory: str, target: str, **info: str) -> tuple[Path, Path, dict[str, common.JsonData]]:
        root = Path(directory) / "src"
        binary = root / "build" / target / common.executable_name(target)
        binary.parent.mkdir(parents=True)
        binary.write_bytes(f"release-binary-{target}".encode())
        out = Path(directory) / "out"
        goos, goarch = common.GOOS_GOARCH[target]
        build_run = fake_run(goos=goos, goarch=goarch, **info)

        def run(
            command: list[str], **_: str | int | bool | Path | dict[str, str]
        ) -> subprocess.CompletedProcess[str]:
            if command[0] == "go":
                return build_run
            return self.fake_packaging_run(command, target, out)

        with patch("package.subprocess.run", side_effect=run):
            return root, out, package_target(target, SHA, out, root=root)

    def assert_rejected_build(self, info: dict[str, str]) -> None:
        with tempfile.TemporaryDirectory() as directory:
            goos, goarch = common.GOOS_GOARCH["linux-amd64"]
            root = Path(directory) / "src"
            binary = root / "build" / "linux-amd64" / "inferyard-studio"
            binary.parent.mkdir(parents=True)
            binary.write_bytes(b"binary")
            out = Path(directory) / "out"
            run = fake_run(goos=goos, goarch=goarch, **info)
            with patch("package.subprocess.run", return_value=run):
                with self.assertRaises(ValueError):
                    package_target("linux-amd64", SHA, out, root=root)
            self.assertFalse(out.exists())

    def test_darwin_package_receipt_and_archive(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            _, out, receipt = self.make_build(directory, "darwin-arm64")
            version = common.studio_version()
            name = common.distribution_name(version, "darwin-arm64")
            dist = out / name
            self.assertTrue((dist / "LICENSE").is_file())
            self.assertTrue((dist / "inferyard-studio").is_file())
            self.assertTrue((dist / "InferYard Studio.app").is_dir())
            import plistlib
            plist = plistlib.loads(
                (dist / "InferYard Studio.app/Contents/Info.plist").read_bytes()
            )
            self.assertEqual(plist["NSPrincipalClass"], "NSApplication")
            archive = out / common.archive_name(version, "darwin-arm64")
            self.assertEqual(archive.name, f"inferyard-studio-v{version}-darwin-arm64.dmg")
            self.assertEqual(receipt["kind"], "studio-package.v1")
            self.assertEqual(receipt["version"], version)
            self.assertEqual(receipt["source_commit"], SHA)
            self.assertIs(receipt["source_dirty"], False)
            self.assertEqual(receipt["target"], "darwin-arm64")
            archive_raw = archive.read_bytes()
            self.assertEqual(receipt["archive"]["path"], archive.name)
            self.assertEqual(receipt["archive"]["bytes"], len(archive_raw))
            self.assertEqual(receipt["archive"]["sha256"], common.sha(archive_raw))
            binary_rel = f"{name}/inferyard-studio"
            binary_raw = (out / binary_rel).read_bytes()
            self.assertEqual(receipt["binary"]["path"], binary_rel)
            self.assertEqual(receipt["binary"]["bytes"], len(binary_raw))
            self.assertEqual(receipt["binary"]["sha256"], common.sha(binary_raw))
            self.assertEqual(receipt["build_info"]["vcs_revision"], SHA)
            self.assertEqual(receipt["build_info"]["vcs_modified"], False)
            self.assertIn("release", receipt["build_info"]["tags"])

    def run_installer_package(
        self, target: str, root: Path, out: Path, scripts: list[str]
    ) -> list[list[str]]:
        commands = []

        def run(
            command: list[str], **_: str | int | bool | Path | dict[str, str]
        ) -> subprocess.CompletedProcess[str]:
            commands.append(command)
            if command[-1].endswith(".iss"):
                scripts.append(Path(command[-1]).read_text())
            return self.fake_packaging_run(command, target, out)

        with patch("package.subprocess.run", side_effect=run):
            package_target(target, SHA, out, root=root)
        return commands


    def test_windows_uses_inno_setup_with_user_install(self) -> None:
        with tempfile.TemporaryDirectory(dir=Path.cwd()) as directory:
            root = Path(directory) / "src"
            binary = root / "build/windows-amd64/inferyard-studio.exe"
            binary.parent.mkdir(parents=True)
            binary.write_bytes(b"release-binary")
            out = Path(directory).relative_to(Path.cwd()) / "out"
            scripts = []
            commands = self.run_installer_package("windows-amd64", root, out, scripts)
            self.assertTrue(any(command[0].endswith("ISCC.exe") for command in commands))
            archive = out / common.archive_name(common.studio_version(), "windows-amd64")
            self.assertEqual(archive.suffix, ".exe")
            script = scripts[0]
            self.assertIn(f"AppVersion={common.studio_version()}", script)
            self.assertIn("PrivilegesRequired=lowest", script)
            self.assertIn("UninstallDisplayName=", script)
            self.assertIn("{autopf}", script)
            output_dir = str(out.resolve()).replace("/", "\\")
            distribution = out / common.distribution_name(common.studio_version(), "windows-amd64")
            license_path = str((distribution / "LICENSE").resolve()).replace("/", "\\")
            self.assertIn(f"OutputDir={output_dir}", script)
            self.assertIn(f"LicenseFile={license_path}", script)

    def test_rejects_non_release_dirty_or_mismatched_build(self) -> None:
        for info in [dict(tags="ui_debug"), dict(modified="true"), dict(revision=OTHER_SHA)]:
            with self.subTest(info=info):
                self.assert_rejected_build(info)


_TEST_METHODS = (
    SingleTargetPackageTests.test_darwin_package_receipt_and_archive,
    SingleTargetPackageTests.test_windows_uses_inno_setup_with_user_install,
    SingleTargetPackageTests.test_rejects_non_release_dirty_or_mismatched_build,
)


def load_tests(
    loader: unittest.TestLoader,
    tests: unittest.TestSuite,
    pattern: str | None,
) -> unittest.TestSuite:
    for method in _TEST_METHODS:
        if pattern is None or fnmatch.fnmatch(method.__name__, pattern):
            tests.addTest(loader.loadTestsFromName(method.__qualname__, sys.modules[__name__]))
    return tests


if __name__ == "__main__":
    unittest.main()
