import fnmatch
import json
import subprocess
import sys
import tempfile
import unittest
from collections.abc import Callable
from pathlib import Path
from unittest.mock import patch

import release_common as common
import verify_release_candidate
from package import package_target
from prepare_release_candidate import prepare
from verify_release_candidate import verify

SHA = "a" * 40
OTHER_SHA = "b" * 40


def fake_run(target: str) -> subprocess.CompletedProcess[str]:
    goos, goarch = common.GOOS_GOARCH[target]
    settings = ", ".join(
        f'{{"Key": "{key}", "Value": "{value}"}}'
        for key, value in [
            ("-tags", "release"),
            ("GOOS", goos),
            ("GOARCH", goarch),
            ("vcs.revision", SHA),
            ("vcs.modified", "false"),
            ("vcs.time", "2026-11-16T00:00:00Z"),
        ]
    )
    stdout = f'{{"GoVersion": "go1.27.3", "Settings": [{settings}]}}'
    return subprocess.CompletedProcess([], 0, stdout=stdout)


def make_input(directory: str, target: str, commit: str = SHA, name: str | None = None) -> Path:
    label = name or target
    root = Path(directory) / "build-roots" / label
    binary = root / "build" / target / common.executable_name(target)
    binary.parent.mkdir(parents=True, exist_ok=True)
    binary.write_bytes(f"binary-{label}-{commit[:1]}".encode())
    out = Path(directory) / "packages" / label

    def run(
        command: list[str], **_: str | int | bool | Path | dict[str, str]
    ) -> subprocess.CompletedProcess[str]:
        if command[0] == "hdiutil":
            Path(command[-1]).write_bytes(b"test-dmg")
        elif command[0].endswith("ISCC.exe"):
            archive = out / common.archive_name(common.studio_version(), target)
            archive.write_bytes(b"test-installer")
        return fake_run(target)

    with patch("package.subprocess.run", side_effect=run):
        package_target(target, commit, out, root=root)
    return out


def make_candidate(directory: str, commit: str = SHA) -> tuple[Path, dict[str, common.JsonData]]:
    inputs = [make_input(directory, target, commit) for target in common.TARGETS]
    candidate = Path(directory) / "candidate"
    result = prepare(inputs, candidate, commit)
    return candidate, result


def rewrite_receipt(receipt: Path, mutate: Callable[[dict[str, common.JsonData]], None]) -> None:
    data = json.loads(receipt.read_text())
    mutate(data)
    with receipt.open("w", encoding="utf-8") as stream:
        json.dump(data, stream, indent=2)


def rewrite_manifest(candidate: Path, mutate: Callable[[dict[str, common.JsonData]], None]) -> str:
    path = candidate / "studio-release-manifest.json"
    data = json.loads(path.read_text())
    mutate(data)
    with path.open("w", encoding="utf-8") as stream:
        json.dump(data, stream, indent=2)
    return common.sha(path.read_bytes())


