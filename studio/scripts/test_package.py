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


class ReleasePackagingTests(unittest.TestCase):
    def run_build_tags(self, tags: str) -> None:
        output = f'{{"Settings": [{{"Key": "-tags", "Value": "{tags}"}}]}}'
        result = subprocess.CompletedProcess([], 0, stdout=output)
        with patch("package.subprocess.run", return_value=result):
            require_release_build(Path("app"))

    def test_build_tags(self) -> None:
        for tags in ("release", "ui_debug,release"):
            with self.subTest(tags=tags):
                self.run_build_tags(tags)
        for tags in ("ui_debug", ""):
            with self.subTest(tags=tags):
                self.assertRaises(ValueError, self.run_build_tags, tags)

    def test_missing_build_information_is_rejected(self) -> None:
        for output in ["{}", "not build info"]:
            result = subprocess.CompletedProcess([], 0, stdout=output)
            with patch("package.subprocess.run", return_value=result):
                with self.assertRaises(ValueError):
                    require_release_build(Path("app"))

    def test_all_inputs_are_checked_before_writing_distributions(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            with patch("package.require_release_build", side_effect=[None, ValueError("debug")]):
                with self.assertRaises(ValueError):
                    package(root)
            self.assertEqual(list(root.iterdir()), [])


class DevDistributionTests(unittest.TestCase):
    def make_root(self, directory: str) -> Path:
        root = Path(directory)
        for target in common.TARGETS:
            binary = root / "build" / target / common.executable_name(target)
            binary.parent.mkdir(parents=True)
            binary.write_bytes(f"binary-{target}".encode())
        return root

    def test_distribution_contents(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = self.make_root(directory)
            with patch("package.require_release_build", return_value=None):
                package(root)
            version = common.studio_version()
            license_raw = (common.STUDIO_ROOT.parent / "LICENSE").read_bytes()
            for target in common.TARGETS:
                dist = root / "dist" / common.distribution_name(version, target)
                self.assertEqual(
                    (dist / common.executable_name(target)).read_bytes(),
                    f"binary-{target}".encode(),
                )
                self.assertEqual((dist / "LICENSE").read_bytes(), license_raw)
                readme = (dist / "README.txt").read_text()
                self.assertIn("Unsigned build; not notarized.", readme)
                self.assertNotIn("no notarization", readme)
            app = (
                root / "dist" / common.distribution_name(version, "darwin-arm64")
                / "InferYard Studio.app" / "Contents" / "MacOS" / "inferyard-studio"
            )
            self.assertTrue(app.is_file())

    def test_refuses_to_overwrite_existing_distribution(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = self.make_root(directory)
            version = common.studio_version()
            existing = root / "dist" / common.distribution_name(version, "linux-amd64")
            existing.mkdir(parents=True)
            with patch("package.require_release_build", return_value=None):
                with self.assertRaises(FileExistsError):
                    package(root)
            self.assertEqual(list(existing.iterdir()), [])
            darwin = root / "dist" / common.distribution_name(version, "darwin-arm64")
            self.assertFalse(darwin.exists())

    def test_missing_license_is_rejected_before_writing(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = self.make_root(directory)
            missing = root / "missing-LICENSE"
            with (
                patch("package.require_release_build", return_value=None),
                patch("package.LICENSE_SOURCE", missing),
            ):
                with self.assertRaises(ValueError):
                    package(root)
            self.assertFalse((root / "dist").exists())


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

    def test_darwin_uses_hdiutil_and_applications_shortcut(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "src"
            binary = root / "build/darwin-arm64/inferyard-studio"
            binary.parent.mkdir(parents=True)
            binary.write_bytes(b"release-binary")
            out = Path(directory) / "out"
            commands = self.run_installer_package("darwin-arm64", root, out, [])
            self.assertTrue(any(command[0] == "hdiutil" for command in commands))
            archive = out / common.archive_name(common.studio_version(), "darwin-arm64")
            self.assertEqual(archive.suffix, ".dmg")
            dist = out / common.distribution_name(common.studio_version(), "darwin-arm64")
            self.assertFalse((dist / "Applications").is_symlink())

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
            self.assertIn("AppVersion=0.0.4", script)
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

    def test_rejects_invalid_source_commit(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "src"
            out = Path(directory) / "out"
            with self.assertRaises(ValueError):
                package_target("darwin-arm64", "not-a-sha", out, root=root)
            self.assertFalse(out.exists())

    def test_refuses_existing_output(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root, out, _ = self.make_build(directory, "linux-amd64")
            with patch("package.subprocess.run", return_value=fake_run(goos="linux", goarch="amd64")):
                with self.assertRaises(FileExistsError):
                    package_target("linux-amd64", SHA, out, root=root)

    def test_missing_binary_is_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "src"
            out = Path(directory) / "out"
            with self.assertRaises(ValueError):
                package_target("darwin-arm64", SHA, out, root=root)
            self.assertFalse(out.exists())

    def test_cli_entry_parses_target_arguments(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            out = Path(directory) / "out"
            argv = ["package.py", "--target", "darwin-arm64", "--source-commit", SHA, "--out", str(out)]
            import package
            with patch("sys.argv", argv), patch.object(package, "package_target") as target:
                target.return_value = {"version": common.studio_version(), "source_commit": SHA}
                package.main()
            target.assert_called_once_with("darwin-arm64", SHA, out)


_TEST_METHODS = (
    ReleasePackagingTests.test_build_tags,
    ReleasePackagingTests.test_missing_build_information_is_rejected,
    ReleasePackagingTests.test_all_inputs_are_checked_before_writing_distributions,
    DevDistributionTests.test_distribution_contents,
    DevDistributionTests.test_refuses_to_overwrite_existing_distribution,
    DevDistributionTests.test_missing_license_is_rejected_before_writing,
    SingleTargetPackageTests.test_darwin_package_receipt_and_archive,
    SingleTargetPackageTests.test_darwin_uses_hdiutil_and_applications_shortcut,
    SingleTargetPackageTests.test_windows_uses_inno_setup_with_user_install,
    SingleTargetPackageTests.test_rejects_non_release_dirty_or_mismatched_build,
    SingleTargetPackageTests.test_rejects_invalid_source_commit,
    SingleTargetPackageTests.test_refuses_existing_output,
    SingleTargetPackageTests.test_missing_binary_is_rejected,
    SingleTargetPackageTests.test_cli_entry_parses_target_arguments,
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
