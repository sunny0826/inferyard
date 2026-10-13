# ADR 010：监测器与测评结果只读适配

状态：Accepted

## 决策

observer 可用 `--bench-run` 读取 v3 运行的配置/PID 身份，将模型标签和来源摘要写入自己的流，
并以运行所在卷采集磁盘。Python 离线桥验证完整流和封存 run，生成独立诊断摘要。
取消或缺结束记录可显式生成 partial，损坏记录仍拒绝。

## 理由与代价

时间相近不能证明请求归属，两个程序的单调时钟不可直接相减；因此不自动合并逐题指标或写回原 run。
保留独立证据与哈希需要额外摘要，但避免把观察关联误当作完整测评覆盖或全流程开销。

接口见[适配契约](../contracts/observer-contract.md#测评来源关联)，基本时钟与资源规则见[observer 契约](../contracts/observer-contract.md)。
