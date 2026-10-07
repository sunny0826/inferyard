# ADR 014：常用引擎的显式观察接口

状态：Accepted

## 决策

在 vLLM/SGLang 基础上增加 llama.cpp、MLX-LM、LM Studio，离线 `engine-fit engines` 列出能力。
llama.cpp 绑定单 GGUF 与 metrics；LM Studio 绑定唯一加载的本机 GGUF、模型根目录和禁用 LM Link 的实例。
MLX 使用随包受控脚本及公开生成 API，队列覆盖生成器关闭与 GPU 同步。Ollama 因缺完整 idle 来源而阻断。
新 plan.v2 支持目录/单 GGUF，run.v3 保存平台与版本缺测，旧定义保留。

## 理由与代价

仅添加 API 名称会掩盖模型、排队与释放差异；远程 Link 或 Cloud 不能因 loopback 端点被当作本机推理。
改造上游私有 MLX server 会耦合不稳定生命周期，因此采用小型自有服务；Ollama 不伪造能力。
引擎版本未公开时为 null，不用 CLI 版本替代。

LM Studio 调用方式由 [ADR 015](015-lmstudio-local-cli-identity.md)补充；平台和字段见[常用引擎契约](../contracts/engine-fit-contract.md#常用引擎扩展)。
