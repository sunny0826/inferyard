"""Operator-started, local-only MLX-LM diagnostic server (standalone Python 3.12+).

Run this file in an independently prepared MLX-LM environment. It never downloads
models or imports inferyard. This is not the upstream mlx_lm.server frontend.
"""

import argparse
import hashlib
import hmac
import ipaddress
import json
import os
import queue
import re
import socket
import sys
import threading
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from importlib.metadata import version
from pathlib import Path

PROTOCOL = "mlx-lm.v1"
MAX_TOKENS = 8192
MAX_BODY_BYTES = 131072
MAX_OUTPUT_BYTES = 4 * 1024**2
QUEUE_CAPACITY = 4
SOCKET_TIMEOUT = 10


def _json(value):
    return json.dumps(value, ensure_ascii=False, allow_nan=False, separators=(",", ":")).encode()


def _object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate_json_key")
        result[key] = value
    return result


def _constant(_value):
    raise ValueError("invalid_json_number")


def validate_request(body, model):
    expected = {"model", "messages", "max_tokens", "temperature", "stream", "stream_options"}
    if type(body) is not dict or set(body) != expected or body["model"] != model:
        raise ValueError("invalid_request")
    messages = body["messages"]
    if (
        type(messages) is not list
        or len(messages) != 1
        or type(messages[0]) is not dict
        or set(messages[0]) != {"role", "content"}
        or messages[0]["role"] != "user"
        or type(messages[0]["content"]) is not str
        or not messages[0]["content"].strip()
        or len(messages[0]["content"].encode()) > 65536
        or type(body["max_tokens"]) is not int
        or not 1 <= body["max_tokens"] <= MAX_TOKENS
        or type(body["temperature"]) not in (int, float)
        or body["temperature"] != 0
        or body["stream"] is not True
        or type(body["stream_options"]) is not dict
        or set(body["stream_options"]) != {"include_usage"}
        or body["stream_options"]["include_usage"] is not True
    ):
        raise ValueError("invalid_request")
    return messages[0]["content"], body["max_tokens"]


class MLXBackend:
    """All model operations use public MLX APIs and one explicitly tracked stream."""

    synthetic = False

    def __init__(self, model_path, max_model_len=None):
        if max_model_len is not None and (
            type(max_model_len) is not int or not 1 <= max_model_len <= 32768
        ):
            raise ValueError("invalid_max_model_len")
        self.max_model_len = max_model_len
        root = Path(model_path)
        if root.is_symlink() or not root.is_dir():
            raise ValueError("local_model_directory_required")
        root = root.resolve(strict=True)
        os.environ["HF_HUB_OFFLINE"] = "1"
        os.environ["TRANSFORMERS_OFFLINE"] = "1"
        import mlx.core as mx
        from mlx_lm import load, stream_generate
        from mlx_lm.sample_utils import make_sampler

        self.mx, self.generate = mx, stream_generate
        self.version = version("mlx-lm")
        self.stream = mx.new_thread_local_stream(mx.gpu)
        with mx.stream(self.stream):
            self.model, self.tokenizer = load(
                str(root),
                tokenizer_config={"trust_remote_code": False, "local_files_only": True},
                trust_remote_code=False,
                lazy=False,
            )
        self.sampler = make_sampler(temp=0)
        self.synchronize()

    def prepare_prompt(self, prompt, max_tokens):
        tokens = (
            prompt
            if type(prompt) is list
            else self.tokenizer.apply_chat_template(
                [{"role": "user", "content": prompt}],
                tokenize=True,
                add_generation_prompt=True,
            )
        )
        if type(tokens) is not list or not 1 <= len(tokens) <= 32768:
            raise ValueError("prompt_tokens_out_of_bounds")
        if self.max_model_len is not None and len(tokens) + max_tokens > self.max_model_len:
            raise ValueError("context_length_exceeded")
        return tokens

    def responses(self, prompt, max_tokens):
        return self.generate(
            self.model,
            self.tokenizer,
            prompt=self.prepare_prompt(prompt, max_tokens),
            max_tokens=max_tokens,
            sampler=self.sampler,
            stream=self.stream,
        )

    def synchronize(self):
        self.mx.synchronize(self.stream)


class Job:
    def __init__(self, prompt, max_tokens):
        self.prompt, self.max_tokens = prompt, max_tokens
        self.output = queue.Queue(maxsize=16)
        self.cancelled = threading.Event()

    def publish(self, value):
        while not self.cancelled.is_set():
            try:
                self.output.put(value, timeout=0.1)
                return
            except queue.Full:
                continue
        raise RuntimeError("client_disconnected")