class PrepareReleaseCandidateTests(unittest.TestCase):
    def test_prepares_manifest_checksums_and_reports_manifest_sha(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            candidate, result = make_candidate(directory)
            version = common.studio_version()
            self.assertEqual(
                sorted(p.name for p in candidate.iterdir()),
                sorted(
                    [common.archive_name(version, t) for t in common.TARGETS]
                    + ["studio-release-manifest.json", common.CHECKSUMS_NAME]
                ),
            )
            manifest_path = candidate / "studio-release-manifest.json"
            self.assertEqual(result["manifest_sha256"], common.sha(manifest_path.read_bytes()))
            manifest = json.loads(manifest_path.read_text())
            self.assertEqual(manifest["kind"], "studio-release-candidate.v1")
            self.assertEqual(manifest["version"], version)
            self.assertEqual(manifest["source_commit"], SHA)
            self.assertIs(manifest["source_dirty"], False)
            self.assertEqual([a["target"] for a in manifest["artifacts"]], list(common.TARGETS))
            for artifact in manifest["artifacts"]:
                raw = (candidate / artifact["path"]).read_bytes()
                self.assertEqual(artifact["bytes"], len(raw))
                self.assertEqual(artifact["sha256"], common.sha(raw))
                self.assertEqual(artifact["build_info"]["vcs_revision"], SHA)
                self.assertEqual(artifact["build_info"]["vcs_modified"], False)
            lines = (candidate / common.CHECKSUMS_NAME).read_text().splitlines()
            self.assertEqual(len(lines), 3)
            for artifact, line in zip(manifest["artifacts"], lines):
                self.assertEqual(line, f"{artifact['sha256']}  {artifact['path']}")

    def test_requires_exactly_three_inputs(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            inputs = [make_input(directory, t) for t in common.TARGETS[:2]]
            with self.assertRaises(ValueError):
                prepare(inputs, Path(directory) / "candidate", SHA)

    def test_rejects_duplicate_targets(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            inputs = [
                make_input(directory, "darwin-arm64", name="darwin-a"),
                make_input(directory, "darwin-arm64", name="darwin-b"),
                make_input(directory, "linux-amd64"),
            ]
            with self.assertRaises(ValueError):
                prepare(inputs, Path(directory) / "candidate", SHA)

    def test_rejects_tampered_archive_before_writing(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            inputs = [make_input(directory, t) for t in common.TARGETS]
            archive = inputs[0] / common.archive_name(common.studio_version(), "darwin-arm64")
            archive.write_bytes(archive.read_bytes() + b"tamper")
            with self.assertRaises(ValueError):
                prepare(inputs, Path(directory) / "candidate", SHA)
            self.assertFalse((Path(directory) / "candidate").exists())

    def test_rejects_wrong_archive_name_before_writing(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            inputs = [make_input(directory, t) for t in common.TARGETS]
            archive = inputs[0] / common.archive_name(common.studio_version(), "darwin-arm64")
            renamed = inputs[0] / "unexpected.tar.gz"
            archive.rename(renamed)
            receipt = inputs[0] / "studio-package-darwin-arm64.json"

            def rename_receipt(data: dict[str, common.JsonData]) -> None:
                data["archive"]["path"] = renamed.name

            rewrite_receipt(receipt, rename_receipt)
            candidate = Path(directory) / "candidate"
            with self.assertRaises(ValueError):
                prepare(inputs, candidate, SHA)
            self.assertFalse(candidate.exists())

    def test_rejects_inconsistent_source_commit(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            inputs = [make_input(directory, t) for t in common.TARGETS]
            receipt = inputs[0] / "studio-package-darwin-arm64.json"

            def wrong_commit(data: dict[str, common.JsonData]) -> None:
                data["source_commit"] = OTHER_SHA

            rewrite_receipt(receipt, wrong_commit)
            with self.assertRaises(ValueError):
                prepare(inputs, Path(directory) / "candidate", SHA)

    def test_rejects_version_mismatch_across_receipts(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            inputs = [make_input(directory, t) for t in common.TARGETS]
            receipt = inputs[0] / "studio-package-darwin-arm64.json"

            def wrong_version(data: dict[str, common.JsonData]) -> None:
                data["version"] = "9.9.9"

            rewrite_receipt(receipt, wrong_version)
            with self.assertRaises(ValueError):
                prepare(inputs, Path(directory) / "candidate", SHA)

    def test_refuses_existing_output(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            inputs = [make_input(directory, t) for t in common.TARGETS]
            candidate = Path(directory) / "candidate"
            candidate.mkdir()
            with self.assertRaises(FileExistsError):
                prepare(inputs, candidate, SHA)


class VerifyReleaseCandidateTests(unittest.TestCase):
    def run_main(self, candidate: Path, manifest_sha: str, stage: Path, commit: str = SHA) -> None:
        argv = [
            "verify_release_candidate.py",
            "--manifest", str(candidate / "studio-release-manifest.json"),
            "--manifest-sha256", manifest_sha,
            "--source-commit", commit,
            "--stage", str(stage),
        ]
        with patch("sys.argv", argv):
            verify_release_candidate.main()

    def test_stages_only_archives_and_checksums(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            candidate, result = make_candidate(directory)
            version = common.studio_version()
            stage = Path(directory) / "stage"
            self.run_main(candidate, result["manifest_sha256"], stage)
            names = [common.archive_name(version, t) for t in common.TARGETS]
            self.assertEqual(
                sorted(p.name for p in stage.iterdir()),
                sorted(names + [common.CHECKSUMS_NAME]),
            )
            for name in names:
                self.assertEqual((stage / name).read_bytes(), (candidate / name).read_bytes())
            self.assertEqual(
                (stage / common.CHECKSUMS_NAME).read_bytes(),
                (candidate / common.CHECKSUMS_NAME).read_bytes(),
            )

    def test_verify_returns_ordered_checked_bytes(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            candidate, result = make_candidate(directory)
            ordered, checksums_raw = verify(
                candidate / "studio-release-manifest.json", result["manifest_sha256"], SHA
            )
            self.assertEqual(
                [f.name for f in ordered],
                [common.archive_name(common.studio_version(), t) for t in common.TARGETS],
            )
            self.assertEqual(checksums_raw, (candidate / common.CHECKSUMS_NAME).read_bytes())

    def test_rejects_bad_manifest_sha(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            candidate, _ = make_candidate(directory)
            manifest = candidate / "studio-release-manifest.json"
            for expected in ["c" * 64, "not-hex"]:
                with self.subTest(expected=expected), self.assertRaises(ValueError):
                    verify(manifest, expected, SHA)

    def test_rejects_wrong_source_commit(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            candidate, result = make_candidate(directory)
            with self.assertRaises(ValueError):
                verify(
                    candidate / "studio-release-manifest.json",
                    result["manifest_sha256"],
                    OTHER_SHA,
                )

    def test_rejects_tampered_archive(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            candidate, result = make_candidate(directory)
            archive = candidate / common.archive_name(common.studio_version(), "darwin-arm64")
            archive.write_bytes(archive.read_bytes() + b"tamper")
            with self.assertRaises(ValueError):
                verify(candidate / "studio-release-manifest.json", result["manifest_sha256"], SHA)

    def test_rejects_missing_archive(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            candidate, result = make_candidate(directory)
            (candidate / common.archive_name(common.studio_version(), "linux-amd64")).unlink()
            with self.assertRaises(ValueError):
                verify(candidate / "studio-release-manifest.json", result["manifest_sha256"], SHA)

    def test_rejects_duplicate_target_in_manifest(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            candidate, _ = make_candidate(directory)
            digest = rewrite_manifest(candidate, lambda data: data["artifacts"].append(dict(data["artifacts"][0])))
            with self.assertRaises(ValueError):
                verify(candidate / "studio-release-manifest.json", digest, SHA)

    def test_rejects_path_escape_in_manifest(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            candidate, _ = make_candidate(directory)

            def escape(data: dict[str, common.JsonData]) -> None:
                data["artifacts"][0]["path"] = "../escape.tar.gz"

            digest = rewrite_manifest(candidate, escape)
            with self.assertRaises(ValueError):
                verify(candidate / "studio-release-manifest.json", digest, SHA)

    def test_rejects_symlink_archive(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            candidate, result = make_candidate(directory)
            archive = candidate / common.archive_name(common.studio_version(), "darwin-arm64")
            raw = archive.read_bytes()
            archive.unlink()
            outside = Path(directory) / "outside.bin"
            outside.write_bytes(raw)
            archive.symlink_to(outside)
            with self.assertRaises(ValueError):
                verify(candidate / "studio-release-manifest.json", result["manifest_sha256"], SHA)

    def test_rejects_wrong_archive_name(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            candidate, _ = make_candidate(directory)
            archive = candidate / common.archive_name(common.studio_version(), "darwin-arm64")
            renamed = candidate / "unexpected.tar.gz"
            archive.rename(renamed)

            def rename_artifact(data: dict[str, common.JsonData]) -> None:
                data["artifacts"][0]["path"] = renamed.name

            digest = rewrite_manifest(candidate, rename_artifact)
            lines = (candidate / common.CHECKSUMS_NAME).read_text().splitlines()
            lines[0] = f"{common.sha(renamed.read_bytes())}  {renamed.name}"
            (candidate / common.CHECKSUMS_NAME).write_text("\n".join(lines) + "\n")
            with self.assertRaises(ValueError):
                verify(candidate / "studio-release-manifest.json", digest, SHA)

    def test_rejects_symlink_checksums(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            candidate, result = make_candidate(directory)
            checksums = candidate / common.CHECKSUMS_NAME
            outside = Path(directory) / "outside-checksums"
            outside.write_bytes(checksums.read_bytes())
            checksums.unlink()
            checksums.symlink_to(outside)
            with self.assertRaises(ValueError):
                verify(candidate / "studio-release-manifest.json", result["manifest_sha256"], SHA)

    def test_rejects_bad_build_info(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            candidate, _ = make_candidate(directory)

            def dirty(data: dict[str, common.JsonData]) -> None:
                data["artifacts"][0]["build_info"]["vcs_modified"] = True

            digest = rewrite_manifest(candidate, dirty)
            with self.assertRaises(ValueError):
                verify(candidate / "studio-release-manifest.json", digest, SHA)

    def test_rejects_bad_checksums_format(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            candidate, result = make_candidate(directory)
            text = (candidate / common.CHECKSUMS_NAME).read_text()
            (candidate / common.CHECKSUMS_NAME).write_text(text.replace("  ", " "))
            with self.assertRaises(ValueError):
                verify(candidate / "studio-release-manifest.json", result["manifest_sha256"], SHA)

    def test_rejects_checksums_in_wrong_order(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            candidate, result = make_candidate(directory)
            lines = (candidate / common.CHECKSUMS_NAME).read_text().splitlines()
            lines[0], lines[1] = lines[1], lines[0]
            (candidate / common.CHECKSUMS_NAME).write_text("\n".join(lines) + "\n")
            with self.assertRaises(ValueError):
                verify(candidate / "studio-release-manifest.json", result["manifest_sha256"], SHA)

    def test_refuses_existing_stage(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            candidate, result = make_candidate(directory)
            stage = Path(directory) / "stage"
            stage.mkdir()
            with self.assertRaises(FileExistsError):
                self.run_main(candidate, result["manifest_sha256"], stage)


_TEST_METHODS = (
    PrepareReleaseCandidateTests.test_prepares_manifest_checksums_and_reports_manifest_sha,
    PrepareReleaseCandidateTests.test_requires_exactly_three_inputs,
    PrepareReleaseCandidateTests.test_rejects_duplicate_targets,
    PrepareReleaseCandidateTests.test_rejects_tampered_archive_before_writing,
    PrepareReleaseCandidateTests.test_rejects_wrong_archive_name_before_writing,
    PrepareReleaseCandidateTests.test_rejects_inconsistent_source_commit,
    PrepareReleaseCandidateTests.test_rejects_version_mismatch_across_receipts,
    PrepareReleaseCandidateTests.test_refuses_existing_output,
    VerifyReleaseCandidateTests.test_stages_only_archives_and_checksums,
    VerifyReleaseCandidateTests.test_verify_returns_ordered_checked_bytes,
    VerifyReleaseCandidateTests.test_rejects_bad_manifest_sha,
    VerifyReleaseCandidateTests.test_rejects_wrong_source_commit,
    VerifyReleaseCandidateTests.test_rejects_tampered_archive,
    VerifyReleaseCandidateTests.test_rejects_missing_archive,
    VerifyReleaseCandidateTests.test_rejects_duplicate_target_in_manifest,
    VerifyReleaseCandidateTests.test_rejects_path_escape_in_manifest,
    VerifyReleaseCandidateTests.test_rejects_symlink_archive,
    VerifyReleaseCandidateTests.test_rejects_wrong_archive_name,
    VerifyReleaseCandidateTests.test_rejects_symlink_checksums,
    VerifyReleaseCandidateTests.test_rejects_bad_build_info,
    VerifyReleaseCandidateTests.test_rejects_bad_checksums_format,
    VerifyReleaseCandidateTests.test_rejects_checksums_in_wrong_order,
    VerifyReleaseCandidateTests.test_refuses_existing_stage,
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
