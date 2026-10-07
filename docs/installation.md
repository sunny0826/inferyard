# 使用 uv 安装与准备测评

目标版本为 `0.0.1`，当前为源码与本地安装候选，尚未发布发行版本或上传 PyPI。项目采用 [MIT 许可证](../LICENSE)。
安装文件应来自维护者提供的同一候选批次；平台实现与未验项见[平台状态](platforms.md)，接口见 [CLI](cli-surface.md)。
证据与比较结论遵循[证据血缘规则](data-contract.md#证据血缘与比较结论)。

## 1. 安装 uv 和工具

从 [uv 官方安装页](https://docs.astral.sh/uv/getting-started/installation/)选择 Windows
PowerShell、macOS 或 Linux 的独立安装器或包管理器。社区使用无须安装 mise、手动安装 Python
或克隆源码。安装器完成后重开终端；若 uv 不在 PATH，按安装器提示添加其 bin 目录。
工具入口不在 PATH 时运行 `uv tool update-shell`，再重开终端；本工具不会自行改 shell 配置。

向维护者取得同一批次的 wheel、`runtime-constraints.txt` 和 SHA256 清单，先核对摘要。
同时查看该候选的源码外安装结果及已验/未验平台；安装通过不表示原生模型准备或性能实验通过。
候选至少需要一个平台的实际安装检查；其余平台标为 `not_verified`。维护者从构建、安装结果
生成候选及核验暂存的完整步骤见[贡献指南](../CONTRIBUTING.md#本地候选构建与安装检查)。
以下文件名为本地候选，不代表公开发行；路径换成你取得的实际文件：

```bash
uv tool install --managed-python --python 3.14.7 --constraints runtime-constraints.txt ./inferyard-0.0.1-py3-none-any.whl
inferyard --versions
inferyard --help
```

PowerShell 将 `./...whl` 换为 `'.\inferyard-0.0.1-py3-none-any.whl'`；路径含空格或中文
时加引号。uv 管理 Python 和工具环境，缺解释器时首次安装需联网。首批目标为 Linux x64、
Windows x64、macOS arm64；纯 Python wheel 标签不证明所有平台的原生采集和引擎能力已验收。
当前验证基线为 Python 3.14.7 / uv 0.12.18，不宣称 uv 最低支持版本。

临时试用同一 wheel：

```bash
uvx --isolated --managed-python --python 3.14.7 --constraints runtime-constraints.txt --from ./inferyard-0.0.1-py3-none-any.whl inferyard --help
```

`uvx` 是 `uv tool run` 的别名，`--from` 指定分发物，最后的 `inferyard` 是命令名。
不要使用 `uvx inferyard`。`--isolated` 避免复用同名持久工具的不同依赖环境；缓存仍可复用。
实际解释器、依赖和评分身份以 `--versions` 及证据为准，普通安装不会读取维护者仓库的 uv.lock。
公开索引安装命令要等名称归属和真实索引重装验证完成后提供。

## 2. 创建工作区与选择题包

```bash
inferyard init --out bench-work --bundle zh-smoke
inferyard device-check --model /path/to/model.gguf --out bench-work/preflight --json
```

`--bundle` 可选 `zh-smoke`（默认）、`zh-core` 或 `zh-svg-pelican`。导出题包保留权威原字节和
内嵌人工审核；SVG 题只展示生成结果、不评分。工作区含三个平台的未绑定示例、一个所选题包、
空 `assets/`、`results/` 及入门说明。模板中的 `REPLACE` 项必须填完，不直接 run。
Windows 用实际盘符路径替换模型路径；所有系统均只读取已有模型，不自动下载。

五个准备命令的 `--out` 都是**不存在的新目录**，其父目录必须存在。已有空目录、文件和链接
一律拒绝，没有 `--force`。失败可能留下本次不完整目录；保留检查并换新目录重试，不覆盖原件。
`device-check` 的无模型检查可以完成，但创建候选需要成功的模型推荐。

## 3. 准备候选

### Windows x64：先准备固定 runtime

操作者从 Prism 上游的固定 release `prism-b10743-adfffbe` 下载归档；固定来源、文件名、大小和
SHA256 见 [Windows profile](contracts/windows-prism-batch-contract.md#社区安装的离线运行时准备)。
`runtime prepare` 不下载、不启动服务；它在完成全部资产核验后执行一次 `--version`（10 秒超时）。
不需要旧仓库回执，不要求 D 盘，普通用户的新目录即可。

CPU：

```powershell
inferyard runtime prepare --profile prism-b10743-adfffbe-win-cpu-x64 `
  --archive 'C:\Downloads\llama-prism-b10743-adfffbe-bin-win-cpu-x64.zip' `
  --out 'bench-work\assets\cpu-runtime'
```

CUDA 12.4：

```powershell
inferyard runtime prepare --profile prism-b10743-adfffbe-win-cuda-12.4-x64 `
  --archive 'C:\Downloads\llama-prism-b10743-adfffbe-bin-win-cuda-12.4-x64.zip' `
  --runtime-archive 'C:\Downloads\cudart-llama-bin-win-cuda-12.4-x64.zip' `
  --out 'bench-work\assets\cuda-runtime'
inferyard config create --preflight 'bench-work\preflight\device-preflight.json' `
  --runtime-receipt 'bench-work\assets\cuda-runtime\runtime-receipt.json' `
  --bundle 'bench-work\bundles\zh-smoke.json' --results 'bench-work\results' `
  --out 'bench-work\candidate'
```

按预检推荐的 mode 选择 CPU/CUDA；不匹配会阻断。CPU 禁止 `--runtime-archive`，CUDA 必填。
候选生成重新核验包内 profile、回执、全部成员、清单和实际字节，再查询引擎版本。
候选引用 runtime 目录的资产和清单，**不会复制**。移动或删除 runtime 目录后，既有候选失效；
应在确定位置重新生成候选并绑定。保留该目录直到所有引用它的配置不再使用。

### macOS arm64

准备已有、适配版本的 Prism 引擎及同目录动态库：

```bash
inferyard config create --preflight bench-work/preflight/device-preflight.json --engine /path/to/llama-server --bundle bench-work/bundles/zh-smoke.json --results bench-work/results --out bench-work/candidate
```

候选生成会执行引擎 `--version`（10 秒），重新检查容量、mode 和 AC 电源；Metal 要求相应动态库。
`--model-repo` 默认为 local，`--model-revision` 默认为模型文件 SHA256，`--port` 默认为 48857。
本地 SHA256 不冒充上游模型 revision。macOS 不接受 Windows 回执。

### Linux x64

```bash
inferyard config assets --model /path/to/model.gguf --engine /path/to/llama-server --out bench-work/assets/local-model
```

此动作不执行引擎。它从 GGUF 导出原 UTF-8 模板字节，写出模型/引擎哈希和同目录共享库清单。
模板缺失时阻断，不使用其他模型或历史运行的模板。库链接只能指向引擎同目录内的普通文件。

复制 `bench-work/configs/linux.example.toml` 到新候选文件，按 `assets.json` 填入模型、引擎、
模板和清单路径/哈希；按实际设备和所选题包填写版本、线程、上下文、生成参数、环境与字节预算。
填写与外部服务完全一致的 `engine.startup_args`。保留未绑定哨兵 PID/start_ticks=1，随后由 bind
替换；这不是运行授权。Linux 首版不提供自动候选算法，禁止为了通过而降低停止条件。

## 4. 外部启动、绑定、运行与报告

操作者按照候选 `engine.binary_path` 和完整 `engine.startup_args` 在外部终端启动服务。
CLI 不管理该服务的启动、终止或重启。将实际 PID 和本机端点代入：

```bash
inferyard config bind --candidate bench-work/candidate/candidate.toml --pid 1234 --endpoint http://127.0.0.1:48857 --out bench-work/bound
inferyard run --config bench-work/bound/config.toml
inferyard report --runs bench-work/results/RUN_ID --out bench-work/report
inferyard verify --path bench-work/report
```

Linux 的 `--candidate` 换成手工填写的新 TOML。绑定只读取进程身份和 argv，不发送模型请求。
PID、启动时间或参数不符会阻断；不要修改候选去掩盖观测差异。`run` 会做当次普通/流式探测，
独立 `probe` 仅用于可选排障且会发送请求。服务恢复、dirty、主机锁、取消和排空仍按原规则执行。
不要手工删除锁文件，也不要在运行或恢复期间升级工具环境。

用浏览器打开 `bench-work/report/report.html`。报告自包含，服务关闭后仍可离线阅读；
`verify` 离线检查已有产物。准备 JSON 的 prepared/complete 不表示测评或平台验收完成。
Windows/PowerShell 可使用相同 CLI 参数，路径加引号，PID 使用实际数字。

## 5. 升级、回退和离线

在运行和恢复结束后，取得新版本同批 wheel、约束及摘要，以相同安装命令显式安装。
回退时使用保留的旧 wheel 和旧约束，不重写用户工作区或旧证据。当前没有已发布版本，
不能把同版本重装声称为跨版本兼容验收。已安装同版本时可显式 `uv tool install --force ...` 重装。

正式运行优先使用持久安装。直接调用安装后的离线命令不会下载依赖；uvx 冷缓存需要准备环境：

```bash
uvx --offline --isolated --managed-python --python 3.14.7 --constraints runtime-constraints.txt --from ./inferyard-0.0.1-py3-none-any.whl inferyard --versions
```

`--offline` 要求解释器、wheel 和依赖均已在所选缓存中，冷缓存明确失败，不隐藏首次联网需求。
uvx 缓存可清理，外部长期 MLX 服务使用持久安装提供的脚本路径；不要在其运行时清缓存。
下载/证书/代理问题按 [uv 网络配置](https://docs.astral.sh/uv/concepts/configuration-files/)排查；
安装失败与模型/设备不支持分开报告。记录报错及 `uv --version`，不要将凭据写入 CLI 参数或日志。

准备命令 stdout 为 JSON、stderr 为脱敏诊断；0 准备完成、2 输入/身份阻断、4 IO/工具错误、
130 取消。正式 run 不完整可退出 3，模型答错不等于 CLI 失败。完整语义见 CLI 契约。

## 首次主机初始化

首次实时操作前执行 `inferyard host-state migrate`。它保存旧状态原字节、退休固定旧工具入口，
然后提交新状态；成功仍为 ready_to_run=false，运行前还要核验服务、资产和预算。
旧 dirty 先用原项目恢复；旧 lock-only 或凭据发布后来源变化需要调查，不能删锁或清空状态。
正常运行仅访问 inferyard-host.* 和不可变凭据。离线 verify/report 不建立主机状态。
历史格式和 origin=migrated 明确返回 unsupported_format/2；旧数据留在原项目处理。
Windows 原生迁移本次未验。详见[当前格式契约](contracts/inferyard-current-format.md)。
