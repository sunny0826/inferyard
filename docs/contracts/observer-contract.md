# 独立监测器接口契约

本页统一定义独立监测器、离线摘要与测评来源关联。observer 0.2.0 输出 `lab_observer.v2`；旧 v1 的读取差异见[版本规则](#输入与版本)。

依据 [ADR 009](../decisions/009-native-observer.md)，界面为 CLI + JSON。独立 `inferyard-observer` 不改变现有 `inferyard` CLI、v3 样本结构、测量源码或严格资格；也不自动启动 / 停止模型。

## CLI 与生命周期

| 命令                                                        | 行为                                                                              |
| ----------------------------------------------------------- | --------------------------------------------------------------------------------- |
| `inferyard-observer snapshot`                                     | 一次系统配置与候选服务进程；stdout 单 JSON                                        |
| `inferyard-observer watch --pid PID --interval 1s --duration 30s` | 固定 PID / 启动身份，stdout JSONL；不扫描所有进程                                 |
| `inferyard-observer watch --bench-run RUN_DIR --duration 30s`     | 读取既有 v3 `run.json` / `config.frozen.json`，固定 PID / 启动身份并记录来源 hash |
| `--out NEW.jsonl`                                           | 独占创建新文件，拒绝覆盖；全部结束后同步，不另建配置 / 运行依赖                   |
| `--endpoint http://127.0.0.1:PORT`                          | 显式可选；固定 GET `/health`、250ms 截止、至多每 5s；不推理、不代理、不重定向     |
| `--version` / `--help`                                      | 不采集实时后端                                                                    |

watch 的 PID 与 bench-run 二选一；周期 100ms–60s，时长 1s–24h；前台固定期限，SIGINT / SIGTERM 取消并写结束记录。退出码：0 正常完成，2 参数 / 绑定拒绝，3 目标丢失 / 身份变化，4 IO / 证据错误，130 取消。权限失败不能写成进程已退出。没有服务的 snapshot 成功返回空候选，不自动加载模型。

只读、无提权，无全局安装 / 常驻 daemon / 自更新。默认无网络；本机健康检查应由显式 endpoint 启用，字面回环 IP 且无凭据 / query / fragment。run 中的 endpoint 不隐式启用请求，不读取 / 输出环境凭据。进程发现只读取名称，不保存全局 argv、环境变量、模型输出或用户凭据。

## 输出与来源

输出 `schema_version = 3`，definition 按下述版本规则保存。这是独立诊断结构，`kind = observer_snapshot | observer_header | observer_sample | observer_end`，不是现有 `sample` / `extension_packet`，不能直接作为正式采集资格。Go 类型是接口源，Python 离线桥验证相同冻结结构；破坏性变化升级 definition 并说明旧流读取策略。

header 保存随机 session ID、工具版本、编译源码 SHA、自身二进制 SHA（不可读则原因）、OS / 架构、系统静态配置、周期 / 时长、固定 PID / raw start identity、可选 run ID 与两个原始文件 SHA。sample 保存连续 seq、session ID、读取前后 `elapsed_ns`、可用内存 / 磁盘、进程与自身 CPU 累计 ns / RSS bytes / CPU 单核百分比、缺测原因和可选端点状态。end 保存数量、实际结束偏移、停止原因、调度跳过次数。

header 的 `observer_target` 固定监测器自身身份；每个 process 含实际 `read_started_ns` / `read_finished_ns`，都位于该 sample 的读区间内，CPU 差值使用它们的中点。seq 从 1 连续递增。健康记录含 `checked_utc` 与 `cached`，5s 内复用结果明确标记缓存；UTC 不当作单调时钟。文件写入失败不会生成伪完整记录。

所有数据使用有界缓冲，未知为 `value = null` 且 `reason` 非空，每个观测带 `source`；成功值 reason 为 null。首个 CPU 百分比缺测，需要同身份两次计数，按实际读区间中点差计算；不是全机归一化百分比，不能从高 CPU 推断正在生成。计数回退 / 缺段清除 CPU 基线，PID 复用不自动重新绑定。Linux start 为内核 jiffy，Darwin 为原生 epoch 整数微秒，Windows 为 FILETIME 原始 100ns 单位；不能跨平台比较 start。

Linux CPU HZ 从 `AT_CLKTCK` 读取，无法读取不猜 100；RSS 来自 status，身份前后包围。Darwin proc_taskallinfo / libproc 的 CPU 经 `mach_timebase_info` 换算为 ns，Mach 可用页、sysctl；Windows GetProcessTimes / working set / GlobalMemoryStatusEx。可用内存与 working set / RSS 各自保留口径；Linux 可能仅观察当前 PID 命名空间，cgroup 配额和 GPU 驻留不推断。磁盘限定当前目录所在文件系统。

读取采用绝对调度点，落后明确累计 skipped；串行编码 / 写入，不积累无限队列。不每次刷新静态库存，不周期哈希模型大文件，不跨样本使用旧值。输出阻塞可能推迟采样，缺口显示且退出后 fsync 包含在外部开销检查中；已采未写错误退出 4，不声称完整。

## 与本项目配合

`--bench-run` 只读现有 run / config，拒绝旧 schema、重复 JSON 键、非有限数、错误数值类型、PID / start 不匹配、不可读 / 超限文件。配置的模型名 / backend 仅作已声明标签，不认证模型映射、二进制或模板。读取端点也不证明其属于 PID；健康结果单独标未验证端点归属，不取代本项目预检。

会话时钟从本进程 `time.Since` 起点产生，UTC 只用于注记；不得与 Python 原始单调时钟直接相减，也不能按时间相近自动分配 request ID。Python 离线桥读取完整新 JSONL，拒绝错误类型、超限、seq 乱序、时钟逆转、来源变化、身份复用、未正常结束和损坏；生成新诊断摘要，保留输入 hash，`performance_comparison_qualified = false`，不改旧 run / manifest / 报告。不导入虚构 token、GPU 或推理指标。

绑定文件各限 1 MiB、JSON 嵌套最多 128 层；静态发现最多 32 个候选，Linux / Darwin 发现至多读取 32768 个条目，部分发现附原因。离线流最多 512 MiB、每行 1 MiB、864001 个 sample，逐行读取，不把整套日志装入内存。低周期 / 长时日志超限会拒绝摘要，原始文件保留。未纳入首版的动态 GPU / 温度 / 能耗以 `limitations` 声明未采集；已有指标缺测为 null 并附原因。

## 风险与验收

只输出无来源的系统摘要无法解释缺测或复核；独立观察器也不直接替代 v3 采集。风险为 ABI、不同 RSS 语义、低频缺采与用户误解服务候选，分别以平台编译、原生 / fixture 拒绝回归、缺口记录和明确标签验证。GPU 动态指标 / 温度 / 能耗不支持，后续属于独立来源扩展。

## 测评来源关联

依据 [ADR 010](../decisions/010-observer-benchmark-adaptation.md)，关联不改变独立工具与原件保护边界。

### 输入与版本

Go 0.2.0 输出 `schema_version = 3`、`definition = lab_observer.v2`。header 新增必填 `disk_scope`，取值为 `benchmark_run_filesystem` / `observer_cwd_filesystem`；`--bench-run` 以所传 run 目录读取磁盘，直接 PID 以当前目录读取。静态 snapshot 仍以当前目录读取。所有 record definition 在单会话内一致。

离线桥兼容 `lab_observer.v1` 原始字节：v1 header 没有 disk_scope，解释为 `observer_cwd_filesystem_legacy_v1`。不补造旧观测或把它解释为 run 文件系统。新摘要固定 `lab_observer_summary.v2`，另记 `input_definition`。

### 完整与部分摘要

`analyze_observer.py --log LOG --out NEW.json` 默认保持严格正常结束。显式 `--allow-incomplete` 可读取消 / target_unavailable / observer_unavailable 终态，或至少一条 sample 的完整前缀缺 end；保存 `completeness = incomplete`、`stop_reason`、`end_record_present`、已观测数量、CPU 有效覆盖及缺测原因。正常截止为 complete，但这只表示监测期限完成。

非 running 进程行仅在部分模式允许，应与固定目标一致，CPU / RSS / 百分比全部 null，原因与 exited / identity_changed / unavailable 状态一致；不能重新绑定新 PID。非 running 行应是最后 sample，end 原因与目标 / 自身失败一致。来源转换只允许失败行的 `native_process` 缺测，不能把未知换成读数或延续旧值。任何序号 / 时钟 / 数值 / 原始 JSON 损坏，包括截断末行，仍拒绝。

### 整套测评关联

新增 `--bench-run SEALED_RUN`，关联离线摘要与已封存 v3 证据，不发网络请求、不读当前 PID、不启动模型。应先有监测 header 的 `benchmark_binding`；精确比对 run ID、原始 run / config 文件 SHA、config 中 PID / start 与固定观察目标。核心 manifest 和 `read_trial` 校验全部原始来源；原件变化、来源迁移、无封存或不匹配拒绝。

摘要另存 `benchmark_evidence`，含 run / experiment / trial 标识、封存 manifest 和 events SHA、benchmark 源码、diagnostic、completeness、终态 counts、监测与测评来源匹配状态。`whole_run_coverage_verified = false`、`request_alignment = not_performed_no_shared_clock`，不合并成绩或推断请求内 CPU / RSS、GPU、token。CPU 指标名称相同不证明不同采集器可互换。

原 benchmark 目录及 manifest 不添加文件，报告仍通过现有 `inferyard report` / `verify`；监测摘要写在独立新路径，不能落入源 run 目录。摘要同时绑定日志 SHA 与关联源码 SHA，全部保持 `performance_comparison_qualified = false`。

替代是猜测时间重叠或把部分流算完整，会产生假覆盖与成绩；风险由显式部分开关、严格拒绝回归及原生 fixture 全链路验证控制。首版资源预算与其他平台真机缺口继续保留。
