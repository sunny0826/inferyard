# 源码获取宿主公共接口 v1

范围是独立操作者库，设计理由见 [ADR 031](../decisions/031-source-acquisition-process-owner.md)。
真实获取 CLI 尚未接入，库接口不改变产品 Schema 或模型服务生命周期。

## 职责和数据权威

```mermaid
flowchart LR
    H[同一个宿主实例] --> C[进程保管器]
    C --> W[直接子进程]
    W --> F[读文件 写文件 计算哈希]
    H --> R[再次调用 继续检查或停止]
```

宿主运行期间不得 open/read/write/flush/fsync/close 文件、访问网络或等待阻塞管道。
所有原 Popen 或原生进程对象及句柄引用只存于一个 Custodian 实例；它不写盘，返回值由调用方决定如何保存。
原对象不能序列化迁移或由 PID 重建。快照只记录状态，不转移控制权。
进程启动与原生句柄操作属于 Backend；文件 I/O 仅属于独立 worker。
实际获取入口保持禁用。

## 身份、期限与资源

协议字符串为 `ninfer-source-host/1`，真实候选阶段固定为整数 `45`；拒绝 bool。
UUID 使用小写标准带横线格式，SHA256 使用 64 位小写十六进制。以下字典精确字段集合，
输入不得被校验器修改。未知键、重复 JSON 键、非有限数、类型冒充均拒绝。
源码映射摘要是 `scripts_sha256`，与 `execution_sha256` 分开绑定；集成层分别计算二者。

`RunIdentity` 字典字段：`protocol, host_id, execution_sha256, scripts_sha256, stage,
work_end_ticks, total_end_ticks`。ticks 是严格非负整数，total > work。
`Job` 增加精确字段 `job_id, kind, task_end_ticks`；kind 为 `file_io` 或 `asset`，
task <= work、task > 当前 ticks。Job 的 RunIdentity 字段应与实例完全一致。
单实例 job_id 不重复使用，即使已关闭也不能重试同一 job。

原共同期限不重置：未来实际总 1800/工作 1740/失败收尾 60 秒，每资产 420/下载 180 秒；
原共享账本 64 MiB 下载、256 MiB 展开、6000 文件、600 MiB 哈希不变。
本库不替代资产账本或资源采样；任何集成应沿用这些限制。
时钟注入 `now_ticks: Callable[[], int]`，frequency 是严格正整数；倒退拒绝。
停止、输出超限、主错或收尾错一旦发生，后续完成不能改成成功。
每 job stdout/stderr 分别最多保留 32768 bytes，超过即停止该 job，保留前缀与超限原因。

## Backend 的唯一冻结接口

Python 结构化 Protocol；测试可注入 fake 后端。
opaque ref 应持有 start 创建的原 Popen 或原生进程对象与原句柄；方法不接收数字 PID。

```python
start(spec: dict) -> object
pump(ref: object, limit: int) -> dict  # stdout: bytes, stderr: bytes, eof: bool
exit_code(ref: object) -> int | None  # 非阻塞；None 仅表示仍运行
terminate(ref: object) -> None
kill(ref: object) -> None
close_pipes(ref: object) -> None
close_handle(ref: object) -> None
```

Spec 精确字段为 `argv, cwd, executable_sha256, script_sha256, no_descendants`；argv 为非空
tuple[str, ...]，每项无 NUL，总 UTF-16 字符不超过 12000；cwd 非空绝对路径。
两项 SHA 严格校验，no_descendants 应为 True。实际文件哈希预检属于未来集成的 I/O job；
Backend 不在控制路径读取 EXE/脚本。start 失败也不得泄漏已创建的原对象：若已创建对象，
抛 `BackendStartError(ref, cause)`，Custodian 应接收并保留该 ref；尚未创建则 ref=None。
`HostError(code)`、`BackendError`（HostError 子类，code=host_backend）及 BackendStartError
（BackendError 子类，另有 ref/cause）在 custody.py 定义，后端只在错误路径延迟导入。

pump 每次最多读取 limit=4096 bytes/stream，Peek 后才读取，不能读超过已可用字节。
eof 只有两个流均 EOF 才为 True。退出后仍排空管道；无 EOF 不关闭正在使用的读取对象。
exit_code 应用原句柄确认，无法判断应抛 BackendError，不以 None 或“已退出”掩盖 API 错误。
close_handle 最多尝试一次；失败结果未知，不重试、不释放该引用、不宣称成功。
原生实现仅 Windows 可构造，其他平台在加载 WinDLL 前拒绝；mock 不调用真实 API。
直接子进程限制由 Windows Job Object（禁止 breakaway、活跃进程上限 1）控制；
挂入 Job 应在主线程恢复前完成。若平台不能提供该原子启动保证，start 拒绝并保管已创建对象。
不能用事后枚举子进程或 worker 自觉不 spawn 代替该保证。

## Custodian 的唯一冻结接口

构造和所有方法仅内存与 Backend 调用，无文件 I/O、sleep 或隐藏轮询循环。
每次 step 有界调用每个 job 一次；同时最多一个未关闭 job，文件 I/O 和资产任务都串行。
terminate/kill 各至多调用一次；terminate 后 grace_ticks 过去才 kill，grace 不延长 total。

