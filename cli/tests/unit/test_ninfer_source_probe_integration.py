"""Stage46 集成：真实诊断/宿主/后端代码，纯内存 API 和时钟。

不创建进程或 Windows 文件。Windows cwd 判断显式注入，不能作为原生证据。
"""

import io

from scripts.ninfer_source_host.probe_fixture import _run_fixture
from scripts.ninfer_source_host.probe_fixture_frames import loads_frame
from scripts.ninfer_source_host.probe_host import ProbeHost
from scripts.ninfer_source_host.probe_io import ProbeFileApi
from scripts.ninfer_source_host.probe_io_limits import FILE_ROOT, OUTPUT_PREFIX, STREAM_LIMIT
from scripts.ninfer_source_host.windows_backend import WindowsBackend
from tests.unit.test_ninfer_source_backend import FakeApi, spec
from tests.unit.test_ninfer_source_probe_host import EXEC_SHA, HOST_ID, JOB_ID, identity
from tests.unit.test_ninfer_source_probe_io import NOTE, Native, request


class Clock:
    frequency = 1

    def __call__(self):
        return 1


def fixture_bytes(mode):
    ident = identity(work_end_ticks=50, total_end_ticks=60)
    body = request("read", path=NOTE)
    for key in ("protocol", "host_id", "execution_sha256", "scripts_sha256", "stage"):
        body[key] = ident[key]
    body.update(job_id=JOB_ID, task_end_ticks=50, total_end_ticks=60, frequency=1)
    native = Native({NOTE: b"hi"})
    sink = io.BytesIO()
    code, _ = _run_fixture(
        mode,
        body,
        io_api=ProbeFileApi(FILE_ROOT, native) if mode == "file_io" else None,
        now_ticks=Clock(),
        stdout=sink,
    )
    assert code == 0
    if mode == "file_io":
        assert len(native.closed) == 1
    return ident, sink.getvalue()


def start_host(monkeypatch, ident, raw):
    # 仅模拟 Windows 路径判定，不修改冻结库或加载 WinDLL。
    monkeypatch.setattr("scripts.ninfer_source_host.custody.os.path.isabs", lambda _: True)
    api = FakeApi()
    api.stdout.data, api.stdout.eof = raw, True
    api.stderr.eof = True
    host = ProbeHost(ident, WindowsBackend(api), Clock(), 1, 1)
    job = {**ident, "job_id": JOB_ID, "kind": "file_io", "task_end_ticks": 50}
    host.start(job, spec())
    ref = host.owned_objects()[0]
    assert ref.process is api.process
    assert api.order == ["spawn", "job", "assign", "resume"]
    return host, api, ref


def test_file_result_crosses_backend_and_same_host_resume(monkeypatch):
    ident, raw = fixture_bytes("file_io")
    host, api, ref = start_host(monkeypatch, ident, raw)
    custodian = host._custodian
    first = host.resume(HOST_ID, EXEC_SHA)
    assert first["jobs"][0]["stdout"] == raw
    assert host.owned_objects() == (ref,)
    api.wait_result = 0
    host.resume(HOST_ID, EXEC_SHA)
    evidence = host.evidence()
    assert host._custodian is custodian
    assert evidence["experiment_stage"] == 46
    assert evidence["snapshot"]["stage"] == ident["stage"] == 45
    assert evidence["decision"]["success"] is True
    assert evidence["retained_count"] == 0
    frame = loads_frame(evidence["snapshot"]["jobs"][0]["stdout"])
    assert frame["job_id"] == JOB_ID and frame["host_id"] == HOST_ID
    assert frame["result"]["bytes"] == 2
    assert frame["result"]["data_b64"] == "aGk="
    assert api.closed == [api.stdout, api.stderr, api.process, api.thread, api.job]


def test_fixture_output_prefix_survives_real_backend_limit(monkeypatch):
    ident, raw = fixture_bytes("output")
    assert raw.startswith(OUTPUT_PREFIX) and len(raw) > STREAM_LIMIT
    host, api, ref = start_host(monkeypatch, ident, raw)
    for _ in range(12):
        row = host.step()["jobs"][0]
        if row["state"] == "stopping":
            break
    assert row["primary_error"] == "host_output_limit"
    assert row["stdout"] == raw[:STREAM_LIMIT]
    assert api.terminated == [api.process]
    assert host.owned_objects() == (ref,)
    api.wait_result = 0
    host.resume(HOST_ID, EXEC_SHA)
    decision = host.decision()
    assert decision["can_exit"] is True and decision["success"] is False
    assert decision["reason"] == "host_output_limit"
    assert decision["jobs"][0]["stdout"] == raw[:STREAM_LIMIT]
    assert api.closed == [api.stdout, api.stderr, api.process, api.thread, api.job]


def test_fixture_success_does_not_hide_backend_unknown_close(monkeypatch):
    ident, raw = fixture_bytes("file_io")
    host, api, ref = start_host(monkeypatch, ident, raw)
    api.wait_result, api.close_raises = 0, True
    host.step()
    before = list(api.closed)
    host.resume(HOST_ID, EXEC_SHA)
    evidence = host.evidence()
    assert evidence["decision"]["success"] is False
    assert evidence["decision"]["can_exit"] is False
    assert evidence["snapshot"]["custody_required"] is True
    assert evidence["retained_count"] == 1
    assert host.owned_objects() == (ref,)
    assert evidence["snapshot"]["jobs"][0]["stdout"] == raw
    assert api.closed == before
    assert api.closed == [api.stdout, api.process]
