# 使用 uv 安装与准备测评

项目采用 [MIT 许可证](../LICENSE)。以下每条命令也可用 `uvx --isolated --managed-python --python 3.15 inferyard ...` 单次运行；详细参数通过 `inferyard --help` 查询。

## 1. 安装与升级 uv 与 CLI

从 [uv 官方安装页](https://docs.astral.sh/uv/getting-started/installation/)安装 `uv`（要求 `uv >= 0.13.6`），随后安装 `inferyard`（由 `uv` 自动托管 Python 3.15）：

```bash
uv tool install --managed-python --python 3.15 inferyard
inferyard --versions
inferyard --help
```

升级到最新版本：

```bash
uv tool upgrade inferyard
```

若使用 GitHub Releases 离线附件，在核对 `SHA256SUMS` 后直接安装 wheel：

```bash
uv tool install --managed-python --python 3.15 ./inferyard-*.whl
```

在已缓存相同解释器与依赖的环境可附加 `--offline` 运行。

## 2. 初始化工作区、设备预检与资产准备

```bash
inferyard init --out bench-ops
inferyard device-check --model /path/to/model.gguf --out bench-ops/preflight/device-1 --json
```

- 省略 `--bundle` 时导出默认双题包 `zh-core` 与 `zh-svg-pelican`；显式传入 `--bundle` 可选 `zh-smoke`、`zh-core`、`zh-svg-pelican` 或 `zh-context-speed`。
- `inferyard init --out` 接受不存在的新目录、已存在的非符号链接空目录，或仅含非符号链接受管子目录（`models`、`engines`、`results`、`reports`）的目录，不覆盖历史产物。
- 工作区当前阶段可随时用 `inferyard progress --workspace bench-ops` 查询（若结果或报告目录位于工作区外，可附加 `--results DIR` / `--reports DIR`）；已有本地资产可通过 `inferyard assets scan` 只读扫描，正在运行的本机推理服务可通过 `inferyard service discover` 只读发现。
- `inferyard device-check --json` 同时输出硬件感知首装推荐（`starter_recommendation`）：在满足条件的 Apple Silicon、NVIDIA CUDA 或 AMD ROCm（`hip`）设备上优先推荐 **PolyStrata**，其余环境平滑回退至 `llama.cpp`。

### 模型解析与下载（`model resolve` / `model acquire`）

支持从 Hugging Face 与 ModelScope 解析并下载单文件 GGUF、多分片 GGUF 或完整目录模型（`--kind auto|gguf|directory`）：

```bash
# 先解析到不可变 revision 与文件清单
inferyard model resolve --source <HF_OR_MODELSCOPE_URL> --kind auto

# 下载到新目录（支持断点续传、独立代理与流式 JSON 进度）
inferyard model acquire \
  --source <HF_OR_MODELSCOPE_URL> \
  --kind auto \
  --out bench-ops/models/my-model \
  --resume \
  --progress-stream \
  --proxy-origin http://127.0.0.1:7890
```

如需私有仓库鉴权，通过 `--token-env ENV_VAR_NAME` 仅传入环境变量名。下载固定版本引擎归档可使用 `inferyard engine acquire` / `inferyard engine prepare`（同样支持 `--proxy-origin`、`--resume`、`--progress-stream`）。

### 自动准备本地 PolyStrata（推荐）

在受支持的 macOS（Apple Silicon）、Linux x64 或 Windows x64 设备上，可直接通过一条命令自动完成预检、独立 Python 3.10–3.13 虚拟环境与运行时构建、模型下载并生成 `installation.json`：

```bash
inferyard engine prepare-polystrata \
  --out bench-ops/engines/polystrata \
  --data-dir bench-ops/polystrata-data \
  --progress-stream
```

常用选项：
- `--family` / `--model`（如 `--family qwen36 --model UD-Q4_K_M`）、`--context`
- `--backend auto|cuda|hip|metal`、`--model-source auto|modelscope|huggingface`、`--gguf-path`（复用本地 GGUF）
- `--python /path/to/python3.12`（显式指定用于创建 PolyStrata 独立 `.venv` 的 Python 3.10–3.13 解释器，与 CLI 自身的 Python 3.15 解耦）
- `--proxy-origin http://127.0.0.1:7890`、`--resume`（断点续装）、`--progress-stream`（向 stderr 输出单行 JSON 进度事件）

安装完成后可使用 `inferyard engine check --installation bench-ops/engines/polystrata/installation.json` 核验。

## 3. 准备候选

### 跨平台本地引擎安装声明（`installation.json`）

在 macOS、Linux 或 Windows 上接入独立安装的 `polystrata`、`llama-cpp`、`mlx-lm`、`vllm`、`sglang` 或 `lmstudio` 时，可先探测本机可执行文件与虚拟环境，再生成并校验 `installation.json`（完整 Schema 见 `inferyard --schema local_engine_installation`）：

```bash
# 只读探测本机引擎二进制与 Python 虚拟环境
inferyard engine detect --extra-path /abs/path/to/venv

# 生成并自动校验 local-engine-installation.v1 声明文件
inferyard engine init-installation \
  --family mlx-lm \
  --executable /abs/path/to/venv/bin/python3 \
  --working-directory /abs/path/to/workdir \
  --runtime-root /abs/path/to/venv \
  --out bench-ops/engines/mlx-lm/installation.json
```

生成的 `installation.json` 示例（支持标准 `venv`、`uv`、`conda` 环境及自定义非敏感环境变量，如 `HF_ENDPOINT`、`CUDA_VISIBLE_DEVICES`）：

```json
{
  "schema_version": 3,
  "definition": "local-engine-installation.v1",
  "family": "mlx-lm",
  "executable": "/abs/path/to/venv/bin/python3",
  "working_directory": "/abs/path/to/workdir",
  "runtime_roots": ["/abs/path/to/venv"],
  "environment": {}
}
```

可通过 `inferyard engine check --installation /abs/path/to/installation.json` 核验，并通过 `--installation` 配合 `--model` 与 `--results` 直接创建候选：

```bash
inferyard config create \
  --installation bench-ops/engines/mlx-lm/installation.json \
  --model /abs/path/to/model \
  --bundle bench-ops/bundles/zh-core.json \
  --results bench-ops/results \
  --out bench-ops/candidates/zh-core-1
```

对于 PolyStrata `installation.json`，还可通过 `--reasoning-effort {none,low,medium,high}` 冻结推理思考强度（开启思考时会自动上调默认输出 token 上限，并保留模型配置中的思考预算参数）。

在 Studio 中，也可通过「模型 → 配置本地引擎」向导或「设置 → 已有本地引擎」直接探测与登记。

### Windows x64：使用固定 `llama.cpp`（Prism）归档

除上述本地 PolyStrata 自动安装方式外，如使用 Prism 固定版本 `prism-b10743-adfffbe` 归档，可校验解包后创建候选（CUDA 12.4 需额外传入 `--runtime-archive`）：

```powershell
# CPU
inferyard runtime prepare --profile prism-b10743-adfffbe-win-cpu-x64 `
  --archive 'C:\Downloads\llama-prism-b10743-adfffbe-bin-win-cpu-x64.zip' `
  --out 'bench-ops\assets\cpu-runtime'

# CUDA 12.4
inferyard runtime prepare --profile prism-b10743-adfffbe-win-cuda-12.4-x64 `
  --archive 'C:\Downloads\llama-prism-b10743-adfffbe-bin-win-cuda-12.4-x64.zip' `
  --runtime-archive 'C:\Downloads\cudart-llama-bin-win-cuda-12.4-x64.zip' `
  --out 'bench-ops\assets\cuda-runtime'

inferyard config create --preflight 'bench-ops\preflight\device-1\device-preflight.json' `
  --runtime-receipt 'bench-ops\assets\cuda-runtime\runtime-receipt.json' `
  --bundle 'bench-ops\bundles\zh-core.json' --results 'bench-work\results' `
  --out 'bench-ops\candidate'
```

Windows 另提供 `inferyard memory-check [--protect-pid PID] --out NEW_DIR` 只读诊断物理内存与工作集，以及基于 `configs/` 示例模板的 KVMem / NInfer 固定质量配置。

### macOS arm64

如从 GitHub Releases 下载 `.dmg` 安装 InferYard Studio，拖入 `Applications` 后首次启动前需清除隔离属性（当前安装包未签名）：

```bash
xattr -dr com.apple.quarantine /Applications/InferYard\ Studio.app
```

使用本地 `llama-server`（同目录保留 `libggml-metal.dylib` 等动态库）创建候选：

```bash
inferyard config create --workspace bench-ops --bundle zh-core --engine /path/to/llama-server --out bench-ops/candidates/zh-core-1
```

### Linux x64

除 `installation.json` 流程外，使用本地 `llama-server` 时可导出 GGUF 聊天模板与文件哈希清单，填入 `bench-ops/configs/linux.example.toml` 副本：

```bash
inferyard config assets --model /path/to/model.gguf --engine /path/to/llama-server --out bench-ops/assets/local-model
```

## 4. 启动服务、绑定、运行与生成报告

在独立终端按候选 `engine.binary_path` 与完整 `engine.startup_args` 启动模型服务，随后绑定真实 PID 与本机端点（凭据仅通过 `endpoint.api_key_env` 环境变量名引用）：

```bash
inferyard config bind --candidate bench-ops/candidates/zh-core-1/candidate.toml --pid 1234 --endpoint http://127.0.0.1:48857 --out bench-ops/bound/zh-core-1
inferyard run --config bench-ops/bound/zh-core-1/config.toml
inferyard verify --path bench-ops/results/CORE_RUN_ID
```

常规测评再按相同流程运行 `zh-svg-pelican`，最后合并生成报告供 Studio 打开（分享脱敏证据可使用 `inferyard public package`）：

```bash
inferyard report --runs bench-ops/results/CORE_RUN_ID bench-ops/results/SVG_RUN_ID --out bench-ops/reports/summary-1
```

### 长上下文速度专项（`zh-context-speed`）

测量 64K / 128K / 256K 上下文预填充与解码速度时，先导出 `zh-context-speed` 工作区并运行设备预检，创建并绑定对应上下文长度的候选配置，再冻结计划、执行批次并离线读取结果：

```bash
# 1. 导出长上下文专项工作区并运行设备预检
inferyard init --bundle zh-context-speed --out bench-ops-ctx
inferyard device-check --model /path/to/model.gguf --out bench-ops-ctx/preflight/device-1 --json

# 2. 创建 64K 候选（128K 用 131072，256K 用 262144；Windows 使用 --runtime-receipt 替代 --engine），启动服务后执行 config bind
inferyard config create --profile context-speed --context-size 65536 \
  --workspace bench-ops-ctx --bundle zh-context-speed \
  --engine /path/to/llama-server \
  --out bench-ops-ctx/candidates/ctx-64k

# 3. 冻结长上下文速度计划、执行并读取核验结果
inferyard plan --preset context-speed --config bench-ops-ctx/bound/ctx-64k/config.toml --out bench-ops-ctx/plans/ctx-64k
inferyard run --plan bench-ops-ctx/plans/ctx-64k/plan.json --output-root bench-ops-ctx/results/ctx-64k-batch
inferyard speed-results --batch bench-ops-ctx/results/ctx-64k-batch
```

### 单独启动本地模型服务（无需测评任务）

仅需拉起本地 OpenAI 兼容模型服务、不建测评任务时，使用 `inferyard serve prepare` 生成回环启动计划 `<out>/serve-plan.json`（不计算模型哈希、无需题包或设备预检，上下文长度可设为任意不超过模型上限的正整数）：

```bash
# 引擎来源三选一：--installation <dir|installation.json> / --engine <path> / --runtime-receipt <path>
inferyard serve prepare \
  --model /path/to/model.gguf \
  --engine /path/to/llama-server \
  --context-size 16384 \
  --port 48857 \
  --out ./serve-out \
  --json
```

该命令会写入 `<out>/serve-plan.json` 并准备引擎所需的配套文件，不会自动拉起进程；保留输出目录，按计划中的 `working_directory`、`environment` 与 `argv` 另行启动服务。查询 `serve_plan` 契约定义：

```bash
inferyard --schema serve_plan
```

退出码约定：`0` 完成、`2` 输入或身份预检阻断、`3` 运行不完整、`4` IO 或工具错误、`130` 取消。