```python
Custodian(identity: dict, backend: object, now_ticks: callable, frequency: int,
          grace_ticks: int)
start(job: dict, spec: dict) -> str  # job_id
step() -> dict                     # 一个有界轮次，返回 snapshot
cancel(job_id: str, reason: str) -> dict
resume(host_id: str, execution_sha256: str) -> dict  # 同实例继续 step；先核对身份
snapshot() -> dict                 # 只读派生状态
owned_objects() -> tuple[object, ...]  # 同进程审查/接手；必须是同一原对象
decision() -> dict                 # 复查当前工作期限；不写成功报告
```

cancel reason 只接受 `operator_cancel`、`deadline`、`output_limit`、`io_error`。
step 先检查时钟和截止时间；到期拒绝新工作并转停止，退出确认/收尾使用原 total 期限。
到 total 仍无法退出时保留对象，返回 custody_required；不得自行退出宿主。
confirmed exit 后才关闭管道和原句柄，各最多尝试一次，任一异常保留原因和未知对象。
start、step、cancel、resume 的 Backend 错误不丢引用；用户输入错误在 start 前拒绝零调用。

snapshot 精确顶层字段为 `protocol, host_id, execution_sha256, scripts_sha256, stage,
jobs, custody_required`。jobs 按启动顺序，字段 `job_id, kind, state, returncode,
original_exited, close_attempts, close_successes, stdout, stderr, primary_error, cleanup_errors`。
stdout/stderr 是已保存前缀 bytes；快照为独立副本。state 为
`running/stopping/exited/closed/unknown`；returncode 严格 int 或 None，退出确认应为 bool。
unknown 表示退出/管道/句柄结果不确定，custody_required=True；closed 的原引用已释放。
primary_error 是首个稳定原因码或 None，cleanup_errors 为原因码列表；不可用错误覆盖主错。

decision 在 snapshot 基础上增加 `can_exit, success, reason`：can_exit 仅在原对象全部
确认退出并完成关闭时 True；success 还要求至少一个 job、所有 returncode=0、无任何错误、
两个流已 EOF、原工作期限未到。所有未知结果否决成功；失败但已清理可以 can_exit=True。
job 输出业务校验和最终 I/O 完成由集成层另做；本 decision 不能单独授予源码通过。
稳定原因码：`host_identity`、`host_job`、`host_clock`、`host_deadline`、
`host_output_limit`、`host_backend`、`host_cleanup`、`host_cancelled`、`host_busy`。
输入拒绝用 `HostError(code)`；运行/收尾错误记入快照。原异常文本只在独立脱敏诊断中使用。

## 文件 worker 的接口

接口为：`validate_io_request(value: dict) -> dict`、
`execute_io(value: dict, *, io_api: object, now_ticks: callable) -> dict`。
前者不 I/O；后者只用于子进程或注入模拟，不得在宿主直接调用。
worker CLI `main(argv: list[str] | None = None) -> int` 接收一个 Base64 JSON 参数，
严格 UTF-8、无重复键，解码不超过 12288 bytes；当前 CLI 始终拒绝真实 I/O。
集成开放 CLI 须另行审查，不以本库单测通过替代。

请求精确字段为 `protocol, host_id, execution_sha256, scripts_sha256, stage, job_id,
task_end_ticks, total_end_ticks, frequency, operation, path, expected_bytes, expected_sha256, data_b64`。
与 Job 同名字段同义，total > task，调用检查原 task 期限。operation 为 `read/hash/write_new`。
path 为绝对普通文件路径，拒绝 device 前缀、相对路径和 reparse 祖先。
expected_bytes 严格非负整数，上限 40 MiB；expected_sha256 为小写 SHA256。
read 文件上限 8192 bytes；hash 不返回正文；二者 data_b64=None。
write_new 内容不超过 8192 bytes，data_b64 严格 Base64；内容长度/SHA 先核验，再 CreateNew 写入，
不覆盖旧文件。禁止 append、替换、删除或修改已有文件；更大封存产物留给集成的独立任务。

结果精确字段为 `protocol, host_id, execution_sha256, scripts_sha256, stage, job_id,
operation, bytes, sha256, data_b64, primary_error, cleanup_errors`；read 成功带正文，其他不带。
错误也保留已知计数，不把未知字节填 0；bytes/sha256 不可确认时为 None。
成功要原 task deadline 内完成 read/write/flush/fsync/close 和最终检查。
主错与 close 错分别保留；结果 JSON 单帧上限 24576 bytes，stdout 整体 32768 bytes。
IO 原因码：`io_request`、`io_deadline`、`io_identity`、`io_path`、`io_failure`、`io_cleanup`。
真实获取、下载或构建入口需单独集成，不能由库接口推定可执行。

## 验收边界

模拟门分别覆盖保管、再次调用、期限、错误、关闭计数，以及文件身份和 I/O 收尾。
Windows 门应现场制造阻塞 I/O，证明同宿主仍响应、停止原对象、再次接手原引用并封存真实结果。
如果原生 Job/管道/进程状态未知，保留限制，不把库开发完成写成实际获取通过。
