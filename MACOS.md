# macOS 使用说明

目标为 Apple Silicon（arm64）。支持原生单次与批量执行、CPU/Metal 身份核验和资源采集。
历史模型覆盖及当前候选未验项统一见[平台状态](docs/platforms.md)。

## 准备候选与外部服务

先按[安装指南](docs/installation.md)安装，再导出工作区并检测已有模型：

```bash
inferyard init --out bench-work --bundle zh-smoke
inferyard device-check --model /path/to/model.gguf --out bench-work/preflight --json
inferyard config create --preflight bench-work/preflight/device-preflight.json --engine /path/to/llama-server --bundle bench-work/bundles/zh-smoke.json --results bench-work/results --out bench-work/candidate
```

候选生成目前要求 Prism `prism-b10743-adfffbe`，引擎与动态库放在同目录；不自动下载或构建。
它执行一次有界 `--version`，重查容量和 AC，读取 GGUF 模板与资产哈希。
Metal 需要已绑定的 `libggml-metal.dylib`，实际加载由运行预检核验。

在外部终端按 `candidate.toml` 中的 `engine.binary_path` 和完整 `engine.startup_args` 启动服务。
不要复用历史 PID；以实际 PID 和端点绑定：

```bash
inferyard config bind --candidate bench-work/candidate/candidate.toml --pid 1234 --endpoint http://127.0.0.1:48857 --out bench-work/bound
inferyard run --config bench-work/bound/config.toml
inferyard report --runs bench-work/results/RUN_ID --out bench-work/report
inferyard verify --path bench-work/report
```

工具不更改电源策略、不管理模型服务；运行结束后由操作者关闭服务。
批量计划、取消和 dirty 恢复见[使用指南](docs/usage.md)。

## 原生采集边界

进程身份使用启动时间和文件身份，`lsof` 核验二进制、监听与 Metal 动态库。
模型绑定来自启动参数指向的文件，模型映射仍为未观察；Metal 能力与库加载不证明模型 GPU 驻留。

`macos-resource.v1` 保存 CPU、RSS、主机可用内存及换页；只读 AppleSMC 温度记录实际来源与缺测原因。
换页属于主机，不直接归因到模型。统一内存不能与 RSS 相加；独立显存、频率、功耗和能耗未采集时为 null。
电源来自 `pmset`，不填造 Linux governor/EPP；所需策略或传感器缺失时按协议阻断或披露。

首次实时入口获得主机锁时自动创建 clean 状态，不需要显式初始化命令。锁为 `/var/tmp/inferyard-host.lock`，状态为同目录的 `inferyard-host.state.json`。
旧工具文件不读取、不加锁；隔离根进程测试不构成真实主机部署验收。
取消后若无法确认排空，保留 dirty；不要删除锁/state 绕过恢复。

## 引擎诊断与开发

同机引擎诊断使用 [engine-fit 指南](docs/engine-fit-common.md)，MLX-LM/LM Studio 仍由操作者启动。
MLX 受控服务脚本路径可从 `inferyard engine-fit engines` 查询；不从源码目录猜路径。
开发环境和源码命令见[贡献指南](CONTRIBUTING.md)，实现细节见[macOS 契约](docs/contracts/macos-contract.md)。
