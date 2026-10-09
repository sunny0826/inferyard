# Windows lab 观测与身份接口

本文规定已实现的客户端解析与原生身份接口；默认 auto 与显式 lab_required 的准入规则见[适配契约](windows-ninfer-adaptation-contract.md)。
引擎发布包是否提供 lab 接口须实际探测，不能由客户端实现推定。

## 公共值与错误

- JSON 原字节严格 UTF-8，拒绝重复键、NaN/Infinity、布尔计数；不修改输入对象。
- SHA256 为 64 个小写十六进制字符；实例及请求 ID 为 32 个小写十六进制字符。
- 计数、大小、序号上限为 `2**63-1`；布尔只接受 `type(x) is bool`。
- 观测时间 `observed_ns` 是客户端单调时钟整数，不是引擎墙钟；deadline 为调用者
  的绝对 `time.monotonic()` 秒数。函数不能延长 deadline 或自动重试。
- `LabProtocolError` 继承 `RuntimeError`，只含稳定原因码，不回显响应、路径、凭据。
  Windows 模块使用现有 `PreflightError`，原因前缀 `lab_windows_`。
- 传输响应累计上限 2 MiB；单 SSE 帧上限 256 KiB。超过即拒绝，不能截断后报告成功。
- 模块接口接收普通 dict/bytes，公共应用请求/结果类型仅在 `application/types.py` 定义。

## 观测协议 lab_observation.v1

所有路由要求 loopback 与服务已有认证；观察不进入生成账本。identity 以外的请求
带 `X-Lab-Instance-ID`，生成另带 `X-Lab-Request-ID`。服务拒绝旧实例、缺 ID、重复 ID，
不得默默生成替代 ID。HTTP、SSE 和引擎 JSONL 应关联同一对 ID。
认证信息不能被上述 ID 替代，不允许重定向到其他 origin。

| 方法与路径                                  | 必需响应字段与含义                   |
| ------------------------------------------- | ------------------------------------ |
| GET `/lab/v1/identity`                      | 下述 identity 对象；不接受未知协议   |
| GET `/lab/v1/lifecycle`                     | 同步快照，含全部活动请求             |
| GET `/lab/v1/requests/{request_id}`         | 该请求状态及取消标记                 |
| POST `/lab/v1/requests/{request_id}/cancel` | 空 JSON 对象；返回取消已接受或已释放 |

各观测对象根字段 **恰为** 本文列出的集合。嵌套字段也按集合校验。

### identity

```json
{
  "protocol": "lab_observation.v1",
  "server_instance_id": "11111111111111111111111111111111",
  "engine": "ninfer",
  "build_id": "lab-patched-build",
  "model": {
    "kind": "ninfer",
    "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "bytes": 100
  },
  "template_sha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
  "capabilities": {
    "lifecycle": true,
    "request_cancel": true,
    "request_id": true,
    "token_budget": false,
    "effective_parameters": false
  }
}
```

engine 仅 `ninfer|kvmem`，对应 model.kind 仅 `ninfer|gguf`。build_id 是非空可打印
ASCII，最多 128 字节。bytes 正整数。template_sha256 可 null，不能因此获得模板预算资格。
capabilities 五个键均是布尔；解析允许 false，显式 lab_required 由集成层拒绝缺少的能力；auto 按适配契约披露缺测。
服务声明的文件 SHA 不能替代本机哈希。不能从 capability=true 推断真实实现通过。

### 生命周期和请求

活动阶段固定为 `accepted_waiting|preparing|prefilling|decoding|output_draining|releasing`。
状态只允许向前推进；允许跳过未用阶段，释放前不得移出活动列表。
取消标记只能 false→true，不等于立刻释放。`releasing` 仍计入活动。

lifecycle 根字段：`protocol, server_instance_id, snapshot_seq, poisoned, active_total,
stages, requests`。stages 恰含六阶段的非负计数。requests 每项恰含
`request_id, phase, cancel_requested`，最多 16 项，ID 唯一。
active_total 应等于 stages 求和、requests 长度及逐阶段数量。snapshot_seq 为正整数。
引擎在同一锁内读取以上字段；客户端结构校验不能证明引擎真的用了同一锁。
poisoned=true 可以解析并保留，但 **绝不** 视为 idle。

