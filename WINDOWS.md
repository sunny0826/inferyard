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

所有工作副本共享系统公共数据目录（原生 `CSIDL_COMMON_APPDATA`，通常为 `C:\ProgramData`）中的
`local-ai-benchmark-host.lock` 和 `local-ai-benchmark-host.state.json`。
D 盘存在时先持有旧 `D:\local-ai-benchmark-host.lock`，再持有公共目录锁，并同步核验两处状态。
任一 dirty、锁占用、状态冲突或权限错误均阻断；仅 D 盘确实不存在时单独使用公共目录。
文件及最终路径拒绝 reparse point。无 D 盘/多账户场景的当前原生覆盖仍待验证。

`Ctrl+C` 记录取消；强制结束进程不能保证收尾。按[恢复步骤](docs/usage.md#取消与-dirty-恢复)
确认旧服务消失、新身份与空闲，不删除锁文件。CLI 不替操作者停止或重启模型服务。
文件内容使用 `fsync`，快照通过 `MoveFileExW(WRITE_THROUGH)` 发布；`directory_fsync: false`
如实保留，不能授予与 Linux 相同的断电目录持久性资格。

`report`、`compare`、`verify` 和显式 `migrate` 可离线使用；迁移/派生输出写入新目录。
`verify` 支持原始 run/batch/冻结计划与派生产物，具体格式见[CLI 参考](docs/cli-surface.md)。

## Windows engine-fit 诊断

`engine-fit run` 仅支持单 GGUF llama.cpp，使用独立 run.v6；其他 Windows 引擎在请求前拒绝。
计划、外部服务和完整命令见[引擎指南](docs/engine-fit-common.md)。
它记录原生进程树 working-set/CPU 秒、主机内存及可得 NVIDIA 温度；CPU 温度缺测。
这些诊断与正式质量 run 分开，契约见[Windows engine-fit](docs/contracts/engine-fit-contract.md#windows-原生来源)。
源码开发使用[贡献指南](CONTRIBUTING.md)中的 mise/uv 命令。
