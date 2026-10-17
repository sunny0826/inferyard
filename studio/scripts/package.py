"""Assemble unsigned distributions from scripts/build-all.sh outputs."""
from pathlib import Path
import json
import plistlib
import re
import shutil
import subprocess


def require_release_build(executable: Path) -> None:
    result = subprocess.run(
        ["go", "version", "-m", "-json", str(executable)],
        check=True, capture_output=True, text=True,
    )
    info = json.loads(result.stdout)
    settings = {item["Key"]: item["Value"] for item in info.get("Settings", [])}
    if "release" not in settings.get("-tags", "").replace(",", " ").split():
        raise ValueError(f"Refusing to package {executable}: rebuild with -tags release")


def package(root: Path) -> None:
    for target in ('darwin-arm64', 'linux-amd64', 'windows-amd64'):
        executable = 'inferyard-studio' + ('.exe' if target.startswith('windows') else '')
        require_release_build(root / 'build' / target / executable)
    version, = re.findall(r'^\s*version:\s*[\'\"]([^\'\"]+)', (root / 'mygo.config.ts').read_text(), re.M)
    for target in ('darwin-arm64', 'linux-amd64', 'windows-amd64'):
        directory = root / 'dist' / f'inferyard-studio-v{version}-{target}'
        directory.mkdir(parents=True, exist_ok=True)
        executable = 'inferyard-studio' + ('.exe' if target.startswith('windows') else '')
        shutil.copy2(root / 'build' / target / executable, directory / executable)
        instructions = f'InferYard Studio {version} ({target})\n\n'
        if target.startswith('darwin'):
            bundle = directory / 'InferYard Studio.app' / 'Contents'
            (bundle / 'MacOS').mkdir(parents=True, exist_ok=True)
            shutil.copy2(directory / executable, bundle / 'MacOS' / executable)
            with (bundle / 'Info.plist').open('wb') as output:
                plistlib.dump(dict(CFBundleExecutable=executable, CFBundleIdentifier='com.inferyard.studio',
                                  CFBundleName='InferYard Studio', CFBundleDisplayName='InferYard Studio',
                                  CFBundlePackageType='APPL', CFBundleVersion=version,
                                  CFBundleShortVersionString=version, LSMinimumSystemVersion='12.0',
                                  NSHighResolutionCapable=True), output)
            instructions += 'Run: open "InferYard Studio.app" (macOS 12+).\nNative UI uses Metal. Desktop notifications require the .app and notification permission.\n'
        elif target.startswith('linux'):
            instructions += 'Run: ./inferyard-studio\nNative UI: GTK 3 with OpenGL rendering. Desktop notifications need a notification service.\n'
        else:
            instructions += 'Run: inferyard-studio.exe\nRequires Windows 10/11; native UI uses Direct3D 11.\n'
        instructions += '\nConfigure the separately installed inferyard CLI and data directories in Settings.\nDesktop application with MyGo native UI and reports rendered from index.json in the main window.\nGuidance is embedded in the main window. Studio starts and manages the frozen model engine, stops its owned engine on shutdown, and never restarts it automatically.\nUnsigned build; no notarization or publication.\n'
        (directory / 'README.txt').write_text(instructions)
        print(f'{directory.relative_to(root)}: packaged')


if __name__ == "__main__":
    package(Path(__file__).resolve().parent.parent)
