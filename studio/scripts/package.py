"""Assemble unsigned distributions and strict single-target release packages."""

import argparse
import json
import plistlib
import shutil
import subprocess
import tarfile
import tempfile
from pathlib import Path

import release_common as common

LICENSE_SOURCE = Path(__file__).resolve().parents[2] / "LICENSE"


def build_info(executable: Path) -> dict[str, common.JsonData]:
    result = subprocess.run(
        ["go", "version", "-m", "-json", str(executable)],
        check=True, capture_output=True, text=True,
    )
    data = json.loads(result.stdout)
    settings = {item["Key"]: item["Value"] for item in data.get("Settings", [])}
    return {
        "go_version": data.get("GoVersion"),
        "goos": settings.get("GOOS"),
        "goarch": settings.get("GOARCH"),
        "tags": sorted(settings.get("-tags", "").replace(",", " ").split()),
        "vcs_modified": settings.get("vcs.modified") == "true",
        "vcs_revision": settings.get("vcs.revision"),
        "vcs_time": settings.get("vcs.time"),
    }


def require_release_build(executable: Path) -> None:
    info = build_info(executable)
    if "release" not in info["tags"]:
        raise ValueError(f"Refusing to package {executable}: rebuild with -tags release")


def require_verified_build(executable: Path, source_commit: str, target: str) -> dict[str, common.JsonData]:
    info = build_info(executable)
    common.validate_build_info(info, source_commit, target)
    return info


def assemble_distribution(directory: Path, version: str, target: str, binary: Path) -> None:
    executable = common.executable_name(target)
    if not LICENSE_SOURCE.is_file():
        raise ValueError("license_missing")
    directory.mkdir(parents=True)
    shutil.copy2(binary, directory / executable)
    shutil.copy2(LICENSE_SOURCE, directory / "LICENSE")
    instructions = f"InferYard Studio {version} ({target})\n\n"
    if target.startswith("darwin"):
        bundle = directory / "InferYard Studio.app" / "Contents"
        (bundle / "MacOS").mkdir(parents=True)
        shutil.copy2(directory / executable, bundle / "MacOS" / executable)
        with (bundle / "Info.plist").open("wb") as output:
            plistlib.dump(dict(CFBundleExecutable=executable, CFBundleIdentifier="com.inferyard.studio",
                               CFBundleName="InferYard Studio", CFBundleDisplayName="InferYard Studio",
                               CFBundlePackageType="APPL", CFBundleVersion=version,
                               CFBundleShortVersionString=version, LSMinimumSystemVersion="12.0",
                               NSHighResolutionCapable=True, NSPrincipalClass="NSApplication"), output)
        instructions += 'Run: open "InferYard Studio.app" (macOS 12+).\nNative UI uses Metal. Desktop notifications require the .app and notification permission.\n'
    elif target.startswith("linux"):
        instructions += "Run: ./inferyard-studio\nNative UI: GTK 3 with OpenGL rendering. Desktop notifications need a notification service.\n"
    else:
        instructions += "Run: inferyard-studio.exe\nRequires Windows 10/11; native UI uses Direct3D 11.\n"
    instructions += (
        "\nConfigure the separately installed inferyard CLI and data directories in Settings.\n"
        "Desktop application with MyGo native UI and reports rendered from studio.json in the main window.\n"
        "Guidance is embedded in the main window. Studio starts and manages the frozen model engine, stops its owned engine on shutdown, and never restarts it automatically.\n"
        "Unsigned build; not notarized.\n"
    )
    (directory / "README.txt").write_text(instructions)


def make_dmg(archive: Path, distribution: Path) -> None:
    shortcut = distribution / "Applications"
    shortcut.symlink_to("/Applications")
    try:
        subprocess.run([
            "hdiutil", "create", "-fs", "HFS+", "-format", "UDZO",
            "-volname", "InferYard Studio", "-srcfolder", str(distribution), str(archive),
        ], check=True)
    finally:
        shortcut.unlink()


def inno_path(path: Path) -> str:
    return str(path.resolve()).replace("/", "\\")


