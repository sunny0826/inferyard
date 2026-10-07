# Windows 使用说明

目标为 Windows x64。正式串行文本入口支持 Prism、KVMem 和 NInfer；
后两者限固定质量计划。实现与历史原生覆盖分别见[平台状态](docs/platforms.md)。

## 设备预检与单次运行

安装 uv 与本地 wheel 后，在 PowerShell 使用已安装的 `inferyard`，不依赖仓库脚本或 D 盘。
完整 CPU/CUDA 归档准备见[安装指南](docs/installation.md#windows-x64先准备固定-runtime)。

```powershell
inferyard init --out bench-work --bundle zh-smoke
inferyard device-check --model 'C:\Models\model.gguf' --out 'bench-work\preflight' --json
inferyard runtime prepare --profile prism-b10743-adfffbe-win-cpu-x64 `
  --archive 'C:\Downloads\llama-prism-b10743-adfffbe-bin-win-cpu-x64.zip' `
  --out 'bench-work\assets\runtime'
inferyard config create --preflight 'bench-work\preflight\device-preflight.json' `
  --runtime-receipt 'bench-work\assets\runtime\runtime-receipt.json' `
  --bundle 'bench-work\bundles\zh-smoke.json' --results 'bench-work\results' `
  --out 'bench-work\candidate'
```

示例选择 CPU profile，须与预检推荐的 mode 一致；CUDA 需对应 profile 和 runtime 归档。
准备命令不下载或启动服务，`runtime prepare` 和 `config create` 会执行已核验引擎的 `--version`。
候选引用 runtime 目录的 EXE/DLL 与清单，不复制它们；移动资产后重新创建候选。

在另一个终端按候选的完整 `engine.startup_args` 启动服务，确认实际监听 PID 后绑定：

```powershell
inferyard config bind --candidate 'bench-work\candidate\candidate.toml' `
  --pid 1234 --endpoint http://127.0.0.1:48857 --out 'bench-work\bound'
inferyard run --config 'bench-work\bound\config.toml'
inferyard report --runs 'bench-work\results\RUN_ID' --out 'bench-work\report'
inferyard verify --path 'bench-work\report'
```

`RUN_ID` 取自运行结果。路径含空格时加引号；`1234` 换成实际 PID，不给 PowerShell 的只读 `$PID` 变量赋值。
KVMem/NInfer 需按[配置说明](configs/README.md#kvmem--ninfer)准备资产清单及参数，再用同一 `config bind` 入口。

## 采集与支持边界

身份核验使用同账户进程、完整创建 FILETIME、EXE/DLL、argv/cwd、模型文件和唯一 loopback listener。
数字 PID、服务 `/health` 成功或有 GPU 设备均不能代替这些检查。

Prism 的 `windows-resource.v1`、KVMem/NInfer 的 `windows-memory.v1` 采集 RSS 和主机可用内存；温度、频率、功耗、能耗等未采集字段保留 null 和原因。
不以 WMI `AdapterRAM` 证明大显存容量，不把 ACPI 热区当 CPU 封装温度，不把提交量当换页次数。
Linux governor/EPP 缺少对应来源时保持未知。

Windows live 开销、实时扩展和 prepare-length 等平台专用入口仍受限制。
KVMem/NInfer 的 `auto` 模式可使用原生 OpenAI 生成信号，但不推断精确模板预算、有效参数或引擎内部排空。
显式 `lab_required` 要求完整 lab 协议；详见[适配契约](docs/contracts/windows-ninfer-adaptation-contract.md)。

## 锁、持久化与离线证据

首次实时操作前运行 `inferyard host-state migrate`。新锁与状态位于原生
`CSIDL_COMMON_APPDATA`（通常为 `C:\ProgramData`）中的 `inferyard-host.lock` 与
`inferyard-host.state.json`，不可变凭据为同目录 `inferyard-host-migration.json`。
维护时按 D 旧锁（存在时）→公共旧锁→新锁获取，先退休公共旧 state，再退休 D state。
原旧锁 inode 保留；任一旧 dirty、锁占用、状态冲突或权限错误阻断。无 D 盘不额外禁止部署。
ready 后正常运行只用新状态与凭据，不再桥接旧状态。固定旧基线在公共退休标记处拒绝。
文件拒绝 reparse point，保留 file fsync 与 MoveFileExW(WRITE_THROUGH)；目录 fsync 不可用。
本次 Windows 原生迁移、ACL、卷变化与持久化尚未验，临时目录模拟及 Go 交叉构建不算原生证据。
完整事务、已知边界和调查指引见[当前格式契约](docs/contracts/inferyard-current-format.md)。

`report`、`compare`、`verify` 可离线使用；仅接受当前格式，旧证据回原项目处理。
派生产物写新目录，不能把格式拒绝当成证据完整性通过。

## Windows engine-fit 诊断

`engine-fit run` 仅支持单 GGUF llama.cpp，使用独立 run.v6；其他 Windows 引擎在请求前拒绝。
计划、外部服务和完整命令见[引擎指南](docs/engine-fit-common.md)。
它记录原生进程树 working-set/CPU 秒、主机内存及可得 NVIDIA 温度；CPU 温度缺测。
这些诊断与正式质量 run 分开，契约见[Windows engine-fit](docs/contracts/engine-fit-contract.md#windows-原生来源)。
源码开发使用[贡献指南](CONTRIBUTING.md)中的 mise/uv 命令。
