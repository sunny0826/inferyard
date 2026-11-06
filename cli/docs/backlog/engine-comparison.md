# 跨引擎受控比较

[返回 Backlog](../backlog.md)

## 跨引擎受控比较

2026-11-06：[ADR 056](../decisions/056-engine-comparison.md) 与
[实施计划](../plans/engine-comparison.md) 已写入。同日实现 `engine` 比较模式的身份门槛
与[契约](../contracts/engine-comparison.md)；调优参数、逐请求 token 配对、冷缓存和块交错
仍为阻断，本模式尚不产生性能资格。
首组为同一 GGUF 下的 llama.cpp 对 LM Studio；多轮回放暂不做。
能力实测两次检查均因外部背景负载（代理进程约 160% CPU）温度超过 85°C 阈值
（96.9°C、95.5°C）而未发请求。
操作者已授权测评流程停止并重启 LM Studio 后台；原 PID 94330 在本轮检查时已不存在。
