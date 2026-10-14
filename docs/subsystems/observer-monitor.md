# Observer 监测器

`inferyard-observer` 是 [`observer/`](../../observer/README.md) 下的独立 Go module（`module inferyard/observer`，单 `go.mod`、无第三方依赖），对系统配置与指定的模型服务进程做进程外只读监测。
每个 OS/架构 一个可执行文件、默认无网络、无配置文件、无常驻 daemon、不自动加载模型。
界面为命令行 + JSON，产品定位、命令示例与历史验证范围以 [`observer/README.md`](../../observer/README.md) 为准。

它与 CLI 和 Studio 都没有进程内耦合：CLI 只在构建（[`build_observer.py`](../../cli/scripts/build_observer.py)）与离线摘要（[`analyze_observer.py`](../../cli/scripts/analyze_observer.py)）两个脚本层与之交互，交互介质是磁盘上的二进制与 JSONL/封存 run 文件。
它是独立诊断来源，不改变 CLI 的 v3 样本结构、正式指标或比较资格，也不替代主机锁、身份预检与安全停止。
监测器的接口定义、资格边界与失败表见 [observer 契约](../../cli/docs/contracts/observer-contract.md)，设立理由见 [ADR 009](../../cli/docs/decisions/009-native-observer.md) 与 [ADR 010](../../cli/docs/decisions/010-observer-benchmark-adaptation.md)。
正式的执行与采集通路见 [运行时执行与引擎适配](runtime-execution.md) 与 [平台采集与设备身份](platform-sampling.md)；封存与离线读取见 [证据、封存与离线读取](evidence-storage.md)。

## 职责与事实源

| 部分 | 责任 | 事实源 |
| --- | --- | --- |
| 命令行入口 | `snapshot` / `watch` 分派、`--help` / `--version`、参数有界校验、退出码映射、SIGINT/SIGTERM 取消、SIGPIPE 忽略 | `main`、`run`、`parse`、`reject` 在 [`observer/cmd/inferyard-observer/main.go`](../../observer/cmd/inferyard-observer/main.go)；采样循环 `watch` 在 [`observer/cmd/inferyard-observer/watch.go`](../../observer/cmd/inferyard-observer/watch.go) |
| 输出类型 | `lab_observer.v2` 记录结构：snapshot / header / sample / end；缺测 `Metric`（`value = null` + `reason` + `source`，成功值 reason 为 null） | `Definition`、`Metric`、`Known`、`Missing`、`Envelope`、`Base`、`Snapshot`、`Header`、`Sample`、`End`、`Target`、`Process`、`Binding` 在 [`observer/internal/observe/types.go`](../../observer/internal/observe/types.go) |
| 平台原生读取 | 进程 CPU/RSS/启动身份、主机可用内存/磁盘、系统静态配置、候选进程发现 | `NativeProcess`、`NativeHost`、`NativeInventory`、`NativeDiscover` 按平台实现：[`native_darwin.go`](../../observer/internal/observe/native_darwin.go)（cgo + libproc/Mach/IOKit，CPU 经 `mach_timebase_info` 换算 ns）、[`native_linux.go`](../../observer/internal/observe/native_linux.go)（`/proc`，HZ 从 `AT_CLKTCK` 读取）、[`native_windows.go`](../../observer/internal/observe/native_windows.go)（toolhelp/GetProcessTimes/working set/GlobalMemoryStatusEx）；Linux 文本解析 `parseProcStat`、`parseMemValue`、`ticksNS` 在 [`proc_parse.go`](../../observer/internal/observe/proc_parse.go) |
| CPU 差值与身份防护 | 同身份两次计数差按读区间中点求 CPU 百分比；PID 复用永久拒绝重绑；counter 回退清基线 | `Tracker`、`NewTracker`、`Tracker.Read`、`IsCandidate`、`ErrorReason` 在 [`observer/internal/observe/process.go`](../../observer/internal/observe/process.go) |
| run 绑定 | 只读 v3 `run.json` / `config.frozen.json`，严格 JSON 校验并记录文件 SHA 与已声明标签 | `ReadBinding`、`StrictJSON`、`boundedFile` 在 [`observer/internal/observe/binding.go`](../../observer/internal/observe/binding.go) |
| 健康端点 | 显式可选、字面回环 HTTP，只 GET `/health`，250ms 超时、5 秒节流、不重定向 | `NewHealthProbe`、`HealthProbe.Read` 在 [`observer/internal/observe/health.go`](../../observer/internal/observe/health.go) |
| 构建 | 六目标交叉编译、源码摘要、构建回执、版本与 source hash 注入 | `build`、`source_digest`、`GO_VERSION`、`TOOL_VERSION`、`TARGETS` 在 [`cli/scripts/build_observer.py`](../../cli/scripts/build_observer.py) |
| 离线摘要 | 逐行有界严格校验 JSONL、生成摘要、可选关联封存 run | `analyze`、`main` 在 [`cli/scripts/analyze_observer.py`](../../cli/scripts/analyze_observer.py)；流校验 `Series`、`require`、`metric`、`header` 在 [`observer_stream.py`](../../cli/scripts/observer_stream.py)；关联 `link_benchmark`、`analysis_source_hash`、`output_outside_run` 在 [`observer_benchmark.py`](../../cli/scripts/observer_benchmark.py) |

