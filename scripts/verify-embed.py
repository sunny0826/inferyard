"""Verify the built macOS binary serves assets with web/dist absent."""
from pathlib import Path
import re
import signal
import subprocess
import time
import urllib.error
import urllib.request

root = Path(__file__).resolve().parent.parent
assets = root / 'web/dist'
hidden = root / 'web/dist.verify-backup'
log_path = root / 'docs/evidence/embed.raw.log'
report = root / 'docs/evidence/embed.log'
assert not hidden.exists(), 'Previous verification backup exists'
assets.rename(hidden)
process = None
try:
    with log_path.open('w') as log:
        process = subprocess.Popen(
            [str(root / 'build/darwin-arm64/inferyard-studio'), '-headless'],
            cwd=root / 'build', stdout=log, stderr=log,
        )
        for _ in range(100):
            match = re.search(r'STUDIO_URL=(\S+)', log_path.read_text())
            if match:
                break
            if process.poll() is not None:
                raise RuntimeError('Server exited before publishing URL')
            time.sleep(.1)
        else:
            raise TimeoutError('Server did not publish URL')
        url = match.group(1)
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        with opener.open(url, timeout=5) as response:
            html = response.read().decode()
            assert response.status == 200 and 'id="app"' in html
        asset_paths = re.findall(r'(?:src|href)="(/assets/[^\"]+)"', html)
        assert len(asset_paths) >= 2
        lines = [
            'web/dist absent during verification: true',
            'Vite dev server: not started',
            'working directory: build/',
            'binary: build/darwin-arm64/inferyard-studio',
            'GET /?token=<redacted>: HTTP 200; app root present',
        ]
        base = url.split('/?')[0]
        for asset in asset_paths:
            with opener.open(base + asset, timeout=5) as response:
                data = response.read()
                assert response.status == 200 and data
                lines.append(f'GET {asset}: HTTP 200; {len(data)} bytes')
        try:
            opener.open(base + '/', timeout=5)
            raise AssertionError('Unauthenticated homepage was accepted')
        except urllib.error.HTTPError as error:
            assert error.code == 401
            lines.append('GET / without token: HTTP 401')
        process.send_signal(signal.SIGINT)
        code = process.wait(timeout=5)
        assert code == 0
        lines.append(f'headless process exit={code}')
        report.write_text('\n'.join(lines) + '\n')
        print(report.read_text())
finally:
    if process is not None and process.poll() is None:
        process.kill()
        process.wait()
    hidden.rename(assets)