request 根字段：`protocol, server_instance_id, snapshot_seq, request_id, phase,
cancel_requested, outcome`。phase 为六阶段之一或 `released`；活动时 outcome=null，
released 时 outcome 为 `completed|failed|cancelled`。只有资源和响应消费者已释放才
发布 released；完成 SSE 不自动代表 released。

cancel 根字段：`protocol, server_instance_id, request_id, disposition`。
disposition 仅 `accepted|already_released`。同一个请求重复取消幂等；accepted 后仍须查询。
错误固定为 HTTP409 `instance_mismatch|duplicate_request_id`、404 `request_not_found`、
410 `request_expired`、429 `request_registry_full`。错误 JSON 恰含 `protocol, error`。
释放历史可以有界保留，但本实例曾用 ID 不得重新接受；到容量上限拒绝新请求。
取消不依赖有效的 Responses cancel 路由，也不终止整个服务。

## 观测与生成解析接口

`adapters/lab_observation.py`：

```python
parse_identity(raw: bytes, *, expected_engine: str) -> dict
parse_lifecycle(raw: bytes, *, expected_instance_id: str) -> dict
parse_request(raw: bytes, *, expected_instance_id: str, expected_request_id: str) -> dict
parse_cancel(raw: bytes, *, expected_instance_id: str, expected_request_id: str) -> dict
is_idle(snapshot: dict) -> bool
```

parse 返回完整、独立的已校验对象，不补造缺字段。is_idle 应重新验证输入，
仅在 active_total=0、poisoned=false 返回 true。实例稳定与前后样本顺序由下面的 tracker 核验。
`ObservationTracker(instance_id)` 提供 `accept_lifecycle(snapshot)` 和
`accept_request(snapshot)`；拒绝实例变化、seq 回退、相同 seq 内容冲突、已释放请求复活、
阶段倒退及取消标记回退。相同 seq 相同内容允许重复观察；不同端点同 seq 不混作同一对象。
所有结构错误原因码 `lab_invalid_<identity|lifecycle|request|cancel>`；关联错误
`lab_instance_mismatch|lab_request_mismatch|lab_observation_regressed`。

`adapters/lab_generation.py`：

```python
GenerationDecoder(request_id, instance_id, *, streaming: bool, t_send_ns: int)
decoder.feed(raw: bytes, *, observed_ns: int) -> None
decoder.finish(*, observed_ns: int) -> dict
```

只负责解析，不做 HTTP、启动进程、写文件或清理 dirty。流式按 data 帧严格 UTF-8 跨块
解析，支持 CRLF/LF；普通 JSON 在 finish 解析累计字节。生成根对象应含
`lab_request_id, lab_server_instance_id` 且匹配；choices 恰一项 index=0，usage-only SSE
允许 choices=[]。流式应有合法 finish_reason 和 `[DONE]`；普通 JSON 应合法完整终态。
终态仅 `stop|length`；[DONE] 后 data、重复 finish、finish 后非空文本、错误帧、截断、
时间倒退均拒绝。注释心跳不算生成内容。content/reasoning 分开，禁止非字符串。
finish 返回字段恰为 `request_id, server_instance_id, content, reasoning, finish_reason,
protocol_complete, prompt_tokens, completion_tokens, cached_tokens, reasoning_tokens,
usage_missing_reasons, t_send_ns, t_first_content_ns, t_first_answer_ns, t_terminal_ns`。
protocol_complete=true 只代表完整响应，不代表释放。