class GenerationQueue:
    """Counters include queued work, iterator cleanup and GPU synchronization."""

    def __init__(self, backend):
        if not re.fullmatch(r"[0-9][A-Za-z0-9.+_-]{0,127}", backend.version):
            raise ValueError("invalid_backend_version")
        self.backend = backend
        self.lock = threading.Lock()
        self.preparation_lock = threading.Lock()
        self.jobs = queue.Queue(maxsize=QUEUE_CAPACITY)
        self.running = self.waiting = 0
        self.poisoned = self.closing = False
        self.active = None
        self.thread = threading.Thread(target=self._work, daemon=True)
        self.thread.start()

    def metrics(self):
        with self.lock:
            return self.running, self.waiting, int(self.poisoned or self.closing)

    def submit(self, prompt, max_tokens):
        with self.lock:
            if self.poisoned or self.closing:
                raise RuntimeError("service_poisoned")
            if self.waiting >= QUEUE_CAPACITY:
                raise queue.Full
            self.waiting += 1
        admitted = False
        try:
            if getattr(self.backend, "max_model_len", None) is not None:
                with self.preparation_lock:
                    with self.lock:
                        if self.poisoned or self.closing:
                            raise RuntimeError("service_poisoned")
                    prompt = self.backend.prepare_prompt(prompt, max_tokens)
            job = Job(prompt, max_tokens)
            with self.lock:
                if self.poisoned or self.closing:
                    raise RuntimeError("service_poisoned")
                self.jobs.put_nowait(job)
                admitted = True
            return job
        finally:
            if not admitted:
                with self.lock:
                    self.waiting -= 1

    def close(self):
        with self.lock:
            self.closing = True
            if self.active:
                self.active.cancelled.set()
        self.thread.join(timeout=2)

    def _work(self):
        while True:
            try:
                job = self.jobs.get(timeout=0.1)
            except queue.Empty:
                if self.closing:
                    return
                continue
            with self.lock:
                self.waiting -= 1
                rejected = self.poisoned or self.closing or job.cancelled.is_set()
                if not rejected:
                    self.running, self.active = 1, job
            if rejected:
                if not job.cancelled.is_set():
                    job.output.put_nowait(("error", None))
                continue
            self._generate(job)

    def _generate(self, job):
        iterator, terminal, failed, synchronized, closed = None, None, False, False, False
        output_bytes, prior_tokens = 0, 0
        try:
            iterator = self.backend.responses(job.prompt, job.max_tokens)
            for response in iterator:
                if job.cancelled.is_set():
                    raise RuntimeError("client_disconnected")
                count, prompt = response.generation_tokens, response.prompt_tokens
                if (
                    terminal is not None
                    or type(response.text) is not str
                    or type(count) is not int
                    or not prior_tokens < count <= job.max_tokens
                    or type(prompt) is not int
                    or not 1 <= prompt <= 32768
                    or response.finish_reason not in (None, "stop", "length")
                ):
                    raise ValueError("invalid_backend_response")
                prior_tokens = count
                output_bytes += len(_json(response.text))
                if output_bytes > MAX_OUTPUT_BYTES:
                    raise ValueError("response_too_large")
                if response.text:
                    job.publish(("text", response.text))
                if response.finish_reason is not None:
                    terminal = (response.finish_reason, prompt, count)
            if terminal is None:
                raise ValueError("missing_backend_finish")
        except Exception:
            failed = True
        finally:
            try:
                if iterator is not None:
                    iterator.close()
                closed = True
            except Exception:
                failed = True
            try:
                self.backend.synchronize()
                synchronized = True
            except Exception:
                failed = True
            with self.lock:
                self.poisoned |= failed or job.cancelled.is_set()
                if synchronized and closed:
                    self.running, self.active = 0, None
        try:
            job.publish(("error", None) if failed else ("done", terminal))
        except RuntimeError:
            with self.lock:
                self.poisoned = True


class DiagnosticServer(ThreadingHTTPServer):
    daemon_threads = True
    block_on_close = False

    def __init__(self, address, backend, served_model, api_key=None):
        if not ipaddress.ip_address(address[0]).is_loopback:
            raise ValueError("loopback_required")
        if not served_model.strip() or len(served_model.encode()) > 4096:
            raise ValueError("invalid_served_model")
        self.address_family = socket.AF_INET6 if ":" in address[0] else socket.AF_INET
        self.served_model, self.api_key = served_model, api_key
        self.source_hash = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
        self.capacity = threading.BoundedSemaphore(32)
        self.generations = GenerationQueue(backend)
        try:
            super().__init__(address, Handler)
        except Exception:
            self.generations.close()
            raise

    def process_request(self, request, client_address):
        if not self.capacity.acquire(blocking=False):
            self.shutdown_request(request)
            return
        try:
            super().process_request(request, client_address)
        except Exception:
            self.capacity.release()
            raise

    def process_request_thread(self, request, client_address):
        try:
            super().process_request_thread(request, client_address)
        finally:
            self.capacity.release()

    def server_close(self):
        super().server_close()
        self.generations.close()


