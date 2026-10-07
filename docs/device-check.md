# 设备检测

`device-check` 在 macOS、Linux、Windows 读取本机 CPU、内存、磁盘和可观测 GPU；可扫描已有 GGUF 元数据并估算 CPU/CUDA/Metal 容量。不下载模型、不加载张量、不发送模型请求、不管理服务。

```bash
inferyard device-check
inferyard device-check --json
inferyard device-check --model /path/to/model.gguf --out reports/device-new
inferyard device-check --models-root /path/to/models --format json
```

默认摘要适合人类；`--json` 等同 `--format json`，两个格式选项不能同时提供。格式不依赖终端类型，成功、阻断、错误和取消均沿用该格式。其他命令仍输出 JSON。帮助保留 argparse 文本，诊断走 stderr。

## 接口与职责

CLI 只选择呈现方式，应用层的 `CommandRequest` / `CommandResult` 类型不变。平台模块负责只读采集、GGUF 元数据和容量建议；业务模块不导入 CLI。格式选项不进入请求 DTO。

JSON stdout 继续使用 `CommandResult` 外层，`details.kind` 为 `device_preflight.v1`。既有 `hardware`、`models`、`recommendation`、`model_requests_sent` 保留；新增 `platform_capabilities`、`ready_to_run`、`next_steps`。容量原值为 bytes；摘要按 GiB 换算。`hardware.sources` 记录来源，`hardware.missing` 记录缺测原因；缺测为 `null`。

推荐仍使用 `status = recommended/blocked` 与 `selected`；`selected.mode` 可为 CPU/CUDA/Metal 对应的 `cpu`/`cuda`/`metal`，并保留 GPU 索引、context、threads、内存和磁盘预算字段。Metal 的 `gpu_index` 和独立 GPU 内存估算为 `null`，按主机统一内存估算容量。推荐只做容量筛选，不核验引擎、服务身份或测量资格。`ready_to_run` 始终为 `false`，下一步按平台能力给出；`metal_backend_verified` 仍为 `false`。

## 状态与输出

| 情况                                               | 外层状态      | 退出码 |
| -------------------------------------------------- | ------------- | ------ |
| 没有显式模型要求，也未发现模型                     | `inspected`   | 0      |
| 发现模型且容量建议可用                             | `recommended` | 0      |
| 显式模型缺失、GGUF/模板不适用、内存或磁盘不足/缺测 | `blocked`     | 2      |
| 本地文件或工具错误                                 | `error`       | 4      |
| 取消                                               | `interrupted` | 130    |

无模型完成只证明检测执行了；推荐仍阻断、`selected` 为 `null`。缺测与容量不足使用不同原因。查询 GPU 失败不阻断已有主机内存支持的 CPU 容量建议。

不传 `--out` 不保存文件。传入后创建新目录并写 `device-preflight.json`，包括模型建议阻断的结果；目录已存在时拒绝写入并保留原件。磁盘检查使用输出路径中最近已存在的祖先目录，不预先创建目录；无输出路径时检查当前目录。

## 平台采集与限制

| 平台    | 设备来源                                                  | 实时边界                          |
| ------- | --------------------------------------------------------- | --------------------------------- |
| Linux   | `/proc`、`/sys`、磁盘接口、可选 `nvidia-smi`              | 单次与批量按现有预检执行          |
| Windows | 注册表、psutil、磁盘接口、可选 `nvidia-smi`               | 单次 CPU/CUDA；批量限支持的引擎/协议 |
| macOS   | `sysctl`、`vm_stat`、`system_profiler`、`pmset`、磁盘接口 | 原生单次与批量；覆盖范围见平台状态  |

macOS 可用内存按 `(free + inactive + speculative) × page_size` 估算，标明口径；不再额外计入重叠的 purgeable/压缩页。Apple Silicon 的 GPU 标为共享主机内存，独立显存容量为 `null`；Metal 支持仅表示硬件观察，未验证推理后端。查询有超时，profiler 仅保存白名单字段，不保存序列号或 UUID。

Apple Silicon 上观察到 Metal 支持且主机内存足够时，可给出 `metal` 候选；未满足 Metal 条件时仍可评估 CPU。容量估算包含模型文件、启动余量和运行内存下限；磁盘需满足输出预算。候选生成时重新检测当前容量，不能凭旧报告绕过资源不足。

设备 JSON 不是正式 run 证据，不能用检测或模拟结果替代真机测评。macOS 候选准备、服务绑定及当前阻断见 [macOS 说明](../MACOS.md)；设备输出决策见 [ADR 004](decisions/004-device-check.md)，后续原生支持见 [ADR 005](decisions/005-macos-runtime.md)。

实现与原生验证分别列在[平台状态](platforms.md)。