usage 允许合法嵌套 details 和未知扩展字段；四个已知计数分别取 prompt_tokens、
completion_tokens、prompt_tokens_details.cached_tokens、completion_tokens_details.reasoning_tokens。
缺测 null 并在 usage_missing_reasons 对应键记 `not_reported`；已测对应原因 null。
SSE 的 `usage:null` 为未报告用量的占位，接受并跳过；后续 usage 对象按上述规则合并。
全流未收到 usage 对象仍可完成请求，四项用量保留 null + `not_reported`，不补零。
usage 若为非 null 的非对象值仍拒绝；普通非流式响应的对象校验保持原规则。
details 如出现须对象，计数如出现须非负整数；cached≤prompt、reasoning≤completion
在二者均可读时核验。已报告计数不能被后续不同值覆盖。total_tokens 如出现须整数，
prompt/completion 同时存在时应等于其和。无 reasoning delta 不能补 reasoning_tokens=0。
首内容为首个含非空白 content 或 reasoning 的块；首答案只取非空白 content。
保留原文本空白，不在结果里裁剪。错误码 `lab_generation_<原因>`，测试固定具体值。

## Windows 进程与文件接口

`platforms/windows_lab_process.py`：

```python
ProcessHandle(pid: int)  # context manager，延迟加载 WinAPI
handle.creation_filetime() -> int
handle.is_exited() -> bool
verify_process_binding(expected: dict, *, deadline: float) -> dict
```

OpenProcess 使用 query limited information + synchronize，无终止/写权限；GetProcessTimes
和零等待 WaitForSingleObject 读取 **同一句柄**。每条成功、异常路径恰关闭一次句柄。
ERROR_INVALID_PARAMETER(87) 是对象不存在；access denied、WAIT_FAILED、API 失败是 unknown，
不能返回“已退出”。禁止依赖 psutil.pid_exists 或浮点 create_time。Mac 导入不能加载
winreg/WinDLL。
verify 返回恰 expected 的绑定字段与 `listener_identity`、`listener_source`、
`process_start_source`（来源分别为 GetExtendedTcpTable:owner_pid 与
GetProcessTimes:creation_FILETIME_100ns_since_1601，可选 `details`）；FILETIME
不匹配时抛 `lab_windows_filetime_mismatch`，不能以绑定旧对象已不存在的结论
替代当前进程详情。

expected 恰含 `pid, creation_filetime, origin, executable_path, executable_sha256,
argv_sha256, cwd_sha256`。PID 正整数，FILETIME 正整数；origin 只允许显式 IPv4/IPv6
loopback 与端口，无凭据/query/fragment。读取 exe/cwd/argv、账户和唯一监听，
按同一句柄前后核验未退出。若 PID 中途复用，另开校验句柄核对 creation 与原句柄，不能
把 psutil 读到的新对象详情接到旧身份。wildcard 或双栈歧义监听拒绝；账户大小写无关相同。
exe SHA 复用现有 Windows 安全文件哈希；argv SHA 为 UTF-8 严格编码参数，用 NUL 连接并加末尾 NUL，
cwd SHA 为严格 UTF-8 的解析绝对路径字符串。均不返回原 argv/账户/环境。
返回恰为 expected 七字段加 `listener_identity, listener_source, process_start_source`；
listener_identity=`windows:tcp:{address}:{port}:pid:{pid}`；source 沿用原 Windows 来源常量。
路径可以被调用者单独脱敏；本函数异常不回显路径。新增函数不改变旧绑定算法及旧证据。

`platforms/windows_lab_files.py`：

```python
verify_file_manifest(entries: list[dict], *, deadline: float) -> list[dict]
```

每项恰 `path, role, bytes, sha256`；role=`model|engine|library|component_ledger|template`。
要求绝对路径、正大小、有效 SHA；1–256 项、大小合计 ≤64 GiB，拒绝 Windows 大小写重名。
复用 native descriptor/path stamp 与安全 open，不混 stat/fstat；拒绝所有路径分量的 reparse。
计算实际完整 SHA，并前后 native stamp 核验；逐读取块检查 deadline，不能复用无 deadline 的大文件哈希。
返回原四字段加 `native_stamp`（七项 JSON 数组，128-bit file ID 用 32 字符小写 hex）。
该接口只核验清单文件身份，不宣称原生容器目录、组件关系或 GPU 驻留通过。
模型/EXE/DLL/模板更换、同大小同 mtime 替换、读取中变更均拒绝。
