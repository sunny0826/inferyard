"""Acquisition child test entry point: every HTTP request is redirected to IPv4 loopback."""

import sys

import httpx

from inferyard.platforms import model_source_worker as worker
from inferyard.platforms.model_source_download import transfer


class LoopbackTransport(httpx.BaseTransport):
    def __init__(self, port):
        self.port = port
        self.local = httpx.HTTPTransport(retries=0)

    def handle_request(self, request):
        local = httpx.Request(
            request.method,
            f"http://127.0.0.1:{self.port}{request.url.path}",
            headers={"Accept-Encoding": "identity"},
            extensions=request.extensions,
        )
        return self.local.handle_request(local)

    def close(self):
        self.local.close()


def local_transfer(*args, **kwargs):
    with httpx.Client(transport=LoopbackTransport(int(sys.argv[1])), trust_env=False) as client:
        return transfer(*args, client=client, **kwargs)


if __name__ == "__main__":
    worker.transfer = local_transfer
    worker.main()