关键状态与配置的事实源：definition 与 `schema_version = 3` 的冻结结构以 [`types.go`](../../observer/internal/observe/types.go) 的 Go 类型为接口源，Python 离线桥验证同一结构，语义约束见 [observer 契约](../../cli/docs/contracts/observer-contract.md)。
工具版本 `0.2.0` 同时硬编码在 `main.go` 的 `version` 与 `build_observer.py` 的 `TOOL_VERSION`，构建时经 `-ldflags -X main.version -X main.sourceHash` 注入二进制。
未经构建注入的 `sourceHash` 在 header 中记为 `unbound_development_build` 缺测（`buildSource`）；二进制自身 SHA 由 `binaryHash` 读取当前可执行文件字节计算，读取期间文件被替换则记缺测。

## 运行路径

1. **构建**：`build_observer.py build` 计算 `observer/**/*.go`、`observer/go.mod` 与脚本自身的 `source_digest`；`go version` 必须恰为 `GO_VERSION`（不符即 `go_version_does_not_match_mise`），darwin 目标还要求本机 Apple SDK 与 clang 并设最低系统 13.0，Linux/Windows 关闭 cgo。逐目标 `go -C observer build -trimpath`，记录大小与字节 SHA（大小预算 10 MB），写 `build-receipt.json`（含 `native_validation_granted = false`）；输出目录必须新建（拒绝覆盖），全部目标完成后复查源码摘要，构建期间源码变化即失败。
2. **snapshot**：`run` 识别 `snapshot` 后调 `NativeDiscover` 枚举候选进程（进程名经 `IsCandidate` 限定为 `llama-server`、`ollama`、`vllm`、`mlx_lm.server`，Python 启动的服务名可能识别不了，可直接传 PID）、`NativeInventory` 取 OS/架构/CPU/内存/GPU 设备列表等静态配置、`NativeHost(cwd)` 取当前目录文件系统的可用内存/磁盘，编码单个 `observer_snapshot` JSON 到 stdout。发现过程失败时结果附 `discovery_reason`；无候选返回 `[]`，不自动加载模型。
3. **watch 启动**：`parse` 校验 `--pid` 与 `--bench-run` 恰选一、周期 100ms–60s、时长 1s–24h、PID 有界；`NewHealthProbe` 校验显式 endpoint 必须是字面回环 IP 的纯 HTTP（无凭据/query/fragment，否则拒绝）。随后 `NativeProcess` 读取目标与监测器自身进程身份；`--bench-run` 模式先经 `ReadBinding` 从 run 目录读取 v3 `run.json` / `config.frozen.json`（`StrictJSON` 拒绝重复键/嵌套超限/非有限数/尾随值，文件各限 1 MiB），取 `server_pid` / `process_start_ticks` 与 `run_id`，并在采样前与实时读数核对启动身份（不符即 `benchmark_process_identity_changed`）。`--out` 以 `O_EXCL` 独占创建新文件，benchmark 模式下 `outputInsideRun` 经符号链接解析拒绝写进 run 目录。最后写 `observer_header`：随机 session ID、工具/源码/二进制 SHA、系统静态配置、周期/时长、固定目标与监测器自身身份、可选 `Binding`（run 文件 SHA 与已声明模型/后端标签）、`clock` 注明会话单调时钟非 benchmark 时钟、`disk_scope` 区分 run 文件系统与当前目录。
4. **采样循环**：按绝对调度点等待到下一周期（落后则把缺口累计为 `skipped_intervals` 并顺延，不补采）。每条 `observer_sample` 串行编码写入：目标进程与监测器自身各经一次 `NativeProcess` + `Tracker.Read`，`NativeHost(diskPath)` 按 header 的 `disk_scope` 读可用内存/磁盘，显式 endpoint 每 5 秒至多探测一次 `/health`（结果带 `checked_utc`，5 秒内复用标记 `cached`）。采样中目标状态非 `running` 立即停采：目标失败写 `stop_reason = target_unavailable` 退出 3，监测器自身失败退出 4；`Ctrl+C`/SIGTERM 写 `cancelled` 记录退出 130。
5. **收尾**：写 `observer_end`（样本数、实际耗时、跳过数、停止原因；`PerformanceQualified` 固定 `false` 且代码从不置真），`--out` 模式 `Sync` 后再关闭；消费方提前关闭 stdout 时监测器以 4 退出而不是被 SIGPIPE 杀死。
6. **离线摘要**（CLI 侧，监测器本身不调用 Python）：`analyze_observer.py` 的 `analyze` 用 `Series` 逐行有界读取并严格校验 JSONL（序号连续、时钟单调、来源一致、进程身份不复用），默认只接受正常截止的完整日志，`--allow-incomplete` 显式接受取消/目标退出/缺 end 的完整前缀并记 `completeness = incomplete`；截断 JSON、来源变更等损坏输入仍拒绝。`--bench-run SEALED_RUN` 时 `link_benchmark` 经核心 manifest 与 `read_trial` 核验已封存 run 的 config 哈希和 PID/启动身份，把摘要关联为 `benchmark_evidence`，摘要写在 run 目录之外的新路径，不新增或改写源文件。

