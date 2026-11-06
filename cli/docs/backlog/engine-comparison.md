# 跨引擎受控比较

[返回 Backlog](../backlog.md)

## 跨引擎受控比较

2026-11-06：[ADR 056](../decisions/056-engine-comparison.md) 与
[实施计划](../plans/engine-comparison.md) 已写入。同日实现单对 `compare --mode engine`
身份门槛和多块 `compare-engines`（调优声明、逐请求 token 配对、冷缓存、块交错与判定），
规则见[契约](../contracts/engine-comparison.md)。仅有合成数据单元回归；
未在真实 llama-server／LM Studio run 上执行，LM Studio 是否报告 `cached_tokens` 未知，
不报告时该侧阻断 `cold_cache_unverified`。结果未封存、无 HTML。
首组为同一 GGUF 下的 llama.cpp 对 LM Studio；多轮回放暂不做。
能力实测两次检查均因外部背景负载（代理进程约 160% CPU）温度超过 85°C 阈值
（96.9°C、95.5°C）而未发请求。
操作者已授权测评流程停止并重启 LM Studio 后台；原 PID 94330 在本轮检查时已不存在。
