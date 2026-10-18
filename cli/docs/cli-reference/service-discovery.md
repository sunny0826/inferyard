# 本机服务发现契约

本页是[CLI 接口](../cli-surface.md) 的子文档，收录「本机服务发现契约」的正文；内容不变，入口保留原节标题与锚点。

`inferyard service discover` 不接受进程名过滤参数，枚举 TCP loopback 监听端口，
反查监听 PID、可执行文件基名及 `process_start_ticks`。Linux 读取 `/proc`，
macOS/Windows 使用已声明依赖 psutil 逐进程读取连接表；权限不足、进程消失或身份变化
记入 limitations，不把不完整扫描当作没有服务。身份时间读取复用 bind 的 identity 层。

探测范围是所有发现的 loopback 监听端点，**不限模型服务**。通配监听地址
`0.0.0.0`/`::` 分别通过 `127.0.0.1`/`::1` 探测；其他非 loopback 地址不探测。
每个不同 HTTP origin 依次发送 `GET /health` 与 `GET /v1/models`，HTTP 超时 2 秒，
不重试、不跟随重定向、不使用环境代理、不发送认证或 Cookie。
只输出状态码类别和模型 ID，不保存响应头、凭据、原始响应或进程 argv。
模型列表响应上限 1 MiB，重复 JSON 键、非有限数、非法 ID 列表均拒绝。
CLI 不启动、停止或绑定服务；`model_requests_sent` 恒为 0。

输出为 CommandResult，`command="service discover"`；`run_id`/`evidence_dir` 为 null。
`details={"services": [...], "model_requests_sent": 0}`，services 每项为：

| 字段 | 类型与含义 |
| --- | --- |
| `pid` | 正整数，监听进程 PID |
| `process_name` | 非空 string，可执行文件基名，仅作展示，不作过滤 |
| `process_start_ticks` | 正整数，与当前平台 bind identity 层同源的启动身份 |
| `endpoints` | 非空 array，每项含下表字段 |

| 端点字段 | 类型与含义 |
| --- | --- |
| `url` | 数字 loopback HTTP origin；IPv6 带方括号 |
| `health` | `/health` 的 `1xx`/`2xx`/`3xx`/`4xx`/`5xx`，传输失败为 null |
| `models` | 非空模型 ID string array（去重保序），失败或无模型为 null |
| `reason` | 下表固定值；两项探测均无缺测时为 null |
| `ambiguous_listener` | boolean；同端口观察到多个 PID 时为 true，跨 IPv4/IPv6 也不猜唯一归属 |

同一端口的所有可读监听进程全部列出，同 origin 的探测结果复用。
枚举与探测不是原子快照，不推断 IPv6 是否同时接受 IPv4；歧义标记 false 也不证明唯一性。
可将发现 PID 和 endpoint 显式传给 `config bind`，但 discover 不预判 bind 可行性；
bind 仍重新检查身份、资产和候选 argv，可能返回 `startup_arguments_mismatch` 等拒绝。

固定端点 reason（模型列表失败优先于 health 传输失败）：

| reason | 含义 |
| --- | --- |
| `models_timeout` | `/v1/models` 超时 |
| `models_unreachable` | `/v1/models` 连接、传输或 HTTP 协议失败 |
| `models_http_1xx` | `/v1/models` 返回 1xx |
| `models_http_3xx` | `/v1/models` 返回 3xx，不跟随重定向 |
| `models_http_4xx` | `/v1/models` 返回 4xx，包括需要认证 |
| `models_http_5xx` | `/v1/models` 返回 5xx |
| `models_invalid_response` | 非合法 JSON、缺少 data array 或有空白/非 string ID |
| `models_empty` | data 为空 array，没有可列出的模型 |
| `models_response_too_large` | 模型列表响应超过 1 MiB |
| `health_timeout` | 模型列表可用，但 `/health` 超时 |
| `health_unreachable` | 模型列表可用，但 `/health` 连接、传输或协议失败 |

枚举完整时退出 0、`status=discovered`、`completeness=complete`，包括 services 为空
或所有端点均无模型列表。部分枚举失败退出 3、`status=partial`、`completeness=incomplete`，
保留可用条目。工具错误退出 4、`status=error`、`completeness=incomplete`，保留已收集的
details。取消退出 130、`status=interrupted`、`completeness=incomplete`、details=null、
limitations=`cancelled`。

| limitations 固定值 | 含义 |
| --- | --- |
| `listener_table_incomplete` | 部分 Linux 连接表不可读或含坏行 |
| `listener_ownership_incomplete` | 权限、进程消失或未找到监听 socket 的可读属主 |
| `process_identity_unavailable` | 监听进程可执行名缺失或读取期间启动身份变化 |
| `listener_enumeration_failed` | 无法读取连接表或枚举进程（退出 4） |
| `service_discovery_unsupported_platform` | 平台不支持（退出 4） |
| `service_discovery_error` | 其他工具错误，不输出原始异常（退出 4） |
| `cancelled` | Ctrl-C 取消（退出 130） |

`inferyard --schema service_discovery` 导出此 CommandResult 结构；不改变核心证据契约版本。
回归使用模拟进程表和 HTTP 响应；Linux/macOS/Windows 真机权限、连接表覆盖及模型服务
发现效果须单独验证，模拟通过不代表原生平台验收。