### 采样状态与结束原因

- 进程行状态：`running`；目标退出为 `exited`（`process_exited`），同 PID 启动身份变化为 `identity_changed`，权限/其他读取失败为 `unavailable`，三者都使该 PID 进入 `Tracker.lost`，后续样本不再重新绑定（PID 复用防护），对应 CPU/RSS/百分比全部缺测并附原因。
- 首条样本 CPU 百分比恒为 `first_observation` 缺测：百分比是同身份两次 `cpu_total_ns` 计数按两次读区间中点之差求得（来源 `counter_delta_over_monotonic_interval`），计数回退或时钟逆转记 `counter_or_clock_regressed` 并清除基线。CPU 为单核百分比，多线程可超过 100%。
- `observer_end.stop_reason` 取值：`duration_reached`（正常，退出 0）、`cancelled`（退出 130）、`target_unavailable`（退出 3）、`observer_unavailable`（退出 4）。已采样本写入失败立即以 4 退出，不生成伪完整记录。

## 约束与失败行为

- 只读、无提权、默认无网络；唯一网络行为是显式 `--endpoint` 的 `/health` GET。进程发现只读进程名，不保存全局 argv、环境变量、模型输出或凭据；GPU 设备列表是库存枚举，不证明后端可用，GPU 动态占用/温度/能耗/token/队列未采集（`InventoryBase` 的 `limitations` 随证据保存）。
- 退出码与失败映射的规范表（0 正常、2 参数/绑定拒绝、3 目标丢失或身份变化、4 IO/证据错误、130 取消）见 [observer 契约](../../cli/docs/contracts/observer-contract.md)；代码中权限失败经 `ErrorReason` 映射为 `permission_denied`，不得写成进程已退出。
- 绑定与版本边界：`--bench-run` 只支持 layout1 的实际 v3 `run.json` / `config.frozen.json`；ADR 048 的 layout2 容器不在支持范围（[observer 契约 §测评来源关联](../../cli/docs/contracts/observer-contract.md)），新布局可显式 `--pid` 生成独立诊断流。旧 schema、重复 JSON 键、非有限数、PID/启动身份不匹配、不可读或超限文件均拒绝；配置中的模型名/后端只是已声明标签，不认证模型映射，端点归属也不因可达而视为已验证。
- 输入版本边界：离线桥只接受 `lab_observer.v2` 原始流，v1 返回 `unsupported_format` / 2 且不生成摘要，旧流交原项目处理；该支持边界由 [ADR 038](../../cli/docs/decisions/038-inferyard-current-format.md) 局部取代原兼容承诺。所有缺测为 `null` 附原因与来源，不补造观测、不导入虚构 token/GPU/推理指标。
- 资格边界：监测时钟不与 benchmark 请求时钟对齐（header `clock` 显式声明），不声称覆盖整个测评、不按时间相近分配请求 ID、不合并每题指标；摘要与关联结果始终 `performance_comparison_qualified = false`。已保存覆盖见 [平台状态](../../cli/docs/platforms.md)：仅 macOS arm64 有原生监测与合成服务关联证据，六目标交叉编译不代表 Linux/Windows/macOS amd64 原生通过；合成服务不加载推理权重，软件/模拟验证不授予真机资格。
- 输出侧约束：`--out` 独占创建（已存在即 `output_create_failed_or_already_exists` / 4），benchmark 模式必须位于 run 目录之外；磁盘可用量按 header `disk_scope` 读取对应文件系统；离线流有界（日志总大小、行长与样本数上限见 observer 契约），超限拒绝摘要且原始文件保留。

### 与 CLI 的交互面

CLI 侧只有脚本层与本监测器交互，没有进程内客户端：

