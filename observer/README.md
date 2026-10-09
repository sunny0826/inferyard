# inferyard-observer

配合 InferYard 使用的独立系统 / 模型进程监测器。当前版本 0.2.0，Go 标准库实现，每个 OS / 架构只有一个可执行文件；运行不需要 Go、Python、配置文件或额外库。界面为命令行 + JSON。

```bash
inferyard-observer snapshot
inferyard-observer watch --pid 12345 --interval 1s --duration 30s --out monitor-new.jsonl
inferyard-observer watch --bench-run reports/EXISTING_RUN --duration 30s --out attached-new.jsonl
```

`snapshot` 输出系统版本、CPU、逻辑核心、总内存、GPU 设备信息、当前可用内存 / 磁盘及候选服务 PID。候选名称限定 `llama-server`、`ollama`、`vllm`、`mlx_lm.server`，Python 启动的服务等名称可能无法自动识别，可直接传 PID。没有候选时返回 `[]`，不自动加载模型。

`watch` 每秒读取指定进程与监测器自身的 CPU / 内存，输出 JSONL。默认 30 秒，前台运行；`Ctrl+C` 写取消记录并退出 130。输出文件应是新路径；默认无网络、无全进程周期扫描、不启动系统命令。可显式添加 `--endpoint http://127.0.0.1:PORT`，只 GET `/health`，250ms 超时、最多每 5 秒访问一次，不重定向、不推理。

CPU 按单核百分比展示，多线程可超过 100%；首条需要建立基线而为 null。内存为 bytes，未知值附原因和来源。进程运行、端点可达、模型已加载、正在生成分别看待；本工具只直接证明前两项。GPU 设备列表不证明后端可用；GPU 动态占用、温度、能耗、token 和队列未采集。

`--bench-run` 只读 v3 的 `run.json` / `config.frozen.json`，核验当前 PID / 启动身份，记录文件 hash 和已声明的模型 / 后端标签。可在 benchmark 已创建这两个文件后开始观察；磁盘可用量读取 run 所在文件系统，直接 PID 模式读取当前目录。`--out` 应在 run 目录外。端点不会从配置自动启用。不替代本项目安全、身份、主机锁和正式指标，也不授予严格性能比较资格。旧运行的服务已退出时会拒绝新的实时绑定。

## 构建与离线读取

仓库开发环境由 mise 固定 Go 1.27.1、Python 3.14.7、uv 0.12.18。先按[贡献指南](../CONTRIBUTING.md)准备环境，再运行：

```bash
mise which go
mise exec -- go version
mise exec -- uv run --frozen python scripts/build_observer.py --out dist/observer
mise exec -- go -C observer test -race ./...
mise exec -- go -C observer vet ./...
```

构建脚本默认生成 macOS / Linux / Windows × arm64 / amd64 的六个文件，以及独立 SHA 回执；拒绝覆盖目录。可用 `--target linux/amd64` 等只构建一个目标。macOS 构建应有 Apple SDK 与 clang，编译时设最低系统 13.0；Linux / Windows 关闭 cgo。最低版本设置不替代旧系统真机验证。复制对应二进制即可运行，回执不是运行依赖。

离线摘要工具是项目可选辅助，监测器本身不调用 Python：

```bash
mise exec -- uv run --frozen python scripts/analyze_observer.py \
  --log monitor-new.jsonl --out summary-new.json

# benchmark 完成并封存后，可关联原始来源；服务退出后仍可离线核验
mise exec -- uv run --frozen python scripts/analyze_observer.py \
  --log attached-new.jsonl --bench-run reports/SEALED_RUN --out linked-new.json

# 保留取消、目标退出或缺结束记录的完整采样前缀，摘要退出码为 3
mise exec -- uv run --frozen python scripts/analyze_observer.py \
  --log interrupted.jsonl --allow-incomplete --out partial-new.json
```

它只接受 `lab_observer.v2` 原始流，校验序号、时钟、来源与进程身份。v1 返回 `unsupported_format` / 2，不生成摘要，旧流交原项目处理。摘要生成峰值采样内存、有效区间 CPU 均值、主机最低可用资源、日志和分析源码 hash。默认只读正常截止的完整日志；显式部分模式仍拒绝截断 JSON、来源变更等损坏输入，缺失时长与跳过计数为 null 并附原因。

关联模式应有匹配的监测 header，并通过核心 manifest / `read_trial` 核验已封存 run、config 哈希和 PID / 启动身份。摘要写在 run 外，不新增或改写源文件；保留测评终态数量与诊断状态。监测时钟未与请求时钟对齐，因此不声称覆盖整个测评、不合并每题指标。摘要的 CPU 不包含冷启动与最终同步，不能代替外部全流程开销测量。摘要正常退出 0、不支持格式退出 2、部分退出 3、输入 / 证据 / 输出错误退出 4。

## 历史验证范围

源项目的六个目标已编译；macOS arm64 已做原生进程、退出 / 取消、只读绑定、模拟健康端点及 30 秒资源测试。它还与真实 TCP 合成服务的 benchmark 并行执行、离线关联及报告核验。合成服务不加载推理权重，不能代替实际模型验收。这些记录不构成 InferYard 当前源码的原生验证；Linux / Windows 和 macOS amd64 尚无上述原生运行证据。重命名后的软件检查见 [backlog](../docs/backlog.md)，不把交叉编译写成全平台验收。

接口见[observer 契约](../docs/contracts/observer-contract.md)，平台覆盖统一见[平台状态](../docs/platforms.md)。
