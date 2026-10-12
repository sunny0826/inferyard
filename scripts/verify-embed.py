"""Verify a built binary without web/dist; private session token never enters logs."""
from pathlib import Path
import argparse
import os
import re
import select
import signal
import subprocess
import tempfile
import urllib.error
import urllib.request

parser = argparse.ArgumentParser()
parser.add_argument('--binary', type=Path, default=Path('build/darwin-arm64/inferyard-studio'))
args = parser.parse_args()
root = Path(__file__).resolve().parent.parent
binary = args.binary.resolve()
assets = root / 'web/dist'
hidden = root / 'web/dist.verify-backup'
assert binary.is_file()
assert not hidden.exists(), 'Previous verification backup exists'
process = None
read_fd, write_fd = os.pipe()
assets.rename(hidden)
try:
    with tempfile.TemporaryDirectory(prefix='studio-embed-') as cwd:
        with tempfile.TemporaryFile(mode='w+') as log:
            process = subprocess.Popen(
                [str(binary), '-headless', '-session-fd', str(write_fd)],
                cwd=cwd, stdout=log, stderr=log, pass_fds=(write_fd,),
            )
            os.close(write_fd)
            write_fd = -1
            if not select.select([read_fd], [], [], 10)[0]:
                raise TimeoutError('Server did not publish session to private pipe')
            url = os.read(read_fd, 4096).decode().strip()
            assert url.startswith('http://127.0.0.1:')
            opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
            with opener.open(url, timeout=5) as response:
                html = response.read().decode()
                assert response.status == 200 and 'id="app"' in html
                assert response.headers['Referrer-Policy'] == 'no-referrer'
            asset_paths = re.findall(r'(?:src|href)="(/assets/[^\"]+)"', html)
            assert len(asset_paths) >= 2
            print('web/dist absent; empty working directory; no Vite server')
            print('Authenticated embedded entry: HTTP 200')
            base = url.split('/?')[0]
            for asset in asset_paths:
                with opener.open(base + asset, timeout=5) as response:
                    data = response.read()
                    assert response.status == 200 and data
                    print(f'Embedded asset: HTTP 200; {len(data)} bytes')
            try:
                opener.open(base + '/', timeout=5)
                raise AssertionError('Unauthenticated homepage was accepted')
            except urllib.error.HTTPError as error:
                assert error.code == 401
                print('Unauthenticated entry: HTTP 401')
            process.send_signal(signal.SIGINT)
            code = process.wait(timeout=5)
            assert code == 0
            log.seek(0)
            assert url.split('token=')[1] not in log.read()
            print('Session token absent from logs; process exit=0')
finally:
    if process is not None and process.poll() is None:
        process.kill()
        process.wait()
    os.close(read_fd)
    if write_fd >= 0:
        os.close(write_fd)
    hidden.rename(assets)