- [`build_observer.py`](../../cli/scripts/build_observer.py)：交叉编译与回执生成，上文运行路径第 1 步。
- [`analyze_observer.py`](../../cli/scripts/analyze_observer.py) 及库 [`observer_stream.py`](../../cli/scripts/observer_stream.py)、[`observer_benchmark.py`](../../cli/scripts/observer_benchmark.py)：离线严格校验与封存关联，上文第 6 步。退出码 0 正常、2 不支持格式、3 部分摘要、4 输入/证据/输出错误（[observer 契约 §完整与部分摘要](../../cli/docs/contracts/observer-contract.md)）。
- 命名含 observer 但属于其他通路的 CLI 模块：[`engine_fit_observer_io.py`](../../cli/src/inferyard/platforms/engine_fit_observer_io.py) 是 engine-fit LM Studio 路径调用 `lms` CLI 观察器的有界子进程读取（`read_process`、`TOTAL_TIMEOUT`，使用方 [`engine_fit_lmstudio.py`](../../cli/src/inferyard/platforms/engine_fit_lmstudio.py)）；[`total_observer_control.py`](../../cli/src/inferyard/extensions/total_observer_control.py) 是 `off/on/on/off` 四臂总开销控制实验（`run_total`、`assess_total`）。二者不调用 `inferyard-observer`（详见文末差异记录）。

## 验证

```sh
# 工具链核对（mise 固定 go 1.27.2 / python 3.14.7 / uv 0.12.18）
mise which go
mise exec -- go version

# 构建（从仓库根；--out 必须是新目录，--target darwin/arm64 等可只构建单目标）
mise exec -- uv run --project cli --frozen python cli/scripts/build_observer.py --out dist/observer

# Go 单测与静态检查（从仓库根；darwin 目标需要 cgo 与 Apple SDK，与 observer README 一致）
mise exec -- go -C observer test -race ./...
mise exec -- go -C observer vet ./...

# 离线摘要（只读；--bench-run 要求 run 已封存，输出不落入源 run 目录）
mise exec -- uv run --project cli --frozen python cli/scripts/analyze_observer.py --log monitor-new.jsonl --out summary-new.json
mise exec -- uv run --project cli --frozen python cli/scripts/analyze_observer.py --log attached-new.jsonl --bench-run reports/SEALED_RUN --out linked-new.json

# CLI 侧离线桥与关联回归（在 cli/ 目录内，pytest/ruff 用法对照 cli/pyproject.toml 与 cli/CONTRIBUTING.md）
cd cli && mise exec -- uv run --frozen pytest -q tests/unit/test_observer_analysis.py tests/unit/test_observer_benchmark.py tests/unit/test_observer_adaptation.py
cd cli && mise exec -- uv run --frozen ruff check src tests scripts

# 原生集成：真实 Go 二进制 + TCP 合成服务，仅 darwin，夹具自行构建二进制
cd cli && mise exec -- uv run --frozen pytest -q tests/integration/test_observer_benchmark_native.py

# CLI 侧观测器开销控制回归（四臂 ABBA 与写盘开销剖析，非本监测器通路）
cd cli && mise exec -- uv run --frozen pytest -q tests/unit/test_total_observer_control.py tests/unit/test_observer_cost_profile.py tests/unit/test_observer_read_costs.py
```

## 与既有文档的差异

- [`observer/go.mod`](../../observer/go.mod) 声明 `go 1.27.1`，而 [observer README](../../observer/README.md)、[贡献指南](../../cli/CONTRIBUTING.md) 与 [mise.toml](../../mise.toml) 固定工具链 Go 1.27.2，且 `build_observer.py` 对 `go version` 不等于 `go1.27.2` 直接失败。go.mod 的 `go` 行是 module 最低语言版本，构建与验证以 1.27.2 为准。
- [`cli/scripts/README.md`](../../cli/scripts/README.md) 的「观察器」分类只列出 `build_observer.py` 与 `analyze_observer.py`；`observer_benchmark.py`、`observer_stream.py` 是 `analyze_observer.py` 导入的关联/流校验库（脚本 README 未单列），`observer_cost_profile.py`、`observer_read_costs.py` 则是 CLI 内部观测器写盘开销剖析工具，不属于监测器构建与分析通路。
- 任务描述把 [`engine_fit_observer_io.py`](../../cli/src/inferyard/platforms/engine_fit_observer_io.py) 与 [`total_observer_control.py`](../../cli/src/inferyard/extensions/total_observer_control.py) 列为本监测器的 CLI 交互面；源码核对（cli/src 中无任何 `inferyard-observer` 引用）显示二者均不调用本监测器，上文「与 CLI 的交互面」按实际归属描述。
