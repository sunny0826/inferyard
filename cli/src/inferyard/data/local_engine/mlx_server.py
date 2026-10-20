"""Operator-started local MLX formal server, standalone on Python 3.12+.

Loads the bundled diagnostic generation queue by absolute file location without
importing inferyard. Both protocols use the same greedy backend and chat template.
"""

import argparse
import hashlib
import importlib.util
import ipaddress
import json
import os
import queue
import re
import select
import socket
import sys
import time
import uuid
from pathlib import Path

PROTOCOL = "mlx-lm-formal.v1"
DEPENDENCY = Path(__file__).resolve().parent.parent / "engine_fit" / "mlx_server.py"
_spec = importlib.util.spec_from_file_location("inferyard_bundled_mlx_queue", DEPENDENCY)
legacy = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(legacy)
GenerationQueue = legacy.GenerationQueue


def validate_request(body, model):
    if type(body) is not dict or type(body.get("stream")) is not bool:
        raise ValueError("invalid_request")
    stream = body["stream"]
    if stream:
        prompt, tokens = legacy.validate_request(body, model)
    else:
        if "stream_options" in body:
            raise ValueError("invalid_request")
        prompt, tokens = legacy.validate_request(
            {**body, "stream": True, "stream_options": {"include_usage": True}}, model
        )
    return prompt, tokens, stream


def validate_context(value):
    if type(value) is not int or not 1 <= value <= 32768:
        raise ValueError("invalid_max_model_len")
    return value


class MLXBackend(legacy.MLXBackend):
    def __init__(self, model_path, max_model_len):
        super().__init__(model_path, max_model_len=validate_context(max_model_len))


class FormalServer(legacy.DiagnosticServer):
    def __init__(self, address, backend, served_model, api_key=None):
        validate_context(getattr(backend, "max_model_len", None))
        super().__init__(address, backend, served_model, api_key)
        self.RequestHandlerClass = Handler
        self.source_hash = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
        self.queue_source_hash = hashlib.sha256(DEPENDENCY.read_bytes()).hexdigest()


