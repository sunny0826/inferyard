# macOS 正式本地安装准备

本页记录本地正式 profile 的安装要求，不授予模型或完整题包资格。
接口见[CLI 参考](cli-reference/local-engines.md)，执行与证据边界见
[契约](contracts/local-engines.md)。当前完成范围统一记录在
[模型与引擎进度](backlog/model-engine.md#本地五引擎正式适配)。

## 独立安装与原件

Python 引擎使用独立 venv，正式入口由候选生成器选择。
`runtime_roots` 包含实际使用的完整安装及外部非系统动态库所在的固定版本目录。
不能只声明 Python 可执行文件，也不能将整个用户目录当作运行时根；
模型目录、凭据、日志和 metrics 状态目录不属于安装库存。
只声明 venv 时，Homebrew 的外部 FFmpeg、OpenMP 等依赖仍可能使绑定拒绝。
以实际映射与完整依赖核验决定缺项，不能通过扩大系统目录豁免解决。

对现有环境做修改前创建独立副本，保留原安装和所有旧候选。
应用补丁后重新冻结候选；旧运行的安装摘要不会变成新安装的资格。
本轮固定安装为 Python 3.12、vLLM `0.31.0+cpu` + vLLM Metal `0.31.0`，
SGLang 源码 `e262b44eef1f2bf83e34345276e2bb91a086587e`，
以及 OpenCV Python `5.0.0.93`。这些版本标识不能代替源码和动态库库存。

## 保留可核验的 worker

以下补丁属于外部安装，不改写 InferYard 的身份检查。

| 补丁 | 适用范围 | 改动 |
| --- | --- | --- |
| [SGLang MLX](../scripts/patches/sglang-e262b44-mlx.patch) | 固定 SGLang 源码；先应用 | MLX stub 不进入 Torch graph 初始化；延迟主进程的 title 模块导入 |
| [SGLang worker](../scripts/patches/sglang-preserve-worker-identity.patch) | 上述补丁后的 SGLang | 去掉文本服务 worker 的进程标题改写，保留原 argv 和 exec 环境 |
| [SGLang run ID](../scripts/patches/sglang-preserve-run-id.patch) | 同一 SGLang 源码 | 尊重显式声明的 `SGLANG_RUN_ID` |
| [vLLM worker](../scripts/patches/vllm-preserve-worker-identity.patch) | 固定 vLLM 安装 | 保留 worker 的原始进程标题 |
| [OpenCV Darwin loader](../scripts/patches/opencv-darwin-loader-environment.patch) | 固定 OpenCV 安装 | Darwin 不注入 Linux 的 `LD_LIBRARY_PATH`；其他平台原行为不变 |

这些补丁均为零上下文格式。在对应源码根先执行
`git apply --unidiff-zero --check PATCH`，通过后使用
`git apply --unidiff-zero PATCH` 应用。
SGLang 补丁路径以 `python/sglang/` 为根，另两个包分别为 `vllm/` 和 `cv2/`。
不能对不同版本强行套用或忽略失败 hunk。补丁后的源码须可解析，
最小原生子进程检查还须确认 argv、环境和子树可观察。
`SPT_NOENV` 只能保护环境，不能解决 argv 已被进程名覆盖的问题。

## 主进程与 worker 环境

`environment` 是操作者传给主进程的启动环境。
可选 `worker_environment` 是安装声明中完整、精确的后代预期，
必须包含主进程已声明项；不传给主进程、不从实际 worker 自动生成。
schema 只接受列出的家族字段及值，不接受任意环境名。

本轮 vLLM Metal 派生期会增加：

| 字段 | 本轮固定值或确定方式 |
| --- | --- |
| `VLLM_WORKER_MULTIPROC_METHOD` | `spawn` |
| `MLX_MAX_OPS_PER_BUFFER` | `2000` |
| `TRANSFORMERS_NO_ADVISORY_WARNINGS` | `1` |
| `VLLM_USE_V2_MODEL_RUNNER` | `0` |
| `VLLM_OMP_NUM_THREADS_SET_BY_VLLM` | `1` |
| `OMP_NUM_THREADS` | 由固定版本的派生线程逻辑与本机线程数确定，须显式核对 |

不能将 worker 的 OMP 值预注入主进程，否则 vLLM 会走不同的线程设置分支。
所有后代都必须与同一 worker 声明匹配；出现不同环境时拒绝，
不能把某字段改成可选匹配或允许多个值任选。

SGLang 主进程显式声明 `SGLANG_RUN_ID` 和 `PROMETHEUS_MULTIPROC_DIR`；
后者必须是本次服务专用的新目录，位于声明的工作目录内。
worker 另有四项固定关闭值 `0`：`SGLANG_USE_CUDA_IPC_TRANSPORT`、
`SGLANG_ENABLE_TORCH_COMPILE`、`SGLANG_DISABLE_OUTLINES_DISK_CACHE`、
`SGLANG_ENABLE_DETERMINISTIC_INFERENCE`。
不同题包启动新服务时使用新 metrics 目录，不沿用旧 PID 的计数文件。
引擎内部启动 warmup 与操作者发出的测评请求分开记录。

## 绑定后的边界

安装核验通过只证明安装可读、库存及声明符合要求；模型加载后的正式绑定
还核验 listener、实际主进程、完整 worker 树、模型和映射库。
绑定通过也不等于题包完成。完整默认测评仍需分别完成 `zh-core` 和
`zh-svg-pelican`、核验跨 scope 的旧进程树退出及新绑定，并生成新报告。
温度停止、软件失败和未获所有权的后台都保留原原因与未执行分母。
