# 配置与外部服务

安装用户先用 `inferyard init --out NEW` 导出三平台模板和题包；完整准备顺序见[安装指南](../docs/installation.md)。
本目录只保留三份可公开 Windows 示例，真实设备 candidate/bound/frozen 配置留在本机。
示例中的路径、SHA、字节数、设备参数和 PID 必须替换，不能直接用于真机。

## 准备与绑定

| 平台 | 准备方式 |
| --- | --- |
| Linux x64 | `config assets` 导出 GGUF 原模板、哈希和库清单，填写 init 导出的 linux.example.toml |
| macOS arm64 | `device-check` 后用 `config create --engine FILE` 生成 CPU/Metal 候选 |
| Windows x64 | `runtime prepare` 核验固定归档，再用 `config create --runtime-receipt FILE` 生成 CPU/CUDA 候选 |

资产路径与哈希必须对应实际字节；清单表示实际运行依赖。候选未绑定，不是运行授权。
操作者按 `engine.binary_path` 和完整 `engine.startup_args` 在外部终端启动服务，然后绑定实际 PID：

```bash
inferyard config bind --candidate bench-work/candidate/candidate.toml --pid 1234 --endpoint http://127.0.0.1:48857 --out bench-work/bound
inferyard run --config bench-work/bound/config.toml
```

绑定读取进程身份和 argv，不发送模型请求。`run` 执行当次身份预检及普通/流式探测；
独立 `probe` 仅用于排障。CLI 不下载、启动、终止或重启服务。
凭据仅填写 `endpoint.api_key_env` 环境变量名，不把明文放入配置、参数或日志。

## 配置规则

- `execution.warmup_count` 为整数 0..3，省略时 3；选择在运行前冻结。
- 容量、温度、磁盘及其他停止条件来自实际设备和协议，不降低阈值以掩盖阻断。
- 环境准入、冷启动和 clean 复用规则见[使用指南](../docs/usage.md#描述性比较与复用)。
- 模型、模板、引擎、动态库、启动参数、PID 和启动时间均须重新核验；历史配置不继承新服务身份。
- 非 KVMem/NInfer 的可选 `model.component_ledger_path` 按配置来源目录解析；冻结包移动后使用封存来源，旧投影兼容见[数据契约](../docs/data-contract.md)。
- 正式题包须有匹配的人工审核；改题、答案或规则后重审受影响项，`--diagnostic` 不代替审核。

Prism 手工配置可参考 [Windows 示例](qwen3-4b.windows.example.toml)；它是占位资产声明，不携带可复用的实机身份。

## KVMem / NInfer

[Windows KVMem 示例](windows-kvmem.example.toml)与[NInfer 示例](windows-ninfer.example.toml)
用于固定质量、串行文本配置。NInfer 需组件账本，全部资产需实际字节/哈希及完整 argv/cwd/PID/FILETIME。
默认 `observation_mode = "auto"`，缺 lab 协议时披露原生信号；`lab_required` 要求完整协议。
库清单与进程实际映射双向核验，不能把发行目录的全部 DLL 直接当作运行依赖。
字段、支持范围及错误处理见[适配契约](../docs/contracts/windows-ninfer-adaptation-contract.md#版本身份与参数)。

## 公开包与复现

`public package` 只创建本地脱敏包。`verify --path PUBLIC --config LOCAL.toml` 核对本地声明，
可加 `--source-run RUN` 核对原运行投影；声明匹配仍为 `ready_to_run: false`。
`public plan` 冻结复现计划，实际 run 仍须通过实时预检。命令见[使用指南](../docs/usage.md#公开包与扩展)。