class Handler(legacy.Handler):
    def do_GET(self):
        if self.path != "/version":
            return super().do_GET()
        if not self._authorized():
            return self._reply(401, {"error": "authentication_required"})
        return self._reply(
            200,
            {
                "version": self.server.generations.backend.version,
                "local_engine_protocol": PROTOCOL,
                "local_engine_source_sha256": self.server.source_hash,
                "generation_queue_source_sha256": self.server.queue_source_hash,
                "synthetic": self.server.generations.backend.synthetic,
                "max_model_len": self.server.generations.backend.max_model_len,
            },
        )

    def _body(self):
        lengths = self.headers.get_all("Content-Length", [])
        if (
            self.headers.get("Transfer-Encoding") is not None
            or len(lengths) != 1
            or not lengths[0].isdigit()
            or not 1 <= int(lengths[0]) <= legacy.MAX_BODY_BYTES
            or self.headers.get_content_type() != "application/json"
        ):
            raise ValueError("invalid_request_body")
        raw = self.rfile.read(int(lengths[0]))
        if len(raw) != int(lengths[0]):
            raise ValueError("incomplete_request_body")
        body = json.loads(
            raw.decode(), object_pairs_hook=legacy._object, parse_constant=legacy._constant
        )
        return validate_request(body, self.server.served_model)

    def _check_connected(self):
        readable, _, _ = select.select([self.connection], [], [], 0)
        if readable and not self.connection.recv(1, socket.MSG_PEEK):
            raise ConnectionError("client_disconnected")

    def do_POST(self):
        if not self._authorized():
            return self._reply(401, {"error": "authentication_required"})
        if self.path != "/v1/chat/completions":
            return self._reply(404, {"error": "unknown_endpoint"})
        try:
            prompt, tokens, stream = self._body()
            job = self.server.generations.submit(prompt, tokens)
        except (ValueError, UnicodeError, TimeoutError) as exc:
            reason = (
                "context_length_exceeded"
                if str(exc) == "context_length_exceeded"
                else "invalid_request"
            )
            return self._reply(400, {"error": reason})
        except queue.Full:
            return self._reply(429, {"error": "generation_queue_full"})
        except RuntimeError:
            return self._reply(503, {"error": "service_poisoned"})
        completed = False
        try:
            if stream:
                self.send_response(200)
                self.send_header("Content-Type", "text/event-stream")
                self.send_header("Cache-Control", "no-cache")
                self.send_header("Connection", "close")
                self.end_headers()
            completed = self._respond(job, stream)
        except (OSError, TimeoutError):  # fmt: skip
            pass
        finally:
            if not completed:
                job.cancelled.set()
                # HTTP delivery can fail after the worker has already synchronized.
                with self.server.generations.lock:
                    self.server.generations.poisoned = True

    def _respond(self, job, stream):
        base = {
            "id": "chatcmpl-" + uuid.uuid4().hex,
            "object": "chat.completion.chunk" if stream else "chat.completion",
            "created": int(time.time()),
            "model": self.server.served_model,
        }
        text = []
        while True:
            self._check_connected()
            try:
                kind, value = job.output.get(timeout=0.1)
            except queue.Empty:
                if stream:
                    self.wfile.write(b": generation pending\n\n")
                    self.wfile.flush()
                continue
            if kind == "error":
                if not stream:
                    self._reply(500, {"error": "generation_failed"})
                return False
            if kind == "text":
                if stream:
                    self._event(
                        {
                            **base,
                            "choices": [
                                {"index": 0, "delta": {"content": value}, "finish_reason": None}
                            ],
                        }
                    )
                else:
                    text.append(value)
                continue
            reason, prompt_tokens, completion_tokens = value
            usage = {
                "prompt_tokens": prompt_tokens,
                "completion_tokens": completion_tokens,
                "total_tokens": prompt_tokens + completion_tokens,
            }
            self._check_connected()
            if stream:
                self._event(
                    {**base, "choices": [{"index": 0, "delta": {}, "finish_reason": reason}]}
                )
                self._event({**base, "choices": [], "usage": usage})
                self.wfile.write(b"data: [DONE]\n\n")
                self.wfile.flush()
            else:
                self._reply(
                    200,
                    {
                        **base,
                        "choices": [
                            {
                                "index": 0,
                                "message": {"role": "assistant", "content": "".join(text)},
                                "finish_reason": reason,
                            }
                        ],
                        "usage": usage,
                    },
                )
            return True


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--model", required=True, type=Path)
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", required=True, type=int)
    parser.add_argument("--served-model-name", required=True)
    parser.add_argument("--api-key-env")
    parser.add_argument("--max-model-len", required=True, type=int)
    args = parser.parse_args()
    try:
        validate_context(args.max_model_len)
        if sys.platform != "darwin" or not 1 <= args.port <= 65535:
            raise ValueError("native_macos_and_valid_port_required")
        if not ipaddress.ip_address(args.host).is_loopback:
            raise ValueError("loopback_required")
        if not args.served_model_name.strip() or len(args.served_model_name.encode()) > 4096:
            raise ValueError("invalid_served_model")
        key = None
        if args.api_key_env:
            if not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", args.api_key_env):
                raise ValueError("invalid_api_key_environment")
            key = os.environ.get(args.api_key_env)
            if not key or len(key) > 512 or any(not 33 <= ord(c) <= 126 for c in key):
                raise ValueError("invalid_api_key_environment")
        backend = MLXBackend(args.model, max_model_len=args.max_model_len)
        with FormalServer((args.host, args.port), backend, args.served_model_name, key) as server:
            server.serve_forever()
    except KeyboardInterrupt:
        return 130
    except Exception:
        print("mlx_formal_server_startup_or_runtime_failed", file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
