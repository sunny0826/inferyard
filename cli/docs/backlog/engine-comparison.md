# 跨引擎受控比较

[返回 Backlog](../backlog.md)

## 跨引擎受控比较

2026-11-06：[ADR 056](../decisions/056-engine-comparison.md) 与
[实施计划](../plans/engine-comparison.md) 已写入，代码未开始。
首组为同一 GGUF 下的 llama.cpp 对 LM Studio；多轮回放暂不做。
能力实测首次尝试因外部背景负载导致温度 96.9°C 超过 85°C 阈值而停止，未发请求。
操作者已授权测评流程停止并重启 LM Studio 后台；原 PID 94330 在本轮检查时已不存在。
