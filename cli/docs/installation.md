# 使用 uv 安装与准备测评

项目采用 [MIT 许可证](../LICENSE)。以下每条命令也可用 `uvx --isolated --managed-python --python 3.15 inferyard ...` 单次运行；详细参数通过 `inferyard --help` 查询。

## 1. 安装 uv 与 CLI

从 [uv 官方安装页](https://docs.astral.sh/uv/getting-started/installation/)安装 `uv`，随后安装 `inferyard`：

```bash
uv tool install --managed-python --python 3.15 inferyard
inferyard --versions
inferyard --help
```

若使用 GitHub Releases 离线附件，在核对 `SHA256SUMS` 后直接安装 wheel：

```bash
uv tool install --managed-python --python 3.15 ./inferyard-*.whl
```

在已缓存相同解释器与依赖的环境可附加 `--offline` 运行。

## 2. 初始化工作区与设备预检

```bash
inferyard init --out bench-ops
inferyard device-check --model /path/to/model.gguf --out bench-ops/preflight/device-1 --json
```

- 省略 `--bundle` 时导出默认双题包 `zh-core` 与 `zh-svg-pelican`；显式传入 `--bundle` 可选 `zh-smoke`、`zh-core`、`zh-svg-pelican` 或 `zh-context-speed`。
- 所有准备与报告命令的 `--out` 均须指向不存在的新目录（不覆盖历史产物）。
- 单文件 GGUF 模型或引擎归档可通过 `inferyard model acquire` / `inferyard engine acquire` 下载；工作区当前阶段可随时用 `inferyard progress --workspace bench-ops` 查询。
- `inferyard device-check --json` 同时输出硬件感知首装推荐（`starter_recommendation`）：在满足条件的 Apple Silicon、NVIDIA CUDA 或 AMD ROCm（`hip`）设备上优先推荐 **PolyStrata**，其余环境平滑回退至 `llama.cpp`。

### 托管安装 PolyStrata（推荐）

在受支持的 macOS（Apple Silicon）、Linux x64 或 Windows x64 设备上，可直接通过一条命令自动完成预检、运行时构建、模型下载并生成 `installation.json`：

```bash
inferyard engine prepare-polystrata --out bench-ops/engines/polystrata --data-dir bench-ops/polystrata-data
```

常用选项：`--family` / `--model`（如 `--family qwen36 --model UD-Q4_K_M`）、`--context`、`--backend auto|cuda|hip|metal`、`--model-source auto|modelscope|huggingface`、`--gguf-path`（复用本地 GGUF）、`--resume`（断点续装）。安装完成后可使用 `inferyard engine check --installation bench-ops/engines/polystrata/installation.json` 核验。

## 3. 准备候选

### Windows x64：先准备固定 runtime

从 Prism 固定版本 `prism-b10743-adfffbe` 下载归档后校验解包（CUDA 12.4 需额外传入 `--runtime-archive`）：

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

使用本地 Prism `llama-server`（同目录保留 `libggml-metal.dylib` 等动态库）：

```bash
inferyard config create --workspace bench-ops --bundle zh-core --engine /path/to/llama-server --out bench-ops/candidates/zh-core-1
```

### macOS 本地安装声明（installation.json）

接入独立安装的 `polystrata`、`llama-cpp`、`mlx-lm`、`vllm`、`sglang` 或 `lmstudio` 时，准备绝对路径声明 `installation.json`（完整结构见 `inferyard schema --kind local_engine_installation`）：

```json
{
  "schema_version": 3,
  "definition": "local-engine-installation.v1",
  "family": "mlx-lm",
  "executable": "/abs/path/to/venv/bin/python3",
  "working_directory": "/abs/path/to/workdir",
  "runtime_roots": ["/abs/path/to/venv"],
  "environment": {
    "PYTHONDONTWRITEBYTECODE": "1",
    "HF_HUB_OFFLINE": "1",
    "TRANSFORMERS_OFFLINE": "1"
  }
}
```

可通过 `inferyard engine check --installation /abs/path/to/installation.json` 核验，或在 Studio「设置 → 已有本地引擎」中直接注册。

### Linux x64

导出 GGUF 聊天模板与文件哈希清单，填入 `bench-ops/configs/linux.example.toml` 副本：

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

退出码约定：`0` 完成、`2` 输入或身份预检阻断、`3` 运行不完整、`4` IO 或工具错误、`130` 取消。