def make_windows_installer(archive: Path, distribution: Path) -> None:
    version = common.studio_version()
    executable = common.executable_name("windows-amd64")
    script = f"""[Setup]
AppName=InferYard Studio
AppVersion={version}
AppVerName=InferYard Studio {version}
AppPublisher=Xudong Guo
DefaultDirName={{autopf}}\\InferYard Studio
DefaultGroupName=InferYard Studio
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=dialog
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
OutputDir={inno_path(archive.parent)}
OutputBaseFilename={archive.stem}
LicenseFile={inno_path(distribution / "LICENSE")}
InfoBeforeFile={inno_path(distribution / "README.txt")}
UninstallDisplayName=InferYard Studio
UninstallDisplayIcon={{app}}\\inferyard-studio.exe
VersionInfoVersion={version}.0
VersionInfoCompany=Xudong Guo
VersionInfoDescription=InferYard Studio Installer
VersionInfoProductName=InferYard Studio
VersionInfoProductVersion={version}.0
DisableProgramGroupPage=yes
WizardStyle=modern
Compression=lzma2
SolidCompression=yes

[Files]
Source: "{inno_path(distribution / executable)}"; DestDir: "{{app}}"; Flags: ignoreversion
Source: "{inno_path(distribution / "LICENSE")}"; DestDir: "{{app}}"; Flags: ignoreversion
Source: "{inno_path(distribution / "README.txt")}"; DestDir: "{{app}}"; Flags: ignoreversion

[Icons]
Name: "{{group}}\\InferYard Studio"; Filename: "{{app}}\\inferyard-studio.exe"

[UninstallDelete]
Type: filesandordirs; Name: "{{app}}"
"""
    with tempfile.TemporaryDirectory() as directory:
        script_path = Path(directory) / "installer.iss"
        script_path.write_text(script, encoding="utf-8-sig")
        subprocess.run([
            r"C:\Program Files (x86)\Inno Setup 6\ISCC.exe", "/Qp", str(script_path)
        ], check=True)


def make_archive(archive: Path, distribution: Path) -> None:
    if archive.suffix == ".dmg":
        make_dmg(archive, distribution)
    elif archive.suffix == ".exe":
        make_windows_installer(archive, distribution)
    else:
        with tarfile.open(archive, "x:gz") as bundle:
            bundle.add(distribution, arcname=distribution.name)


def package(root: Path) -> None:
    """Development entry used by build-all.sh: preflight everything, then write dist/."""
    root = Path(root)
    for target in common.TARGETS:
        require_release_build(root / "build" / target / common.executable_name(target))
    version = common.studio_version()
    directories = [
        root / "dist" / common.distribution_name(version, target) for target in common.TARGETS
    ]
    existing = [str(directory) for directory in directories if directory.exists()]
    if existing:
        raise FileExistsError(
            "distribution already exists; remove dist/ outputs first: " + ", ".join(existing)
        )
    for target, directory in zip(common.TARGETS, directories):
        assemble_distribution(
            directory, version, target, root / "build" / target / common.executable_name(target)
        )
        print(f"{directory.relative_to(root)}: packaged")


def package_target(target: str, source_commit: str, out: Path, root: Path = common.STUDIO_ROOT) -> dict[str, common.JsonData]:
    """Package one verified release target into a new output directory with a receipt."""
    if target not in common.TARGETS:
        raise ValueError(f"unknown target: {target}")
    common.require_source_commit(source_commit)
    binary = Path(root) / "build" / target / common.executable_name(target)
    if not binary.is_file():
        raise ValueError(f"build output missing: {binary}")
    info = require_verified_build(binary, source_commit, target)
    version = common.studio_version()
    out.mkdir(parents=True)
    try:
        distribution = common.distribution_name(version, target)
        assemble_distribution(out / distribution, version, target, binary)
        archive = out / common.archive_name(version, target)
        make_archive(archive, out / distribution)
        archive_raw = archive.read_bytes()
        binary_raw = (out / distribution / common.executable_name(target)).read_bytes()
        receipt = {
            "kind": common.PACKAGE_KIND,
            "version": version,
            "source_commit": source_commit,
            "source_dirty": False,
            "target": target,
            "archive": common.file_record(archive.name, archive_raw),
            "binary": common.file_record(
                f"{distribution}/{common.executable_name(target)}", binary_raw
            ),
            "build_info": info,
        }
        with (out / f"studio-package-{target}.json").open("x", encoding="utf-8") as stream:
            json.dump(receipt, stream, indent=2)
            stream.write("\n")
    except BaseException:
        shutil.rmtree(out, ignore_errors=True)
        raise
    return receipt


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--target", choices=common.TARGETS)
    parser.add_argument("--source-commit", help="full source SHA the build must match")
    parser.add_argument("--out", type=Path, help="new output directory for the package")
    args = parser.parse_args()
    if args.target is None:
        package(Path(__file__).resolve().parent.parent)
        return
    if args.out is None:
        parser.error("--out is required with --target")
    receipt = package_target(args.target, args.source_commit, args.out)
    print(json.dumps({
        "receipt": str(args.out / f"studio-package-{args.target}.json"),
        "target": args.target,
        "version": receipt["version"],
        "source_commit": receipt["source_commit"],
    }))


if __name__ == "__main__":
    main()
