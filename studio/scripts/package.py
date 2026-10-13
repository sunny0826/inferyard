"""Assemble unsigned distributions from scripts/build-all.sh outputs."""
from pathlib import Path
import plistlib
import re
import shutil

root = Path(__file__).resolve().parent.parent
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
        instructions += 'Run: open "InferYard Studio.app" (macOS 12+).\nDesktop notifications require the .app and notification permission.\n'
    elif target.startswith('linux'):
        instructions += 'Run: ./inferyard-studio\nRequires GTK 3 and WebKitGTK 4.1 (or 4.0), plus a desktop notification service.\n'
    else:
        instructions += 'Run: inferyard-studio.exe\nRequires Windows 10/11 and Microsoft WebView2 Runtime.\n'
    instructions += '\nConfigure the separately installed inferyard CLI and data directories in Settings.\nFrontend assets are embedded; no Bun or Vite server is needed.\nUse -headless for the local HTTP server without a window or notifications.\nSession URLs are available only through the private -session-fd pipe; see the repository README.\nUnsigned build; no notarization or publication.\n'
    (directory / 'README.txt').write_text(instructions)
    print(f'{directory.relative_to(root)}: packaged')
