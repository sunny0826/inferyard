# 同机引擎适配诊断

`engine-fit` 记录同一台机器、同一份模型资产在不同引擎上的协议完成情况、客户端耗时、token 和进程树资源。
默认三条短请求，生成离线 HTML；不计算正式质量分数或性能赢家。完整基准使用 `run`。

## 可执行范围

```bash
inferyard engine-fit engines
```

此命令离线列能力与随包 MLX 脚本路径，不安装或启动引擎。

| 引擎 ID | 资产 | 实时平台 | 必要观察 |
| --- | --- | --- | --- |
| vllm | 模型目录 | Linux/macOS | 原生服务身份及全部 running/waiting metrics |
| sglang | 模型目录 | Linux/macOS | 同上，启用 metrics |
| llama-cpp | 单 GGUF | Linux/macOS/Windows | 显式本地模型、`--metrics`、唯一监听 |
| mlx-lm | 模型目录 | macOS | 随包受控服务的队列、释放与 poisoned 状态 |
| lmstudio | 单 GGUF | macOS | 唯一本机加载实例、lms、关闭 LM Link |
| ollama | 单 GGUF | 执行阻断 | 缺完整服务 idle 来源 |

模型、引擎和独立引擎环境由操作者准备；测评器不管理服务生命周期。
平台历史覆盖见[平台状态](platforms.md)，字段和版本规则见[引擎契约](contracts/engine-fit-contract.md)。

## 冻结输入

在实际执行机器上选择同一份资产，计划输出须为模型目录外的新目录。目录允许普通文件软链接，拒绝目录软链接；GGUF 分片不支持。

```bash
inferyard engine-fit plan --model /models/local-model --engines vllm sglang \
  --max-tokens 32 --request-timeout 60 --repetitions 1 \
  --min-free-memory-mib 1024 --out results/fit-plan
```

`--prompts prompts.json` 可事前指定文本：

```json
[
  {"id": "summary", "prompt": "请把这段材料总结成一句话：……"},
  {"id": "json", "prompt": "只输出 JSON 对象，键为 status，值为 ok。"}
]
```

最多 1000 请求，每条输入最多 64 KiB，单请求 deadline 最多 600 秒；顺序、重复、输出上限与 temperature=0 冻结。
默认可用内存下限 512 MiB、磁盘 256 MiB、温度上限 85°C。请求预算不含哈希、预检、写盘与排空。
量化目录、HF 原权重和转换 GGUF 不是同一资产，不强制合并比较。

## vLLM 与 SGLang

在各引擎自己的环境外部启动，不安装进测评器 Python 环境：

```bash
vllm serve /models/local-model --served-model-name bench-model --host 127.0.0.1 --port 8000
```

确认实际监听 PID 后运行：

```bash
inferyard engine-fit run --plan results/fit-plan/plan.json --engine vllm \
  --endpoint-url http://127.0.0.1:8000 --server-pid 12345 \
  --served-model bench-model --out results/fit-vllm
```

完成后由操作者停止该服务，再启动并绑定 SGLang：

```bash
python -m sglang.launch_server --model-path /models/local-model \
  --served-model-name bench-model --host 127.0.0.1 --port 8000 --enable-metrics
inferyard engine-fit run --plan results/fit-plan/plan.json --engine sglang \
  --endpoint-url http://127.0.0.1:8000 --server-pid 23456 \
  --served-model bench-model --out results/fit-sglang
```

