"""Reproduce MLX stream thread affinity without loading MLX or a model."""

import sys
import threading
from contextlib import nullcontext
from types import SimpleNamespace

from tests.unit.test_engine_fit_mlx_server import drain, generations, mlx_server, response


class ThreadBoundStream:
    def __init__(self, device):
        self.device = device
        self.owner = threading.get_ident()


class ThreadLocalStream:
    def __init__(self, device):
        self.device = device
        self.local = threading.local()

    def current(self):
        if not hasattr(self.local, "stream"):
            self.local.stream = ThreadBoundStream(self.device)
        return self.local.stream


def test_main_thread_load_and_worker_generation_synchronize_their_own_streams(
    tmp_path, monkeypatch
):
    syncs, supplied_streams = [], []
    main_thread = threading.get_ident()
    tokenizer = SimpleNamespace(apply_chat_template=lambda *_args, **_kwargs: [1, 2, 3])

    def synchronize(stream):
        resolved = stream.current() if isinstance(stream, ThreadLocalStream) else stream
        if resolved.owner != threading.get_ident():
            raise RuntimeError("There is no Stream(gpu,0) in current thread")
        syncs.append((threading.get_ident(), resolved))

    def generate(_model, _tokenizer, **options):
        supplied_streams.append(options["stream"])
        try:
            yield response()
        finally:
            # mlx_lm.stream_generate synchronizes its supplied stream during cleanup.
            synchronize(options["stream"])

    mx = SimpleNamespace(
        gpu="synthetic-gpu",
        new_stream=ThreadBoundStream,
        new_thread_local_stream=ThreadLocalStream,
        stream=lambda active: nullcontext(active),
        synchronize=synchronize,
    )
    monkeypatch.setitem(sys.modules, "mlx", SimpleNamespace(core=mx))
    monkeypatch.setitem(sys.modules, "mlx.core", mx)
    monkeypatch.setitem(
        sys.modules,
        "mlx_lm",
        SimpleNamespace(
            load=lambda *_args, **_kwargs: ("synthetic-model", tokenizer),
            stream_generate=generate,
        ),
    )
    monkeypatch.setitem(
        sys.modules, "mlx_lm.sample_utils", SimpleNamespace(make_sampler=lambda **kwargs: kwargs)
    )
    monkeypatch.setattr(mlx_server, "version", lambda _package: "0.0.0+synthetic")
    monkeypatch.setenv("HF_HUB_OFFLINE", "1")
    monkeypatch.setenv("TRANSFORMERS_OFFLINE", "1")

    backend = mlx_server.MLXBackend(tmp_path)
    assert syncs[0][0] == main_thread
    with generations(backend) as state:
        worker_thread = state.thread.ident
        assert worker_thread != main_thread
        for prompt in ("first", "second"):
            assert drain(state.submit(prompt, 8)) == [
                ("text", "42"),
                ("done", ("stop", 7, 1)),
            ]
            assert state.metrics() == (0, 0, 0)

    assert supplied_streams == [backend.stream, backend.stream]
    assert [thread for thread, _stream in syncs] == [main_thread] + [worker_thread] * 4
    assert all(stream is syncs[1][1] for _thread, stream in syncs[1:])
    assert syncs[0][1] is not syncs[1][1]
