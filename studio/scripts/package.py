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
        instructions += 'Run: open "InferYard Studio.app" (macOS 12+).\nNative UI uses Metal; report windows use the system WKWebView. Desktop notifications require the .app and notification permission.\n'
    elif target.startswith('linux'):
        instructions += 'Run: ./inferyard-studio\nNative UI: GTK 3 with OpenGL rendering. Report windows: WebKitGTK 4.1 (4.0 fallback). Desktop notifications need a notification service.\n'
    else:
        instructions += 'Run: inferyard-studio.exe\nRequires Windows 10/11; native UI uses Direct3D 11. Report windows need Microsoft WebView2 Runtime (included with Windows 11).\n'
    instructions += '\nConfigure the separately installed inferyard CLI and data directories in Settings.\nDesktop application with MyGo native UI and separate WebView windows for offline HTML reports.\nFirst launch opens the setup wizard. Start model services yourself in a terminal.\nUnsigned build; no notarization or publication.\n'
    (directory / 'README.txt').write_text(instructions)
    print(f'{directory.relative_to(root)}: packaged')