Apple Silicon 的引擎依赖按选定版本的 [vLLM Metal](https://docs.vllm.ai/projects/vllm-metal/en/stable/installation/)
或 [SGLang MLX](https://docs.sglang.io/docs/hardware-platforms/apple_metal) 说明准备。
后者使用 MLX 路线时在引擎环境设置 `SGLANG_USE_MLX=1`。记录真实版本和依赖，不能把后端声明当作 GPU 驻留证明。
缺少 idle 指标、原生身份或模型兼容性时保留阻断，不绕过验证器。

## llama.cpp 与 LM Studio

macOS 上选 LM Studio 模型根中的同一 GGUF，冻结两个引擎：

```bash
inferyard engine-fit plan --model /Users/you/.lmstudio/models/publisher/repo/model.gguf \
  --engines llama-cpp lmstudio --max-tokens 32 --request-timeout 60 \
  --out results/gguf-plan
/absolute/path/to/llama-server --model /Users/you/.lmstudio/models/publisher/repo/model.gguf \
  --alias bench-model --host 127.0.0.1 --port 8000 --metrics
```

服务启动命令在外部终端执行。确认监听 PID 后：

```bash
inferyard engine-fit run --plan results/gguf-plan/plan.json --engine llama-cpp \
  --endpoint-url http://127.0.0.1:8000 --server-pid 12345 \
  --served-model bench-model --out results/fit-llama
```

llama.cpp 不接受额外模型、LoRA、draft、mmproj、router、RPC、下载或未知加载参数。
服务环境不得有 `LLAMA_ARG_` 配置覆盖。Windows 仅此引擎可在 engine-fit 实时执行，见[Windows 说明](../WINDOWS.md#windows-engine-fit-diagnostics)。

关闭 llama.cpp 后，在 LM Studio 中加载同一 GGUF，关闭 LM Link，外部启动 API 服务。
只保留一个本机加载实例，记录其 identifier、模型根和当前监听 PID。只读查询使用显式端口，不传 `--host`：

```bash
/absolute/path/to/lms ps --json --port 1234
/absolute/path/to/lms link status --json --port 1234
inferyard engine-fit run --plan results/gguf-plan/plan.json --engine lmstudio \
  --endpoint-url http://127.0.0.1:1234 --server-pid 23456 \
  --served-model YOUR_LOADED_INSTANCE_IDENTIFIER \
  --lms-path /absolute/path/to/lms --models-root /Users/you/.lmstudio/models \
  --out results/fit-lmstudio
```

lms 应提供 `status`、`queued`、`deviceIdentifier=null` 和 `deviceDisabled`；缺字段即阻断。
还须在稳定服务子树观察到冻结 GGUF 的文件身份，不能只凭同名路径或 CLI 声明。
模型版本、加载配置与原生观察上限见[常用引擎契约](contracts/engine-fit-contract.md#常用引擎扩展)。

## 受控 MLX-LM 服务

从 `engine-fit engines` 的 MLX 项复制 `server_script` 绝对路径。
在已有独立 MLX 环境中启动；下方解释器和脚本路径均替换为实际值：

```bash
"/absolute/mlx-env/bin/python" "/installed/inferyard/data/engine_fit/mlx_server.py" \
  --model /models/local-model --host 127.0.0.1 --port 8001 \
  --served-model-name bench-model --max-model-len 512
inferyard engine-fit plan --model /models/local-model --engines mlx-lm \
  --max-tokens 32 --request-timeout 60 --out results/mlx-plan
inferyard engine-fit run --plan results/mlx-plan/plan.json --engine mlx-lm \
  --endpoint-url http://127.0.0.1:8001 --server-pid 34567 \
  --served-model bench-model --out results/fit-mlx
```

脚本使用公开 MLX-LM API，原版 `mlx_lm.server` 不提供这里要求的完整队列观察。
只接受固定目录、单条文本 chat、temperature=0 和有界输出。`--max-model-len` 为 1–32768，
指定后检查输入与最大输出之和，不截断输入；生成器关闭和 GPU 同步成功后才清零。
同步不确定时保留 poisoned。认证只通过 `--api-key-env ENV_NAME` 引用环境变量，不传密钥值。

## 比较、停止与恢复

```bash
inferyard engine-fit compare --runs results/fit-vllm results/fit-sglang --out reports/fit-comparison
inferyard verify --path reports/fit-comparison
```

同计划、同资产、同主机与同测量源码的两个不同引擎才可比较。输出包含来源副本，关服后可离线核验。
`engine-fit verify` 仍可使用；新 manifest.v2 默认验证封存字节，`--rerender` 显式检查当前渲染。
未封存取证和旧格式规则见[离线读取契约](contracts/offline-reading-contract.md)。

超时、取消、断流停止余项，无法确认排空时保留 dirty。操作者确认旧服务退出并准备新服务后：

```bash
inferyard engine-fit run --plan results/fit-plan/plan.json --engine vllm \
  --endpoint-url http://127.0.0.1:8000 --server-pid 34567 --served-model bench-model \
  --recovery-confirm TOKEN --recovery-note '已确认旧服务退出并启动新服务' \
  --out results/fit-vllm-recovered
```

TOKEN 来自运行结果。旧失败不覆盖，不删除锁/state；资源停止后先处理原因，不反复跑满。
请求时限不含有界原生检查收尾、排空和封存。RSS 是边界采样的进程树之和，不是峰值或显存。

## 显式临时跳过温度停止

操作者明确决定后，创建新计划时可传 `--skip-temperature-stop REASON`。
原因非空且最多 1024 UTF-8 bytes；plan.v3/run.v4 将上限设 null，继续记录温度与缺测，报告明示覆盖。
默认和旧计划仍为 85°C，run 不接受切换开关；身份、内存、磁盘、预算、主机锁与排空继续生效。

## 显式临时跳过内存停止

新计划可传 `--skip-memory-stop REASON`，与 `--min-free-memory-mib` 互斥，使用 plan.v4/run.v5。
原因规则同上，只暂停可用内存下限与内存缺测阻断；可与温度覆盖组合。
内存和温度仍采集，其他停止与恢复继续执行。覆盖不改变设备容量或模型兼容性，也不能混入旧计划比较。
