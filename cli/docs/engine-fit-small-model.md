# 小模型多引擎准备

本页用于 Apple Silicon 上的短请求接入诊断。当前验证结果与未完成项见
[模型与引擎 backlog](backlog/model-engine.md#小模型多引擎接入)；
执行、停止、恢复与证据规则见 [engine-fit 指南](engine-fit-common.md)。

## 模型与资产

通用引擎使用 Qwen3-0.6B，按资产分别冻结计划：

| 资产 | 固定来源 revision | 引擎 |
| --- | --- | --- |
| `Qwen/Qwen3-0.6B-GGUF` 的 `Qwen3-0.6B-Q8_0.gguf` | `23749fefcc72300e3a2ad315e1317431b06b590a` | llama.cpp、LM Studio |
| `mlx-community/Qwen3-0.6B-4bit` 完整目录 | `73e3e38d981303bc594367cd910ea6eb48349da8` | MLX-LM、vLLM Metal、SGLang；oMLX 仅协议冒烟 |

GGUF 文件为 639446688 bytes，SHA256 为
`9465e63a22add5354d9bb4b99e90117043c7124007664907259bd16d043bb031`。
MLX 的 `model.safetensors` 为 335450584 bytes，SHA256 为
`392e8d466d56100ada00eb82031fb854297fc9e389b7d303eba3af114e87bce2`。
完整 MLX 目录仍由 `engine-fit plan` 冻结配置、tokenizer 和权重清单，不能只检查权重。
来源：[Qwen GGUF](https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/tree/23749fefcc72300e3a2ad315e1317431b06b590a)、
[MLX 量化](https://huggingface.co/mlx-community/Qwen3-0.6B-4bit/tree/73e3e38d981303bc594367cd910ea6eb48349da8)。

两个计划的量化和文件不同，不能合成同资产比较。短输出可能只包含思考文字并以
`length` 结束；协议完成不等于答案正确。普通请求可显式使用 `/no_think`，
但不能修改已冻结计划或改写旧响应。

Splash 当前官方支持列表没有 Qwen3-0.6B，也没有覆盖这些引擎的共同小模型。
其特定大模型需要单独的模型、设备和预算；不能用空服务或另一引擎替代验收。
依据 [Splash 模型与内存要求](https://github.com/incoai/splash#models)。
Ollama 不在本轮目标内。

## 独立环境与启动

InferYard 继续使用仓库锁定的 Python 3.15 环境。以下引擎各用独立的 uv Python 3.12
环境，不安装进 CLI 环境。保留引擎版本、完整依赖列表和来源提交。

| 引擎 | 本轮准备版本／来源 |
| --- | --- |
| llama.cpp | `b10566-bb4caa754`，本机已有 `llama-server` |
| MLX-LM | `mlx-lm==0.32.2`、`mlx==0.32.4` |
| vLLM Metal | vLLM `0.31.0+cpu` macOS arm64 wheel + `vllm-metal==0.31.0` |
| LM Studio | 本机 `lms` commit `69d945a`；服务版本不由 engine-fit 暴露 |
| SGLang | `e262b44eef1f2bf83e34345276e2bb91a086587e` 加下述本地补丁 |
| oMLX | `0.7.1rc1`，源码 `7a5a3d75ce7463300bdfa5fedccd34348f60ff46` |

vLLM 使用[官方 macOS wheel 安装方法](https://docs.vllm.ai/projects/vllm-metal/en/stable/installation/)。
CPU wheel 名称不代表这次执行使用 CPU；实际插件和后端必须从进程、依赖与日志另行记录，
仍不能据日志推定所有权重的 GPU 驻留。

`MODEL_DIR` 表示已核验的 MLX 模型绝对路径。服务仅监听 loopback，一次运行一个引擎；
本轮采用 2048 上下文、并发 1、每条最多 64 输出 token、60 秒请求期限，
内存停止下限 1024 MiB、温度停止 85°C。服务加载和原生检查不计入请求期限。

```bash
vllm serve MODEL_DIR --served-model-name bench-model \
  --host 127.0.0.1 --port 48864 --max-model-len 2048 \
  --max-num-seqs 1 --gpu-memory-utilization 0.15

SGLANG_USE_MLX=1 python -m sglang.launch_server \
  --model-path MODEL_DIR --served-model-name bench-model \
  --host 127.0.0.1 --port 48865 --enable-metrics --disable-cuda-graph \
  --context-length 2048 --max-running-requests 1 \
  --mem-fraction-static 0.15 --sleep-on-idle
```

命令中的 `python` / `vllm` 必须来自对应引擎环境。MLX-LM 使用
[受控服务入口](engine-fit-common.md#受控-mlx-lm-服务)，不以原版 `mlx_lm.server`
替代队列与同步观察。LM Studio 使用 `lms ls --json` 返回的 `modelKey` 加载，
再使用已加载的 `identifier` 请求；仓库目录名不一定是 `modelKey`。

## SGLang 固定源码补丁

[补丁](../scripts/patches/sglang-e262b44-mlx.patch) 仅适用于上述源码提交，
不随 InferYard 导入，也不自动改写用户的引擎安装。它处理两个已复现问题：

- MLX stub 继承了 PyTorch graph 初始化，触发 `_DummyModel.shared_experts_config`
  缺失。补丁在 MLX stub 的初始化入口明确保留“无 Torch graph”状态；
  推理仍走原来的原生 MLX runner。
- macOS 的 `setproctitle` 在导入时合并原始 argv。补丁将四个管理模块的导入
  移入真正修改标题的工作进程入口；主进程保持可核验的原始参数。
  InferYard 的模型绑定和 argv 检查保持原样。

在新的 SGLang checkout 内核对提交后应用。下面的 `PATCH` 是本仓库补丁的绝对路径，
`ENGINE_ENV` 是单独的 Python 3.12 环境路径；不要指向 CLI 的 `.venv`。

```bash
git checkout --detach e262b44eef1f2bf83e34345276e2bb91a086587e
git rev-parse HEAD
git apply --check --unidiff-zero "$PATCH"
git apply --unidiff-zero "$PATCH"
cp python/pyproject_other.toml python/pyproject.toml
uv venv --python 3.12 "$ENGINE_ENV"
uv pip install --python "$ENGINE_ENV/bin/python" './python[srt_mps]'
uv pip freeze --python "$ENGINE_ENV/bin/python"
```

补丁使用零行上下文，必须保留 `--unidiff-zero` 并核对来源提交。
目录归档安装可能将分发版本显示为 `0.0.0.dev0`，该字符串不能替代源码提交与补丁摘要。
MLX 依赖安装规则来自 [SGLang 官方说明](https://docs.sglang.io/docs/hardware-platforms/apple_metal)；
只安装文本运行时 `srt_mps`，本任务不需要 diffusion extra。

`--sleep-on-idle` 可降低空闲轮询的 CPU 占用。温停后先保存原件、停止本轮服务并
核对进程退出，处理负载或散热原因，再用新进程和新目录恢复。不可降低停止阈值、
删除主机状态文件，或将部分完成计为通过。

## oMLX 接入边界

oMLX 可以用上述 MLX 目录完成 OpenAI 普通和流式请求，但当前不是
`engine-fit run --engine` 的可选值。其 `/api/status` 的 `active_requests`
统计 output collectors，`waiting_requests` 统计等待队列；取消消费者可以先移除
collector，而 scheduler 的 deferred abort 尚未处理。状态接口没有同时证明
`in_use`、在途清理和实际排空，因此两项计数为零不足以开放 engine-fit。

定位：[server_status](https://github.com/jundot/omlx/blob/7a5a3d75ce7463300bdfa5fedccd34348f60ff46/omlx/server.py)、
[消费者清理](https://github.com/jundot/omlx/blob/7a5a3d75ce7463300bdfa5fedccd34348f60ff46/omlx/engine_core.py)、
[deferred abort](https://github.com/jundot/omlx/blob/7a5a3d75ce7463300bdfa5fedccd34348f60ff46/omlx/scheduler.py)。
还需绑定实际模型目录、加载配置与唯一服务实例，不能用 `--model-dir` 根目录直接
证明某个子目录已加载。这里不添加虚假的 `/metrics` 或用别的引擎 ID 发送诊断。

独立协议冒烟须明确标为未取得 engine-fit 资格，并在结束时确认自有进程退出。
`--base-path` 不隔离所有 oMLX 副作用：该版本会另外写
`~/.omlx/bin/omlx-cluster-python`，启动前应记录该文件状态，结束时只处理本轮创建的文件。
