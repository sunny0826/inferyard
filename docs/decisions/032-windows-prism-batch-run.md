# ADR 032：Windows Prism 批量执行与原生资源采集

状态：Accepted

## 决策

Windows 的 `prism_llama_server_v1` 纳入核心批量执行，使用独立 `windows-resource.v1`。
KVMem/NInfer 保留 `windows-memory.v1`，其他未支持适配器在发请求前拒绝。
复用 v3 内存样本结构，保存绑定 PID 的 RSS 与主机可用内存，RSS 前后核验完整创建 FILETIME。
CPU、换页、温度、频率、GPU 与能耗未实现时为 null 并附原因。

## 理由与代价

仅放宽允许列表仍缺原生采集器；使用 Linux 或 Darwin 计数会误标来源。
独立 collector 标识能明确缺测边界，不强制把可选传感器作为全局阻断。
冻结策略要求某项温度来源时，缺测仍按现有停止规则执行。
RSS 仅含绑定 PID，不表示子进程、GPU 或瞬时峰值；严格性能资格仍需自己的环境与开销证据。

Windows engine-fit 的独立定义不限制核心批量入口。当前接口见[Prism 批量契约](../contracts/windows-prism-batch-contract.md)，实际覆盖见[平台状态](../platforms.md)。
