# engine-fit 引擎绑定

本文属于 [engine-fit 引擎诊断](engine-fit.md)。

### 各引擎绑定方式

- vLLM / SGLang：绑定模型目录，启动入口须为 `vllm serve DIR` 或
  `python -m vllm.entrypoints.openai.api_server` / `sglang.launch_server` 加唯一
  `--model` / `--model-path`；歧义参数（额外的 `--model`/`--config`、值以 `-` 开头）
  即拒绝。idle 来源为 `/metrics` 全部 running/waiting series，缺失、重复、非有限或
  负值均拒绝。
- llama.cpp：绑定单 GGUF，入口为 `llama-server -m|--model FILE` 且参数限于白名单
  （[`engine_fit_entrypoints.py`](../../cli/src/inferyard/platforms/engine_fit_entrypoints.py)
  的 `_LLAMA_VALUES`/`_LLAMA_FLAGS`），拒绝额外模型、LoRA、draft、mmproj、router、
  HF/URL 下载与未知参数；服务环境出现任何 `LLAMA_ARG_` 变量即阻断，环境值本身不读取
  或持久化。idle 使用 `llamacpp:requests_processing` 与 `llamacpp:requests_deferred`
  均为整数零。
- MLX-LM：仅 macOS，进程入口必须是随包
  [`mlx_server.py`](../../cli/src/inferyard/data/engine_fit/mlx_server.py) 的内容匹配
  脚本（binding 增加 `entrypoint_sha256`），`--model` 指向目录；idle 要求
  `engine_fit:num_requests_running`、`engine_fit:num_requests_waiting` 与
  `engine_fit:poisoned` 三项指标齐全且为零，poisoned 状态在重启前不可声明 idle。
- LM Studio：仅 macOS，绑定唯一已加载本机实例；`lms` 调用固定 `127.0.0.1` 与显式
  `--port`，不传 `--host`（[ADR 015](../../cli/docs/decisions/015-lmstudio-local-cli-identity.md)）；
  `link status` 的 issues 必须含 `deviceDisabled`，实例 `deviceIdentifier` 必须为 null，
  相对模型路径不得逃逸 models_root 且解析后与冻结 GGUF samefile；还须通过
  `verify_model_file` 在稳定服务子树的真实打开/映射文件中观察到冻结 GGUF 的
  device/inode，观察到额外 GGUF 即拒绝。原生绑定检查共享 10 秒总预算，单次 CLI/lsof
  最多 2 秒、输出各最多 1 MiB、服务子树最多 8 个进程（`MAX_PROCESSES`），超限阻断。
- Ollama：缺少完整服务 idle 来源，在运行准入与传输构造两处均固定阻断
  （`ollama_service_idle_observation_unavailable`），不发送模型请求；能力表将其标为
  工具观察缺口而非硬件或模型不兼容。

### 温度与内存停止覆盖

`--skip-temperature-stop` 与 `--skip-memory-stop` 只能在创建计划时冻结原因（非空、
最多 1024 UTF-8 bytes，参与计划哈希），分别产生 plan.v3/run.v4 与 plan.v4/run.v5
（[ADR 016](../../cli/docs/decisions/016-engine-fit-temperature-override.md)、
[ADR 019](../../cli/docs/decisions/019-engine-fit-memory-override.md)）；run 不接受
运行时切换。覆盖时对应阈值为 null，`Guard` 跳过该阈值比较与缺测阻断，但温度、内存
与缺测原因仍采集并保存，HTML 逐项明示转义后的冻结原因；内存覆盖与显式
`--min-free-memory-mib` 互斥，非法原因在资产哈希前拒绝。磁盘、身份、主机锁、dirty、
时限、取消与排空不受覆盖影响，旧计划保持默认 85°C 与内存下限；新旧计划不可混合
比较。

### 平台定义分派

Linux 产物为 run.v1，macOS 为 run.v2（v2 绑定增加 `cwd_sha256`、
`listener_identity`/`listener_source` 且 `listener_inode=null`，来源为
`lsof:TCP:LISTEN:pid`），Windows 单 GGUF llama.cpp 为 run.v6
（[ADR 020](../../cli/docs/decisions/020-windows-engine-fit-native.md)，绑定增加
`process_start_source`，温度来源为 `nvidia-smi` 主机 GPU 读数）；plan.v2/v3/v4 分别
对应 run.v3/v4/v5。字段形状与平台/来源规则见
[engine-fit 契约](../../cli/docs/contracts/engine-fit-contract-common.md#常用引擎扩展) 的
macOS 与 Windows 原生来源小节。

