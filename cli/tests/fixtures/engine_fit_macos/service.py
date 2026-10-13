"""Owned native process/TCP fixture; no engine package, weights, or model inference."""

import argparse
import json
import subprocess
import sys
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--engine", choices=("vllm", "sglang"), required=True)
    parser.add_argument("--model")
    parser.add_argument("--model-path")
    parser.add_argument("--record", type=Path, required=True)
    parser.add_argument("--port", type=int, default=0)
    parser.add_argument("--reuse-port", action="store_true")
    args = parser.parse_args()

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def respond(self, value, content_type="application/json"):
            raw = value if isinstance(value, bytes) else json.dumps(value).encode()
            self.send_response(200)
            self.send_header("Content-Type", content_type)
            self.send_header("Content-Length", str(len(raw)))
            self.end_headers()
            self.wfile.write(raw)

        def do_GET(self):
            if self.path == "/v1/models":
                self.respond({"data": [{"id": "native-macos-fixture"}]})
            elif self.path == {"vllm": "/version", "sglang": "/get_server_info"}[args.engine]:
                self.respond({"version": "0.0.1+native-fixture"})
            elif self.path == "/metrics":
                names = {
                    "vllm": ("num_requests_running", "num_requests_waiting"),
                    "sglang": ("num_running_reqs", "num_queue_reqs"),
                }[args.engine]
                metrics = "\n".join(f"{args.engine}:{name} 0" for name in names) + "\n"
                self.respond(metrics.encode(), "text/plain")
            else:
                self.send_error(404)

        def do_POST(self):
            if self.path != "/v1/chat/completions":
                self.send_error(404)
                return
            body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            with args.record.open("a", encoding="utf-8") as handle:
                handle.write(json.dumps(body) + "\n")
            events = [
                {"choices": [{"index": 0, "delta": {"content": "fixture <script>x</script>"}}]},
                {"choices": [{"index": 0, "delta": {}, "finish_reason": "stop"}]},
                {"choices": [], "usage": {"prompt_tokens": 5, "completion_tokens": 2}},
            ]
            raw = "".join("data: " + json.dumps(event) + "\n\n" for event in events)
            self.respond((raw + "data: [DONE]\n\n").encode(), "text/event-stream")

    child = subprocess.Popen(
        [
            sys.executable,
            "-u",
            "-c",
            "import sys; data = bytearray(8 * 2**20); print('ready', flush=True); sys.stdin.read()",
        ],
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        text=True,
    )
    server = None
    thread = None
    try:
        assert child.stdout.readline().strip() == "ready"

        class Server(ThreadingHTTPServer):
            allow_reuse_port = args.reuse_port

        server = Server(("127.0.0.1", args.port), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        print(json.dumps({"port": server.server_port, "child_pid": child.pid}), flush=True)
        for command in sys.stdin:
            if command.strip() == "close":
                server.shutdown()
                server.server_close()
                print("closed", flush=True)
            else:
                break
    finally:
        if server is not None:
            server.shutdown()
            server.server_close()
        if thread is not None:
            thread.join(timeout=5)
        child.communicate(timeout=5)


if __name__ == "__main__":
    main()
