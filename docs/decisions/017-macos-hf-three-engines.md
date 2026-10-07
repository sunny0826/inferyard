# ADR 017：MLX 总序列上限与线程本地 GPU stream

状态：Accepted

## 决策

受控 MLX 服务提供可选 `--max-model-len`，生成前检查输入 token 与最大输出之和，不截断输入。
输入准备计入 waiting，拒绝后撤回本请求预约。加载和生成分别使用公开线程本地 GPU stream，
加载完成先同步；生成器关闭与本线程 GPU 同步全部成功才清除 running。

## 理由与代价

只有输入上限不足以声明总序列预算；普通 GPU stream 跨加载/生成线程使用会失败。
通过公开服务边界校验和线程本地 stream 修复，避免修改第三方推理实现或静默滚动 KV 截断。
未指定上限保留原输入限制，同名量化与原始 HF 目录仍不是同一资产。

同目录引擎对照记录各自依赖、模板和参数差异，不能推断内核等价。接口见[HF/MLX 补充契约](../contracts/engine-fit-contract.md#mlx-总序列与线程)。