class Handler(BaseHTTPRequestHandler):
    def setup(self):
        self.request.settimeout(SOCKET_TIMEOUT)
        super().setup()

    def log_message(self, *_args):
        pass

    def _authorized(self):
        key = self.server.api_key
        headers = self.headers.get_all("Authorization", [])
        return key is None or (
            len(headers) == 1
            and hmac.compare_digest(headers[0].encode(), ("Bearer " + key).encode())
        )

    def _reply(self, status, body, content_type="application/json"):
        raw = _json(body) if content_type == "application/json" else body.encode()
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(raw)))
        self.send_header("Connection", "close")
        self.end_headers()
        self.wfile.write(raw)

    def do_GET(self):
        if not self._authorized():
            self._reply(401, {"error": "authentication_required"})
        elif self.path == "/v1/models":
            self._reply(200, {"object": "list", "data": [{"id": self.server.served_model}]})
        elif self.path == "/version":
            self._reply(
                200,
                {
                    "version": self.server.generations.backend.version,
                    "engine_fit_protocol": PROTOCOL,
                    "engine_fit_source_sha256": self.server.source_hash,
                    "synthetic": self.server.generations.backend.synthetic,
                    "max_model_len": getattr(
                        self.server.generations.backend, "max_model_len", None
                    ),
                },
            )
        elif self.path == "/metrics":
            values = self.server.generations.metrics()
            names = ("num_requests_running", "num_requests_waiting", "poisoned")
            self._reply(
                200,
                "".join(
                    f"engine_fit:{name} {value}\n"
                    for name, value in zip(names, values, strict=True)
                ),
                "text/plain; version=0.0.4",
            )
        else:
            self._reply(404, {"error": "unknown_endpoint"})

    def _body(self):
        lengths = self.headers.get_all("Content-Length", [])
        if (
            self.headers.get("Transfer-Encoding") is not None
            or len(lengths) != 1
            or not lengths[0].isdigit()
            or not 1 <= int(lengths[0]) <= MAX_BODY_BYTES
            or self.headers.get_content_type() != "application/json"
        ):
            raise ValueError("invalid_request_body")
        raw = self.rfile.read(int(lengths[0]))
        if len(raw) != int(lengths[0]):
            raise ValueError("incomplete_request_body")
        body = json.loads(raw.decode(), object_pairs_hook=_object, parse_constant=_constant)
        return validate_request(body, self.server.served_model)

    def do_POST(self):
        if not self._authorized():
            return self._reply(401, {"error": "authentication_required"})
        if self.path != "/v1/chat/completions":
            return self._reply(404, {"error": "unknown_endpoint"})
        try:
            prompt, tokens = self._body()
            job = self.server.generations.submit(prompt, tokens)
        except (ValueError, UnicodeError, TimeoutError):  # fmt: skip
            return self._reply(400, {"error": "invalid_request"})
        except queue.Full:
            return self._reply(429, {"error": "generation_queue_full"})
        except RuntimeError:
            return self._reply(503, {"error": "service_poisoned"})
        completed = False
        try:
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Cache-Control", "no-cache")
            self.send_header("Connection", "close")
            self.end_headers()
            request_id = "chatcmpl-" + uuid.uuid4().hex
            while True:
                try:
                    kind, value = job.output.get(timeout=0.5)
                except queue.Empty:
                    self.wfile.write(b": generation pending\n\n")
                    self.wfile.flush()
                    continue
                if kind == "error":
                    break
                chunk = {"id": request_id, "model": self.server.served_model, "choices": []}
                if kind == "text":
                    chunk["choices"] = [
                        {"index": 0, "delta": {"content": value}, "finish_reason": None}
                    ]
                    self._event(chunk)
                    continue
                reason, prompt_tokens, completion_tokens = value
                chunk["choices"] = [{"index": 0, "delta": {}, "finish_reason": reason}]
                self._event(chunk)
                chunk["choices"] = []
                chunk["usage"] = {
                    "prompt_tokens": prompt_tokens,
                    "completion_tokens": completion_tokens,
                    "total_tokens": prompt_tokens + completion_tokens,
                }
                self._event(chunk)
                self.wfile.write(b"data: [DONE]\n\n")
                self.wfile.flush()
                completed = True
                break
        except (OSError, TimeoutError):  # fmt: skip
            pass
        finally:
            if not completed:
                job.cancelled.set()

    def _event(self, value):
        self.wfile.write(b"data: " + _json(value) + b"\n\n")
        self.wfile.flush()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--model", required=True, type=Path)
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", required=True, type=int)
    parser.add_argument("--served-model-name", required=True)
    parser.add_argument("--api-key-env")
    parser.add_argument("--max-model-len", type=int)
    args = parser.parse_args()
    try:
        if sys.platform != "darwin" or not 1 <= args.port <= 65535:
            raise ValueError("native_macos_and_valid_port_required")
        if not ipaddress.ip_address(args.host).is_loopback:
            raise ValueError("loopback_required")
        key = None
        if args.api_key_env:
            if not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", args.api_key_env):
                raise ValueError("invalid_api_key_environment")
            key = os.environ.get(args.api_key_env)
            if not key or len(key) > 512 or any(not 33 <= ord(c) <= 126 for c in key):
                raise ValueError("invalid_api_key_environment")
        backend = MLXBackend(args.model, max_model_len=args.max_model_len)
        with DiagnosticServer(
            (args.host, args.port), backend, args.served_model_name, key
        ) as server:
            server.serve_forever()
    except KeyboardInterrupt:
        return 130
    except Exception:
        print("mlx_engine_fit_server_startup_or_runtime_failed", file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
